package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type action func(*model) tea.Cmd
type panelRow struct {
	title, detail string
	open          action
}
type panelPage struct {
	title, description string
	overview           bool
	rows               []panelRow
	cursor             int
	load               pageLoader
}
type pageLoader func(context.Context) (panelPage, error)
type panelPrompt struct {
	title              string
	input              textarea.Model
	multiline, confirm bool
	submit             func(string) tea.Cmd
}
type panelState struct {
	pages  []panelPage
	prompt *panelPrompt
	origin screen
	done   chan struct{}
	cancel context.CancelFunc
}
type panelResult struct {
	page  panelPage
	err   error
	apply func(panelPage) tea.Cmd
}

func (m *model) page() *panelPage { return &m.panel.pages[len(m.panel.pages)-1] }
func (m *model) schedule(status string, load pageLoader, apply func(panelPage) tea.Cmd) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	done := make(chan struct{})
	result := make(chan panelResult, 1)
	m.panel.done, m.panel.cancel = done, cancel
	m.status, m.failure = status, ""
	go func() { defer close(done); defer cancel(); p, err := load(ctx); result <- panelResult{p, err, apply} }()
	return func() tea.Msg { return <-result }
}
func openPage(load pageLoader) action {
	return func(m *model) tea.Cmd {
		return m.schedule(tr("Загрузка…"), load, func(p panelPage) tea.Cmd {
			m.panel.prompt = nil
			p.load = load
			m.panel.pages = append(m.panel.pages, p)
			return nil
		})
	}
}
func (m *model) reload() tea.Cmd {
	p := m.page()
	if p.load == nil {
		return nil
	}
	return m.schedule(tr("Обновление…"), p.load, func(next panelPage) tea.Cmd {
		next.cursor = min(p.cursor, max(0, len(next.rows)-1))
		next.load = p.load
		*p = next
		return nil
	})
}
func (m *model) change(status string, write func(context.Context) error) tea.Cmd {
	return m.schedule(status, func(ctx context.Context) (panelPage, error) { return panelPage{}, write(ctx) }, func(panelPage) tea.Cmd {
		m.panel.prompt = nil
		if err := m.refreshLibrary(""); err != nil {
			m.report(err)
			return nil
		}
		return m.reload()
	})
}
func (m *model) ask(title, value string, multiline bool, submit func(string) tea.Cmd) tea.Cmd {
	editor := textarea.New()
	editor.Prompt = ""
	editor.ShowLineNumbers = false
	editor.CharLimit = 256 << 10
	editor.MaxHeight = 0
	editor.MaxWidth = 0
	editor.SetWidth(max(8, m.contentWidth()-2))
	editor.SetHeight(max(1, m.bodyHeight()-3))
	editor.SetValue(value)
	m.failure = ""
	m.panel.prompt = &panelPrompt{title: title, input: editor, multiline: multiline, submit: submit}
	return m.panel.prompt.input.Focus()
}
func (m *model) confirm(title string, yes action) tea.Cmd {
	m.panel.prompt = &panelPrompt{title: title, confirm: true, submit: func(string) tea.Cmd { return yes(m) }}
	return nil
}
func (m *model) stopped(p profile) bool {
	if m.owner == p.dir && (m.process != nil || m.starting || m.restarting) {
		m.report(errors.New(tr("сначала остановите сервер в консоли")))
		return false
	}
	return true
}
func (m *model) enterPanel(p panelPage) {
	m.panel = panelState{origin: m.screen, pages: []panelPage{p}}
	m.screen = panelScreen
	m.status, m.failure = "", ""
}
func (m *model) exitPanel() {
	m.screen = m.panel.origin
	m.panel = panelState{}
	m.status, m.failure = "", ""
}
func (m *model) panelKey(key tea.KeyMsg) tea.Cmd {
	k := key.String()
	if m.panel.done != nil {
		if k == "esc" {
			m.panel.cancel()
		}
		return nil
	}
	if q := m.panel.prompt; q != nil {
		switch {
		case k == "esc":
			m.panel.prompt = nil
			m.failure = ""
			return nil
		case q.confirm:
			if k == "y" || k == "Y" || k == "д" || k == "Д" {
				return q.submit("")
			}
			return nil
		case k == "ctrl+s" || (!q.multiline && k == "enter"):
			return q.submit(q.input.Value())
		default:
			var cmd tea.Cmd
			q.input, cmd = q.input.Update(key)
			return cmd
		}
	}
	p := m.page()
	last := max(0, len(p.rows)-1)
	switch k {
	case "esc":
		if len(m.panel.pages) == 1 {
			m.exitPanel()
			return nil
		}
		m.panel.pages = m.panel.pages[:len(m.panel.pages)-1]
		m.failure = ""
		return m.reload()
	case "up", "k":
		p.cursor = max(0, p.cursor-m.panelColumns())
	case "down", "j":
		p.cursor = min(last, p.cursor+m.panelColumns())
	case "left":
		if m.panelColumns() == 2 && p.cursor%2 == 1 {
			p.cursor--
		}
	case "right":
		if m.panelColumns() == 2 && p.cursor%2 == 0 {
			p.cursor = min(last, p.cursor+1)
		}
	case "pgup":
		p.cursor = max(0, p.cursor-m.panelCapacity())
	case "pgdown":
		p.cursor = min(last, p.cursor+m.panelCapacity())
	case "home":
		p.cursor = 0
	case "end":
		p.cursor = last
	case "r", "ctrl+r":
		return m.reload()
	case "enter":
		if len(p.rows) > 0 && p.rows[p.cursor].open != nil {
			return p.rows[p.cursor].open(m)
		}
	}
	return nil
}
func (m *model) panelColumns() int {
	if m.page().overview && m.width >= 96 && m.height >= 24 {
		return 2
	}
	return 1
}
func (m *model) overviewView() ([]string, string) {
	p := m.page()
	w, left := m.contentWidth(), m.formWidth()
	cell := (left - 2) / 2
	lines := []string{heading(tr("Управление сервером")), label(tr("Выберите раздел")), ""}
	for i := 0; i < len(p.rows); i += 2 {
		leftCard := dashboardCard(p.rows[i], cell, p.cursor == i)
		rightCard := dashboardCard(p.rows[i+1], cell, p.cursor == i+1)
		for j := range leftCard {
			lines = append(lines, leftCard[j]+"  "+rightCard[j])
		}
		lines = append(lines, "")
	}
	right := m.profileCard(m.profiles[m.cursor], w-left-2, m.bodyHeight())
	return sideBySide(lines, left, right, w-left-2, m.bodyHeight()), tr("←→ ↑↓ Выбор раздела   Enter Открыть   Esc Назад")
}
func dashboardCard(row panelRow, width int, active bool) []string {
	bg, mark := panel, "  "
	if active {
		bg, mark = selected, tint("▌ ", mint, true)
	}
	return []string{surface(mark+heading(row.title), width, bg), surface("  "+label(row.detail), width, bg), surface("", width, bg)}
}
func (m *model) panelCapacity() int { return max(1, m.bodyHeight()-4) }
func (m *model) panelView() ([]string, string) {
	p := m.page()
	w := m.contentWidth()
	if m.panel.done != nil && m.panel.prompt != nil {
		return []string{heading(oneLine(p.title)), "", label(m.status)}, tr("Esc Отменить")
	}
	if q := m.panel.prompt; q != nil {
		lines := []string{heading(oneLine(p.title)), ""}
		lines = append(lines, strings.Split(ansi.Wordwrap(oneLine(q.title), w, ""), "\n")...)
		if q.confirm {
			return append(lines, "", tint(tr("Y / Д   Подтвердить"), gold, true)), tr("Esc Отменить")
		}
		lines = append(lines, "")
		lines = append(lines, strings.Split(q.input.View(), "\n")...)
		if q.multiline {
			return lines, tr("Ctrl+S Сохранить   Esc Отмена")
		}
		return lines, tr("Enter Применить   Esc Отмена")
	}
	if m.panelColumns() == 2 {
		return m.overviewView()
	}
	lines := []string{heading(oneLine(p.title)), label(oneLine(p.description)), ""}
	overview := p.overview && m.wide()
	width := w
	if overview {
		width = m.formWidth()
	}
	c := m.panelCapacity()
	start := p.cursor / c * c
	for i := start; i < min(len(p.rows), start+c); i++ {
		r := p.rows[i]
		bg, mark := panel, "  "
		if i == p.cursor {
			bg, mark = selected, tint("▌ ", mint, true)
		}
		name := oneLine(r.title)
		hint := ansi.Truncate(oneLine(r.detail), width/3, "…")
		if hint != "" {
			name = paired(name, label(hint), width-3)
		}
		lines = append(lines, surface(mark+name, width, bg))
	}
	if len(p.rows) == 0 {
		lines = append(lines, label(tr("Пока пусто")))
	}
	if len(p.rows) > c {
		lines = append(lines, label(fmt.Sprintf("%d / %d", p.cursor+1, len(p.rows))))
	}
	keys := tr("↑↓ Выбор  Enter  R Обновить  Esc")
	if m.panel.done != nil {
		keys = tr("Esc Отменить операцию")
	}
	if overview && m.cursor < len(m.profiles) {
		server := m.profiles[m.cursor]
		right := m.profileCard(server, w-width-2, m.bodyHeight())
		lines = sideBySide(lines, width, right, w-width-2, m.bodyHeight())
	}
	return lines, keys
}
func (m *model) serverPanel(p profile) {
	m.enterPanel(panelPage{overview: true, title: p.Name, description: p.edition(), rows: []panelRow{
		{tr("Консоль"), tr("Команды и журнал"), func(m *model) tea.Cmd {
			m.exitPanel()
			if m.screen == listScreen {
				return m.listKey("enter")
			}
			return nil
		}},
		{tr("Файлы"), tr("Просмотр и редактор"), openPage(directoryPage(p, "."))},
		{tr("Базы данных"), "SQLite", openPage(databaseList(p))},
		{tr("Игроки"), tr("Списки и модерация"), openPage(playerPage(p))},
		{tr("Резервные копии"), "ZIP", openPage(backupPage(p, m.store))},
		{tr("Дополнения"), tr("Плагины и моды"), openPage(addonPage(p))},
		{tr("Сеть"), tr("Адреса и порт"), openPage(networkPage(p))},
		{tr("Настройки"), tr("Память и ядро"), openPage(settingsPage(p, m.store))},
	}})
}
