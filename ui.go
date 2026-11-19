package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type screen int

const (
	listScreen screen = iota
	createScreen
	consoleScreen
	panelScreen
)

type refreshMsg struct{}

type installResult struct {
	profile profile
	err     error
}

type javaResult struct {
	path string
	err  error
}

type model struct {
	editor             *codeEditor
	editorClip         string
	panel              panelState
	javaMode, javaPath string
	javaMenu           *javaDialog
	javaDone           chan struct{}
	cancelJava         context.CancelFunc
	javaCanceled       bool
	ctx                context.Context
	store              serverStore
	java               string
	profiles           []profile
	cursor             int
	screen             screen
	width              int
	height             int

	fields               [formCount]textinput.Model
	field                int
	offline              bool
	picker               *choiceMenu
	eulaOpen, eulaAccept bool
	cancelInstall        context.CancelFunc
	installDone          chan struct{}

	command        textinput.Model
	viewport       viewport.Model
	logText        string
	logRevision    uint64
	process        *serverProcess
	owner          string
	starting       bool
	restarting     bool
	quitting       bool
	libraryWarning string
	exitMessage    string
	status         string
	failure        string
}

func newModel(ctx context.Context, store serverStore, profiles []profile, java string) *model {
	command := textinput.New()
	command.Prompt = "› "
	command.Placeholder = tr("Команда сервера…")
	command.CharLimit = 512
	return &model{
		ctx: ctx, store: store, profiles: profiles, java: java,
		width: 80, height: 24, command: command, viewport: viewport.New(72, 14),
	}
}

func refresh() tea.Cmd {
	return tea.Tick(125*time.Millisecond, func(time.Time) tea.Msg { return refreshMsg{} })
}

func (m *model) Init() tea.Cmd { return refresh() }

func (m *model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case syntaxReady:
		if m.editor == msg.owner {
			e := m.editor
			e.highlightBusy = false
			if e.version == msg.version {
				e.syntax = msg
			}
			return m, e.highlight()
		}
		return m, nil
	case pasteReady:
		if m.editor == msg.owner && m.editor.version == msg.version && m.editor.selection == msg.sel && m.editor.mode == "" && m.panel.done == nil {
			if msg.err != nil {
				m.editor.notice = tr("Буфер обмена недоступен · используйте вставку терминала")
			} else if err := m.editor.insert(m.editor.pasteText(msg.text)); err != nil {
				m.editor.notice = err.Error()
			}
		}
		return m, nil
	case panelResult:
		m.panel.cancel, m.panel.done = nil, nil
		m.status = ""
		if msg.err != nil {
			m.report(msg.err)
			return m, nil
		}
		return m, msg.apply(msg.page)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if q := m.panel.prompt; q != nil && !q.confirm {
			q.input.SetWidth(max(8, m.contentWidth()-2))
			q.input.SetHeight(max(1, m.bodyHeight()-3))
		}
		if m.javaMenu != nil {
			m.javaMenu.path.Width = max(8, m.formWidth()-5)
		}
		atBottom := m.viewport.AtBottom()
		m.viewport.Width, m.viewport.Height = m.contentWidth()-4, m.logHeight()
		if atBottom {
			m.viewport.GotoBottom()
		} else {
			m.viewport.SetYOffset(m.viewport.YOffset)
		}
		m.command.Width = max(1, m.contentWidth()-10)
		for i := range m.fields {
			m.fields[i].Width = max(1, m.formWidth()-21)
		}
	case choicesLoaded:
		if msg.menu == m.picker {
			m.picker.loading = false
			m.picker.options = msg.options
			m.picker.index = max(0, slices.Index(msg.options, m.fields[m.field].Value()))
			if msg.err != nil {
				m.report(msg.err)
			} else if len(msg.options) == 0 {
				m.report(errors.New(tr("каталог пуст")))
			}
		}
	case refreshMsg:
		var highlight tea.Cmd
		if m.editor != nil {
			highlight = m.editor.highlight()
		}
		return m, tea.Batch(refresh(), m.refreshProcess(), highlight)
	case installResult:
		m.installDone, m.cancelInstall = nil, nil
		if m.quitting {
			return m, tea.Quit
		}
		if msg.err != nil {
			m.report(msg.err)
			return m, nil
		}
		m.profiles = append(m.profiles, msg.profile)
		slices.SortStableFunc(m.profiles, func(a, b profile) int {
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
		m.cursor = slices.IndexFunc(m.profiles, func(p profile) bool { return p.dir == msg.profile.dir })
		m.screen, m.status = listScreen, tr("Сервер создан.")
	case javaResult:
		canceled := m.javaCanceled
		m.javaCanceled = false
		m.cancelJava = nil
		m.javaDone = nil
		m.starting = false
		if m.quitting {
			return m, tea.Quit
		}
		if canceled {
			m.status = tr("Подготовка Java отменена.")
			return m, nil
		}
		if msg.err != nil {
			m.report(msg.err)
			return m, nil
		}
		process, err := launchServer(m.profiles[m.cursor], msg.path)
		if err != nil {
			m.report(err)
			return m, nil
		}
		m.process = process
		m.status = ""
		return m, m.command.Focus()
	case tea.KeyMsg:
		return m, m.handleKey(msg)
	default:
		if m.editor != nil {
			return m, m.editorFieldMsg(message)
		}
		switch {
		case m.screen == panelScreen && m.panel.prompt != nil && !m.panel.prompt.confirm:
			q := m.panel.prompt
			var cmd tea.Cmd
			q.input, cmd = q.input.Update(msg)
			return m, cmd
		case m.javaMenu != nil && m.javaMenu.browse && m.javaMenu.row < 0:
			var cmd tea.Cmd
			m.javaMenu.path, cmd = m.javaMenu.path.Update(msg)
			return m, cmd
		case m.screen == createScreen && editable(m.field) && m.picker == nil && m.javaMenu == nil && m.installDone == nil:
			var cmd tea.Cmd
			m.fields[m.field], cmd = m.fields[m.field].Update(msg)
			return m, cmd
		case m.screen == consoleScreen && !m.eulaOpen && m.process != nil:
			var cmd tea.Cmd
			m.command, cmd = m.command.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *model) handleKey(key tea.KeyMsg) tea.Cmd {
	if m.quitting {
		return nil
	}
	if m.editor != nil {
		return m.editorKey(key)
	}
	if key.String() == "ctrl+c" {
		m.quitting = true
		switch {
		case m.installDone != nil:
			m.cancelInstall()
			m.status = tr("Отмена установки…")
		case m.process != nil:
			m.process.stop()
			m.status = tr("Сохранение мира перед выходом…")
		default:
			return tea.Quit
		}
		return nil
	}

	switch m.screen {
	case panelScreen:
		return m.panelKey(key)
	case listScreen:
		return m.listKey(key.String())
	case createScreen:
		return m.createKey(key)
	case consoleScreen:
		return m.consoleKey(key)
	}
	return nil
}

func (m *model) listKey(key string) tea.Cmd {
	switch key {
	case "r", "R", "к", "К":
		m.failure, m.status = "", ""
		if err := m.refreshLibrary(""); err != nil {
			m.report(err)
		}
		return nil
	case "t", "T", "е", "Е":
		load := trashPage(m.store)
		m.enterPanel(panelPage{title: tr("Корзина"), load: load})
		return m.reload()
	case "f2":
		if m.cursor < len(m.profiles) {
			m.serverPanel(m.profiles[m.cursor])
		}
		return nil
	case "q", "Q", "й", "Й":
		if m.process != nil {
			m.quitting = true
			m.process.stop()
			m.status = tr("Сохранение мира перед выходом…")
			return nil
		}
		return tea.Quit
	case "up", "k":
		if m.cursor == 0 {
			m.cursor = len(m.profiles)
		} else {
			m.cursor--
		}
	case "down", "j":
		m.cursor = (m.cursor + 1) % (len(m.profiles) + 1)
	case "n", "N", "т", "Т":
		if m.process != nil {
			m.report(errors.New(tr("остановите активный сервер перед созданием профиля")))
			return nil
		}
		m.screen, m.field, m.offline = createScreen, 0, false
		m.status, m.failure = "", ""
		defaults := [formCount]string{coreField: "Vanilla", portField: "25565", memoryField: "2048"}
		for i, value := range defaults {
			field := textinput.New()
			field.Prompt = ""
			field.Width = max(1, m.formWidth()-21)
			field.CharLimit = 64
			field.SetValue(value)
			m.fields[i] = field
		}
		m.javaMode, m.javaPath = javaAuto, ""
		if m.java != "" {
			path := m.java
			if found, err := exec.LookPath(path); err == nil {
				path = found
			}
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
			m.javaMode, m.javaPath = javaManual, path
		}
		m.fields[0].Placeholder = tr("Название мира")
		return m.fields[0].Focus()
	case "enter":
		if m.cursor == len(m.profiles) {
			return m.listKey("n")
		}
		selected := m.profiles[m.cursor]
		if m.process != nil && m.owner != selected.dir {
			m.report(errors.New(tr("остановите активный сервер перед открытием другой консоли")))
			return nil
		}
		if m.owner != selected.dir {
			m.clearConsole(selected.dir)
		}
		m.screen, m.status, m.failure = consoleScreen, "", ""
		if m.process != nil {
			return m.command.Focus()
		}
	}
	return nil
}

func (m *model) createKey(key tea.KeyMsg) tea.Cmd {
	if m.installDone != nil {
		if key.String() == "esc" {
			m.cancelInstall()
			m.status = tr("Отмена установки…")
		}
		return nil
	}
	if m.javaMenu != nil {
		return m.javaKey(key)
	}
	if m.picker != nil {
		return m.pickerKey(key.String())
	}
	switch key.String() {
	case "esc":
		m.screen, m.status, m.failure = listScreen, "", ""
		return nil
	case "down", "tab":
		return m.focusField((m.field + 1) % formCount)
	case "up", "shift+tab":
		return m.focusField((m.field + formCount - 1) % formCount)
	case "enter", " ":
		if key.String() == " " && editable(m.field) {
			break
		}
		switch m.field {
		case versionField, coreField, buildField:
			return m.openPicker()
		case offlineField:
			m.offline = !m.offline
			return nil
		case javaField:
			m.openJava()
			return nil
		case submitField:
			return m.install()
		default:
			return m.focusField(m.field + 1)
		}
	}
	if editable(m.field) {
		var cmd tea.Cmd
		m.fields[m.field], cmd = m.fields[m.field].Update(key)
		return cmd
	}
	return nil
}

func (m *model) focusField(index int) tea.Cmd {
	m.fields[m.field].Blur()
	m.field = index
	if editable(index) {
		return m.fields[index].Focus()
	}
	return nil
}

func (m *model) install() tea.Cmd {
	p := profile{
		JavaMode: m.javaMode, JavaPath: m.javaPath,
		Name: strings.TrimSpace(m.fields[0].Value()), Version: m.fields[versionField].Value(), Core: m.fields[coreField].Value(), Build: m.fields[buildField].Value(), Offline: m.offline,
	}
	port, portErr := strconv.Atoi(strings.TrimSpace(m.fields[portField].Value()))
	memory, memoryErr := strconv.Atoi(strings.TrimSpace(m.fields[memoryField].Value()))
	switch {
	case p.Name == "" || p.Version == "":
		m.report(errors.New(tr("укажите имя сервера и версию Minecraft")))
	case portErr != nil || port < 1 || port > 65535:
		m.report(errors.New(tr("укажите порт от 1 до 65535")))
	case memoryErr != nil || memory < 256 || memory > 65536:
		m.report(errors.New(tr("укажите память от 256 до 65536 МиБ")))
	case p.Build == "":
		m.report(errors.New(tr("выберите сборку ядра")))
	default:
		p.Port, p.Memory = port, memory
		m.status, m.failure = tr("Загрузка и проверка серверного JAR…"), ""
		ctx, cancel := context.WithCancel(m.ctx)
		result := make(chan installResult, 1)
		done := make(chan struct{})
		m.cancelInstall, m.installDone = cancel, done
		store := m.store
		go func() {
			defer close(done)
			defer cancel()
			p, err := store.create(ctx, p)
			result <- installResult{profile: p, err: err}
		}()
		return func() tea.Msg { return <-result }
	}
	return nil
}

func (m *model) consoleKey(key tea.KeyMsg) tea.Cmd {
	if key.String() == "f2" && !m.eulaOpen && !m.starting && !m.restarting {
		m.serverPanel(m.profiles[m.cursor])
		return nil
	}
	if m.eulaOpen {
		return m.eulaKey(key.String())
	}
	switch key.String() {
	case "esc":
		if m.cancelJava != nil {
			m.javaCanceled = true
			m.cancelJava()
			m.status = tr("Отмена подготовки Java…")
			return nil
		}
		if !m.starting && !m.restarting {
			m.command.Blur()
			m.screen, m.status, m.failure = listScreen, "", ""
		}
	case "pgup":
		m.viewport.PageUp()
	case "pgdown":
		m.viewport.PageDown()
	case "ctrl+s":
		if m.process != nil {
			m.restarting = false
			m.process.stop()
		}
	case "ctrl+r":
		if m.process != nil {
			m.restarting = true
			m.process.stop()
		}
	case "enter":
		if m.process == nil {
			return m.start()
		}
		command := strings.TrimSpace(m.command.Value())
		if command != "" {
			if err := m.process.send(command); err != nil {
				m.report(err)
			} else {
				m.command.Reset()
				m.failure = ""
			}
		}
	default:
		if m.process != nil {
			var cmd tea.Cmd
			m.command, cmd = m.command.Update(key)
			return cmd
		}
	}
	return nil
}

func (m *model) clearConsole(owner string) {
	m.owner, m.logText, m.exitMessage = owner, "", ""
	m.logRevision = 0
	m.command.Reset()
	m.viewport.SetContent("")
}

func (m *model) start() tea.Cmd {
	if m.starting || m.process != nil {
		return nil
	}
	p := m.profiles[m.cursor]
	accepted, err := hasEULA(p.dir)
	if err != nil {
		m.report(err)
		return nil
	}
	if !accepted {
		m.eulaOpen, m.eulaAccept = true, false
		m.status, m.failure = "", ""
		return nil
	}
	m.clearConsole(p.dir)
	m.starting = true
	m.status, m.failure = fmt.Sprintf(tr("Проверка Java %d…"), p.Java), ""
	ctx, cancel := context.WithCancel(m.ctx)
	m.cancelJava = cancel
	java := m.java
	cache := filepath.Join(filepath.Dir(m.store.root), "java")
	if p.JavaMode == javaAuto {
		m.status = fmt.Sprintf(tr("Подготовка Java %d · автоматическая установка"), p.Java)
	}
	if installerCore(p.Core) {
		m.status = tr("Подготовка Java и установка ") + p.Core + "…"
	}
	done := make(chan struct{})
	result := make(chan javaResult, 1)
	m.javaDone = done
	go func() {
		defer close(done)
		defer cancel()
		path, err := resolveJava(ctx, p, java, cache)
		if err == nil {
			err = prepareCore(ctx, p, path)
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		result <- javaResult{path: path, err: err}
	}()
	return func() tea.Msg { return <-result }
}

func (m *model) refreshProcess() tea.Cmd {
	if m.process == nil {
		return nil
	}
	stopped := false
	select {
	case <-m.process.done:
		stopped = true
	default:
	}
	raw, next := m.process.log.snapshot(m.logRevision)
	changed := next != m.logRevision
	if changed {
		m.logRevision = next
		m.logText = cleanTerminalText(raw)
	}
	if stopped {
		m.exitMessage = tr("Сервер остановлен.")
		if m.process.err != nil {
			m.exitMessage = tr("Сервер завершился: ") + m.process.err.Error()
		}
		m.process = nil
		m.command.Blur()
		if m.quitting {
			return tea.Quit
		}
		if m.restarting {
			m.restarting = false
			return m.start()
		}
	}
	if changed || stopped {
		m.showLogs()
	}
	return nil
}

func (m *model) showLogs() {
	content := strings.TrimRight(m.logText, "\n")
	if m.exitMessage != "" {
		content = strings.TrimSpace(content + "\n\n" + m.exitMessage)
	}
	if content == "" {
		content = tr("Здесь появится журнал сервера.")
	}
	if m.viewport.AtBottom() || m.viewport.TotalLineCount() <= 1 {
		m.viewport.SetContent(content)
		m.viewport.GotoBottom()
	} else {
		m.viewport.SetContent(content)
	}
}

func cleanTerminalText(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) && r != '\n' {
			return -1
		}
		return r
	}, ansi.Strip(raw))
}

func (m *model) report(err error) {
	m.status = ""
	m.failure = strings.ReplaceAll(cleanTerminalText(err.Error()), "\n", " ")
}

func (m *model) contentWidth() int { return max(1, m.width-4) }
