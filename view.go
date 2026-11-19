package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The outer canvas owns its background so selection and text stay legible on
// both light and dark terminal themes. All geometry is in terminal cells.
const (
	canvas   = "#10151D"
	panel    = "#18212D"
	selected = "#233C44"
	ink      = "#E6F1ED"
	muted    = "#ACBCC7"
	edge     = "#536677"
	mint     = "#88E3BE"
	gold     = "#F6CF8A"
	coral    = "#FFAB9F"
)

func tint(s, color string, bold bool) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(bold).Render(s)
}

func fit(s string, w int) string {
	w = max(0, w)
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

func surface(s string, width int, bg string) string {
	s = fit(s, width)
	sample := lipgloss.NewStyle().Background(lipgloss.Color(bg)).Foreground(lipgloss.Color(ink)).Render("X")
	before, after, _ := strings.Cut(sample, "X")
	return before + strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+before) + after
}

func paired(left, right string, width int) string {
	return fit(left, width-lipgloss.Width(right)-1) + " " + right
}

func label(s string) string   { return tint(s, muted, false) }
func heading(s string) string { return tint(s, ink, true) }

func (m *model) wide() bool { return m.width >= 86 && m.height >= 20 }
func (m *model) formWidth() int {
	if m.wide() {
		return m.contentWidth() * 3 / 5
	}
	return m.contentWidth()
}
func (m *model) bodyHeight() int { return max(1, m.height-6) }
func (m *model) logHeight() int  { return max(1, m.bodyHeight()-4) }

func (m *model) state(p profile) string {
	if m.owner == p.dir {
		switch {
		case m.restarting:
			return "↻ Перезапуск"
		case m.starting:
			return "◐ Запуск"
		case m.process != nil:
			select {
			case <-m.process.stopping:
				return "◐ Остановка"
			default:
				return "● Работает"
			}
		}
	}
	return "○ Остановлен"
}

func stateLabel(state string) string {
	color := muted
	if strings.HasPrefix(state, "●") {
		color = mint
	}
	if strings.HasPrefix(state, "◐") || strings.HasPrefix(state, "↻") {
		color = gold
	}
	return tint(state, color, true)
}

func (m *model) View() string {
	if m.editor != nil {
		status := m.failure
		if status == "" {
			status = m.status
		}
		return m.editor.view(m.width, m.height, status)
	}
	if m.width < 36 || m.height < 13 {
		return ansi.Truncate("Glumine · требуется окно 36 × 13", max(1, m.width), "")
	}
	w := m.contentWidth()
	section := "Серверы"
	var body []string
	var keys string
	switch m.screen {
	case panelScreen:
		section = "Управление"
		body, keys = m.panelView()
	case listScreen:
		body = m.library()
		keys = "Enter Консоль   F2 Панель   N Создать   T Корзина   R Обновить   Q Выход"
		if len(m.profiles) == 0 {
			keys = "N Создать   T Корзина   R Обновить   Q Выход"
		}
	case createScreen:
		section = "Создание сервера"
		body = m.creator()
		keys = "↑↓ Поле   Enter Выбрать   Esc Назад"
		if m.installDone != nil {
			keys = "Esc Отменить загрузку"
		}
	case consoleScreen:
		section = "Консоль"
		body = m.console()
		keys = "Enter Запуск   F2 Панель   PgUp/Dn Журнал   Esc Назад"
		if m.process != nil {
			keys = "↵ Ввод   ^S Стоп   ^R Рестарт   F2 Панель   PgUp/Dn Журнал   Esc Назад"
		}
	}
	if m.picker != nil {
		keys = "↑↓ Выбор  PgUp/Dn  Enter OK  Esc"
	}
	if m.javaMenu != nil {
		keys = "↑↓ Выбор  Enter OK  Esc Назад"
	}
	if m.starting {
		keys = "Esc Отменить подготовку Java"
	}
	if m.eulaOpen {
		keys = "↑↓ Выбор  Enter OK  Esc Назад"
	}
	brand := tint("◆ ", mint, true) + heading("GLUMINE") + label("  /  ") + section
	if w >= 65 {
		brand = paired(brand, label("MINECRAFT  /  JAVA EDITION"), w)
	}
	lines := []string{brand, tint(strings.Repeat("─", w), edge, false), ""}
	for i := 0; i < m.bodyHeight(); i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		lines = append(lines, line)
	}
	status := m.status
	if status != "" {
		status = tint(status, mint, false)
	}
	if m.failure != "" {
		status = tint("! "+m.failure, coral, false)
	}
	if status == "" && m.screen == listScreen && m.libraryWarning != "" {
		status = tint(m.libraryWarning, gold, false)
	}
	if status == "" {
		status = tint(strings.Repeat("─", w), edge, false)
	}
	if lipgloss.Width(keys) > w {
		switch m.screen {
		case listScreen:
			keys = "Enter  F2 Панель  N Новый  T Корзина"
		case createScreen:
			keys = "↑↓ Поле  Enter Выбор  Esc Назад"
		case consoleScreen:
			keys = "Enter  ^S Стоп  F2 Панель  Esc"
			if m.process == nil {
				keys = "Enter Старт  F2 Панель  Esc"
			}
		}
	}
	lines = append(lines, status, tint(keys, mint, true), "")
	for i, line := range lines {
		lines[i] = surface("  "+fit(line, w)+"  ", m.width, canvas)
	}
	return strings.Join(lines, "\n")
}

func (m *model) library() []string {
	w, h := m.contentWidth(), m.bodyHeight()
	if len(m.profiles) == 0 {
		lines := []string{heading("Библиотека серверов"), ""}
		cardH := h - 2
		rows := make([]string, cardH)
		if cardH >= 12 {
			art := islandArt()
			if m.wide() {
				left := w / 2
				copy(rows[max(0, (cardH-len(art))/2):], art)
				copyText := []string{heading("Первый сервер"), label("Локальный мир Minecraft Java."), "", tint("[ N ]  Создать сервер", mint, true)}
				y := (cardH - len(copyText)) / 2
				for i := range rows {
					line := ""
					if i >= y && i < y+len(copyText) {
						line = copyText[i-y]
					}
					rows[i] = fit("   "+line, left) + center(rows[i], w-left)
				}
			} else {
				top := max(0, (cardH-len(art)-3)/2)
				for i, s := range art {
					rows[top+i] = center(s, w)
				}
				rows[top+len(art)] = center(heading("Первый сервер"), w)
				rows[top+len(art)+1] = center(tint("[ N ]  Создать сервер", mint, true), w)
			}
		} else {
			rows[cardH/2-1] = center(heading("Пока нет серверов"), w)
			rows[cardH/2+1] = center(tint("[ N ]  Создать сервер", mint, true), w)
		}
		for _, row := range rows {
			lines = append(lines, surface(row, w, panel))
		}
		return lines
	}
	left := m.formWidth()
	buttonBG := panel
	if m.cursor == len(m.profiles) {
		buttonBG = selected
	}
	rows := []string{paired(heading("Мои серверы"), label(strconv.Itoa(len(m.profiles))), left), surface(tint("  + Создать сервер", mint, true), left, buttonBG), ""}
	step := 2
	if h >= 17 {
		step = 3
	}
	page := max(1, (h-3)/step)
	start := (min(m.cursor, len(m.profiles)-1) / page) * page
	for i := start; i < min(len(m.profiles), start+page); i++ {
		p := m.profiles[i]
		bg, mark := panel, "  "
		if i == m.cursor {
			bg, mark = selected, tint("▌ ", mint, true)
		}
		name := heading(oneLine(p.Name))
		state := stateLabel(m.state(p))
		rows = append(rows, surface(mark+paired(name, state, left-3)+" ", left, bg),
			surface("  "+label(p.edition()+"   ·   :"+strconv.Itoa(p.Port)), left, bg))
		if step == 3 {
			rows = append(rows, "")
		}
	}
	if !m.wide() || m.cursor == len(m.profiles) {
		return rows
	}
	p := m.profiles[m.cursor]
	return sideBySide(rows, left, m.profileCard(p, w-left-2, h), w-left-2, h)
}

func oneLine(s string) string       { return strings.ReplaceAll(cleanTerminalText(s), "\n", " ") }
func center(s string, w int) string { return strings.Repeat(" ", max(0, (w-lipgloss.Width(s))/2)) + s }
func sideBySide(left []string, lw int, right []string, rw, h int) []string {
	lines := make([]string, h)
	for i := range lines {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		lines[i] = fit(l, lw) + "  " + surface(r, rw, panel)
	}
	return lines
}

func (m *model) profileCard(p profile, w, h int) []string {
	rows := []string{"", "  " + tint("ПРОФИЛЬ", mint, true)}
	if h >= 20 {
		for _, s := range islandArt() {
			rows = append(rows, center(s, w))
		}
	}
	rows = append(rows, "", "  "+heading(oneLine(p.Name)), "  "+stateLabel(m.state(p)), "")
	for _, item := range [][2]string{{"Ядро", p.edition()}, {"Java", strconv.Itoa(p.Java)}, {"Память", fmt.Sprintf("%d МиБ", p.Memory)}, {"Порт", strconv.Itoa(p.Port)}} {
		rows = append(rows, "  "+paired(label(item[0]), item[1], w-4))
	}
	return rows
}

func (m *model) choicePage() int { return max(1, m.bodyHeight()-3) }
func (m *model) choicesView(w int) []string {
	menu := m.picker
	rows := []string{heading(fieldNames[m.field]), label("Каталог доступных вариантов"), ""}
	if menu.loading {
		return append(rows, tint("  Загрузка каталога…", mint, false))
	}
	if len(menu.options) == 0 {
		return append(rows, label("  ↵ Повторить загрузку"))
	}
	count := m.choicePage()
	start := menu.index / count * count
	rows[1] = label(fmt.Sprintf("%d из %d", menu.index+1, len(menu.options)))
	for i := start; i < min(start+count, len(menu.options)); i++ {
		bg, marker := panel, "  "
		if i == menu.index {
			bg, marker = selected, tint("▌ ", mint, true)
		}
		text := menu.options[i]
		if m.field == buildField && m.fields[coreField].Value() == "Purpur" {
			text = "Сборка #" + text
		}
		rows = append(rows, surface(marker+text, w, bg))
	}
	return rows
}
func (m *model) creator() []string {
	left, h := m.formWidth(), m.bodyHeight()
	var rows []string
	if m.javaMenu != nil {
		rows = m.javaView(left)
	} else if m.picker != nil {
		rows = m.choicesView(left)
	} else {
		rows = []string{heading("Новый сервер")}
		stride := 1
		if h >= 19 {
			stride = 2
			rows = append(rows, "")
		}
		visible := max(1, (h-1-len(rows))/stride)
		start := max(0, min(m.field, submitField)-visible+1)
		for i := start; i < min(javaField, start+visible); i++ {
			bg, mark := panel, "  "
			if m.field == i {
				bg, mark = selected, tint("▌ ", mint, true)
			}
			var value string
			switch i {
			case versionField, coreField, buildField:
				value = m.fields[i].Value()
				if value == "" {
					value = "Выбрать"
				}
				value = tint("› "+value, mint, false)
			case offlineField:
				value = label("○ Выкл")
				if m.offline {
					value = tint("● Вкл", gold, true)
				}
			case submitField:
				value = tint("Enter", mint, true)
			default:
				value = m.fields[i].View()
			}
			rows = append(rows, surface(mark+fit(label(fieldNames[i]), 17)+value, left, bg))
			if stride == 2 {
				rows = append(rows, "")
			}
		}
		for len(rows) < h-1 {
			rows = append(rows, "")
		}
		bg, mark := "#203C64", "  "
		if m.field == javaField {
			bg, mark = "#305E97", "▌ "
		}
		mode := "Авто"
		if m.javaMode == javaManual {
			mode = "Свой путь"
		}
		rows = append(rows, surface(paired(tint(mark+"Выбрать Java", "#E2F1FF", true), tint(mode+"  › ", "#B8DCFF", false), left), left, bg))
	}
	if !m.wide() {
		return rows
	}
	w := m.contentWidth() - left - 2
	notes := [formCount]string{"Дайте миру название для библиотеки.", "Выберите релиз Minecraft из каталога Mojang.", "Доступные ядра зависят от версии Minecraft.", "Для Vanilla сборка совпадает с релизом игры. Для других ядер выберите номер сборки или версию загрузчика.", "Разрешить вход без проверки аккаунта Minecraft. По умолчанию проверка включена.", "Порт подключения игроков.", "Максимальная память Java в мебибайтах.", "Скачать выбранную сборку и добавить сервер в библиотеку.", "CLI может скачать Java нужной версии или использовать выбранный файл. Путь можно ввести или выбрать в списке папок."}
	right := []string{"", "  " + tint("КОНФИГУРАЦИЯ", mint, true), ""}
	if h >= 18 {
		for _, s := range islandArt() {
			right = append(right, center(s, w))
		}
	}
	for _, s := range strings.Split(ansi.Wordwrap(notes[m.field], w-4, ""), "\n") {
		right = append(right, "  "+s)
	}
	return sideBySide(rows, left, right, w, h)
}

func (m *model) console() []string {
	p := m.profiles[m.cursor]
	w := m.contentWidth()
	if m.eulaOpen {
		box := min(w, 62)
		rows := []string{heading(oneLine(p.Name)), surface("  Minecraft EULA", box, panel), surface("  Лицензия для запуска сервера", box, panel), surface("  "+eulaURL, box, panel)}
		for i, action := range []string{"Отмена", "Принимаю EULA и запускаю"} {
			bg, mark := panel, "  "
			if (i == 1) == m.eulaAccept {
				bg, mark = selected, tint("▌ ", mint, true)
			}
			rows = append(rows, surface(mark+action, box, bg))
		}
		for i, row := range rows {
			rows[i] = strings.Repeat(" ", (w-box)/2) + row
		}
		return rows
	}
	rows := []string{paired(heading(oneLine(p.Name)), stateLabel(m.state(p)), w),
		label(fmt.Sprintf("%s   ·   :%d   ·   %d МиБ", p.edition(), p.Port, p.Memory)), ""}
	logs := strings.Split(m.viewport.View(), "\n")
	for i := 0; i < m.logHeight(); i++ {
		line := ""
		if i < len(logs) {
			line = logs[i]
			if prefix, message, ok := strings.Cut(line, "]: "); ok {
				color := muted
				switch {
				case strings.Contains(prefix, "/WARN"):
					color = gold
				case strings.Contains(prefix, "/ERROR"):
					color = coral
				}
				line = tint(prefix+"]", color, false) + ": " + message
			}
		}
		if m.logText == "" && m.exitMessage == "" && i == 0 {
			line = label("Здесь появится журнал сервера.")
		}
		rows = append(rows, surface("  "+line, w, panel))
	}
	entry := tint("↵  Запустить сервер", mint, true)
	if m.starting {
		entry = label("Подготовка Java…")
	}
	if m.process != nil {
		entry = m.command.View()
		select {
		case <-m.process.stopping:
			entry = label("Сохранение мира…")
		default:
		}
	}
	return append(rows, surface("  "+entry, w, selected))
}
