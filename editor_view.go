package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

const editActive = "#1A2934"
const editSelection = "#355169"

func (e *codeEditor) codeLine(row, columns, gutter int) string {
	current := row == e.row(e.caret)
	bg, num := canvas, edge
	if current {
		bg, num = editActive, mint
	}
	prefix := tint(fmt.Sprintf("%*d ", gutter-2, row+1), num, current) + tint("│", edge, false)
	start, end := e.lines[row], e.lineEnd(row)
	first := start + sort.Search(end-start, func(i int) bool { return e.glyphs[start+i+1].column > e.left || start+i+1 == end })
	// The newline sentinel's column belongs to this line, as does EOF.
	var spans []syntaxSpan
	if e.syntax.version == e.version {
		spans = e.syntax.spans
	}
	token := sort.Search(len(spans), func(i int) bool { return spans[i].end > e.glyphs[first].offset })
	a, b := e.bounds()
	used := 0
	var painted, run strings.Builder
	fg, background := "", ""
	flush := func() {
		if run.Len() > 0 {
			painted.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(background)).Render(run.String()))
			run.Reset()
		}
	}
	for i := first; i <= end && used < columns; i++ {
		x := e.glyphs[i].column
		if x >= e.left+columns {
			break
		}
		ch := " "
		width := 1
		if i < end {
			ch = e.part(i, i+1)
			width = e.glyphs[i+1].column - x
			if x+width <= e.left {
				continue
			}
			if ch == "\t" {
				ch = strings.Repeat(" ", width)
			} else if r, _ := utf8.DecodeRuneInString(ch); unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
				ch = strings.Repeat("·", width)
			} else if width == 1 && len(ch) > 1 && uniseg.StringWidth(ch) == 0 {
				ch = "◌" + ch
			}
			if x < e.left || x+width > e.left+columns {
				width = min(x+width, e.left+columns) - max(x, e.left)
				ch = strings.Repeat(" ", width)
			}
		} else if x < e.left {
			break
		}
		for token < len(spans) && spans[token].end <= e.glyphs[i].offset {
			token++
		}
		color, back := ink, bg
		if token < len(spans) {
			color = spans[token].color
		}
		if i >= a && i < b {
			back = editSelection
		}
		if i == e.caret && e.mode == "" {
			color, back = canvas, mint
		}
		if color != fg || back != background {
			flush()
			fg, background = color, back
		}
		run.WriteString(ch)
		used += width
	}
	flush()
	return surface(prefix+painted.String(), gutter+columns, bg)
}
func (e *codeEditor) outlineRows(width, height int, focused bool) []string {
	idx := e.activeSymbol()
	if focused {
		idx = e.outlineIndex
	}
	out := []string{surface("  "+tint(tr("СТРУКТУРА"), mint, true), width, panel)}
	available := max(1, height-1)
	start := max(0, min(idx-available/2, len(e.syntax.symbols)-available))
	for r := 0; r < height-1; r++ {
		line := ""
		bg := panel
		if i := start + r; i < len(e.syntax.symbols) {
			s := e.syntax.symbols[i]
			marker := "  "
			if i == idx {
				bg = selected
				marker = tint("▌ ", mint, true)
			}
			line = marker + paired(ansi.Truncate(oneLine(s.name), max(1, width-9), "…"), label(strconv.Itoa(s.row+1)), width-3)
		} else if r == 0 {
			line = label(tr("  Нет разделов"))
		}
		out = append(out, surface(line, width, bg))
	}
	return out
}

func editorHelp() []string {
	return []string{tr("Клавиши редактора"), tr("Ctrl+S   Сохранить     Esc   Закрыть"), tr("Ctrl+Z / Ctrl+Y   Отмена / повтор"), tr("Shift + стрелки   Выделить   Ctrl+A   Всё"), tr("Ctrl+C / X / V    Копировать / вырезать / вставить"), tr("Ctrl+F   Поиск    F3 / Alt+P   Вперёд / назад"), tr("Ctrl+R   Заменить совпадение"), tr("Ctrl+G   К строке    F4   Структура"), tr("Tab / Shift+Tab   Отступ / убрать отступ"), tr("Ctrl+←/→   По словам   Ctrl+Home/End   Начало / конец")}
}

func (e *codeEditor) view(w, h int, status string) string {
	if w < 32 || h < 9 {
		return ansi.Truncate(tr("Glumine · увеличьте окно"), max(1, w), "")
	}
	inner := w - 2
	height := h - 5
	side := 0
	if w >= 104 && e.mode != "outline" && e.mode != "help" && len(e.syntax.symbols) > 0 {
		side = min(30, w/4)
	}
	gutter := len(strconv.Itoa(len(e.lines))) + 3
	columns := inner - gutter - side
	if side > 0 {
		columns--
	}
	row, col := e.row(e.caret), e.glyphs[e.caret].column
	e.top = max(0, min(e.top, row))
	if row >= e.top+height {
		e.top = row - height + 1
	}
	e.left = max(0, min(e.left, col))
	if col >= e.left+columns {
		e.left = col - columns + 1
	}
	language := tr("Текст")
	if e.lexer != nil {
		language = e.lexer.Config().Name
	}
	title := tint(" ◆ ", mint, true) + heading("GLUMINE") + label(tr("  /  Редактор"))
	badge := language
	if e.readonly {
		badge += tr("  ·  Чтение")
	}
	name := oneLine(filepath.ToSlash(e.path))
	state := ""
	if e.text != e.saved {
		state = tint(tr("  ● Изменён"), gold, true)
	}
	lines := []string{surface(paired(title, label(badge+" "), w), w, canvas), surface("  "+heading(name)+state, w, panel)}
	var right []string
	if side > 0 {
		right = e.outlineRows(side, height, false)
	}
	var overlay []string
	switch e.mode {
	case "outline":
		overlay = e.outlineRows(inner, height, true)
	case "help":

		offset := min(e.helpTop, max(0, len(editorHelp())-height))
		for _, s := range editorHelp()[offset:] {
			overlay = append(overlay, surface("  "+s, inner, panel))
		}
	}
	for n := 0; n < height; n++ {
		content := ""
		if e.mode == "outline" || e.mode == "help" {
			if n < len(overlay) {
				content = overlay[n]
			}
		} else {
			if r := e.top + n; r < len(e.lines) {
				content = e.codeLine(r, columns, gutter)
			} else {
				content = fit("", columns+gutter)
			}
			if side > 0 {
				content += " " + right[n]
			}
		}
		lines = append(lines, surface(" "+content, w, canvas))
	}
	message := e.notice
	if status != "" {
		message = status
	}
	switch e.mode {
	case "close":
		message = tint(tr("Сохранить?  S Да  D Сбросить  Esc Назад"), gold, true)
	case "search", "needle", "replacement", "goto":
		caption := map[string]string{"search": tr("Найти"), "needle": tr("Что заменить"), "replacement": tr("Заменить на"), "goto": tr("Строка")}[e.mode]
		e.field.Width = max(5, inner-lipgloss.Width(caption)-4)
		message = tint(caption+"  › ", mint, true) + e.field.View()
		if e.notice != "" {
			message = ansi.Truncate(message, inner/2, "…") + " " + label(e.notice)
		}
	case "help":
		message = tr("↑↓ Прокрутка   Esc Назад")
	case "outline":
		message = tr("↑↓ Выбор раздела   Enter Перейти   Esc Назад")
	}
	lines = append(lines, surface(" "+message, w, canvas))
	encoding := "UTF-8"
	if e.bom != "" {
		encoding += " BOM"
	}
	position := fmt.Sprintf(tr(" Стр %d/%d  Кол %d"), row+1, len(e.lines), col+1)
	lines = append(lines, surface(paired(tint(position, mint, true), label(encoding+"  "+e.endings+" "), w), w, panel))
	keys := tr(" ^S Сохранить   ^F Поиск   ^R Замена   ^G Строка   F4 Структура   F1 Клавиши   Esc Назад")
	if w < 89 {
		keys = tr(" ^S Запись   ^F Поиск   F1 Клавиши   Esc")
	}
	if w < 43 {
		keys = tr(" ^S Запись  ^F Поиск  F1  Esc")
	}
	lines = append(lines, surface(label(keys), w, canvas))
	return strings.Join(lines, "\n")
}
