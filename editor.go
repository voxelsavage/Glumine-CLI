package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type codeEditor struct {
	*document
	profile               profile
	path                  string
	readonly              bool
	top, left             int
	mode, notice, query   string
	field                 textinput.Model
	lexer                 chroma.Lexer
	syntax                syntaxReady
	highlightBusy         bool
	outlineIndex, helpTop int
}
type pasteReady struct {
	owner   *codeEditor
	version uint64
	sel     selection
	text    string
	err     error
}

func openCode(p profile, rel string) action {
	return func(m *model) tea.Cmd {
		var raw []byte
		return m.schedule("Чтение файла…", func(context.Context) (panelPage, error) {
			err := inRoot(p.dir, func(r *os.Root) error { var err error; raw, err = smallFile(r, rel); return err })
			return panelPage{}, err
		}, func(panelPage) tea.Cmd {
			e := &codeEditor{document: loadDocument(string(raw)), path: rel, profile: p, lexer: fileLexer(rel),
				readonly: protected(rel) || m.owner == p.dir && (m.process != nil || m.starting || m.restarting)}
			m.editor = e
			return e.highlight()
		})
	}
}
func (e *codeEditor) highlight() tea.Cmd {
	if e.highlightBusy || e.syntax.owner == e && e.syntax.version == e.version {
		return nil
	}
	e.highlightBusy = true
	text, v, l := e.text, e.version, e.lexer
	return func() tea.Msg { return scanSyntax(e, v, text, l) }
}
func (m *model) leaveCode() tea.Cmd {
	m.editor = nil
	m.failure, m.status = "", ""
	if err := m.refreshLibrary(""); err != nil {
		m.report(err)
		return nil
	}
	return m.reload()
}
func (m *model) saveCode(closeAfter bool) tea.Cmd {
	e := m.editor
	if e.readonly {
		e.notice = "Файл открыт для чтения"
		return nil
	}
	before, after := []byte(e.bom+e.saved), []byte(e.bom+e.text)
	e.mode = ""
	return m.schedule("Сохранение…", func(context.Context) (panelPage, error) {
		return panelPage{}, saveEditedFile(e.profile, e.path, before, after)
	}, func(panelPage) tea.Cmd {
		e.saved = e.text
		e.mode = ""
		e.notice = "Сохранено в " + time.Now().Format("15:04")
		if closeAfter {
			return m.leaveCode()
		}
		return nil
	})
}
func (e *codeEditor) prompt(mode, value string) tea.Cmd {
	e.mode = mode
	e.notice = ""
	f := textinput.New()
	f.Prompt = ""
	f.CharLimit = 1024
	f.SetValue(value)
	e.field = f
	return e.field.Focus()
}
func (e *codeEditor) search(back bool) {
	if !e.find(e.query, back) {
		e.notice = "Совпадений нет"
	} else {
		e.notice = "F3 — следующее совпадение"
	}
}
func (e *codeEditor) submitPrompt() tea.Cmd {
	value := e.field.Value()
	switch e.mode {
	case "goto":
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 1 || n > len(e.lines) {
			e.notice = fmt.Sprintf("Введите номер от 1 до %d", len(e.lines))
			return nil
		}
		e.move(e.lines[n-1], false)
		e.preferred = -1
	case "search", "needle":
		if value == "" {
			e.notice = "Введите текст"
			return nil
		}
		e.query = value
		a, b := e.bounds()
		if e.part(a, b) != value && !e.find(value, false) {
			e.notice = "Совпадений нет"
			return nil
		}
		if e.mode == "needle" {
			return e.prompt("replacement", "")
		}
	case "replacement":
		a, b := e.bounds()
		if e.part(a, b) == e.query {
			if err := e.insert(value); err != nil {
				e.notice = err.Error()
				return nil
			}
		}
		e.notice = "Совпадение заменено"
	}
	e.mode = ""
	return nil
}
func (e *codeEditor) indent(out bool) error {
	a, b := e.bounds()
	if !out && a == b {
		return e.insert("\t")
	}
	first, last := e.row(a), e.row(b)
	if b > a && e.lines[last] == b {
		last--
	}
	start, end := e.lines[first], e.lineEnd(last)
	before := e.bytesSelection()
	oldWidth := e.glyphs[end].offset - e.glyphs[start].offset
	var result strings.Builder
	for r := first; r <= last; r++ {
		line := e.part(e.lines[r], e.lineEnd(r))
		if out {
			if strings.HasPrefix(line, "\t") {
				line = line[1:]
			} else {
				spaces := len(line) - len(strings.TrimLeft(line, " "))
				line = line[min(tabSize, spaces):]
			}
		} else {
			result.WriteByte('\t')
		}
		result.WriteString(line)
		if r < last {
			result.WriteString(e.part(e.lineEnd(r), e.lines[r+1]))
		}
	}
	if result.String() == e.part(start, end) {
		return nil
	}
	if err := e.replace(start, end, result.String()); err != nil {
		return err
	}
	if a != b {
		e.anchor = start
		if before.anchor > before.caret {
			e.anchor, e.caret = e.caret, e.anchor
		}
	} else {
		e.move(e.atByte(max(e.glyphs[start].offset, before.caret-oldWidth+result.Len())), false)
	}
	e.undo[len(e.undo)-1].after = e.bytesSelection()
	return nil
}
func (m *model) editorKey(k tea.KeyMsg) tea.Cmd {
	e := m.editor
	if m.panel.done != nil {
		return nil
	}
	key := k.String()
	if e.mode == "close" {
		switch key {
		case "esc":
			e.mode = ""
		case "d", "D", "д", "Д":
			return m.leaveCode()
		case "s", "S", "ы", "Ы", "ctrl+s":
			return m.saveCode(true)
		}
		return nil
	}
	if e.mode == "help" {
		switch key {
		case "down", "pgdown":
			e.helpTop = min(max(0, len(editorHelp)-max(1, m.height-5)), e.helpTop+1)
		case "up", "pgup":
			e.helpTop = max(0, e.helpTop-1)
		default:
			e.mode = ""
		}
		return nil
	}
	if e.mode == "outline" {
		switch key {
		case "esc", "f4":
			e.mode = ""
		case "up":
			e.outlineIndex = max(0, e.outlineIndex-1)
		case "down":
			e.outlineIndex = min(max(0, len(e.syntax.symbols)-1), e.outlineIndex+1)
		case "enter":
			if e.outlineIndex < len(e.syntax.symbols) {
				r := e.syntax.symbols[e.outlineIndex].row
				e.move(e.lines[min(r, len(e.lines)-1)], false)
				e.preferred = -1
			}
			e.mode = ""
		}
		return nil
	}
	if e.mode != "" {
		switch key {
		case "esc":
			e.mode = ""
			e.notice = ""
			return nil
		case "enter":
			return e.submitPrompt()
		}
		var cmd tea.Cmd
		e.field, cmd = e.field.Update(k)
		return cmd
	}
	m.failure = ""
	e.notice = ""
	a, b := e.bounds()
	switch key {
	case "esc", "ctrl+w":
		if e.text != e.saved {
			e.mode = "close"
			return nil
		}
		return m.leaveCode()
	case "ctrl+s":
		return m.saveCode(false)
	case "f1":
		e.mode = "help"
		e.helpTop = 0
		return nil
	case "ctrl+f", "ctrl+r":
		query := e.query
		if a < b && b-a < 256 && e.row(a) == e.row(b) {
			query = e.part(a, b)
		}
		if key == "ctrl+r" {
			if e.readonly {
				e.notice = "Файл открыт для чтения"
				return nil
			}
			return e.prompt("needle", query)
		}
		return e.prompt("search", query)
	case "ctrl+g":
		return e.prompt("goto", "")
	case "f3", "alt+n":
		e.search(false)
		return nil
	case "alt+p":
		e.search(true)
		return nil
	case "f4":
		if e.syntax.version != e.version || e.syntax.owner != e {
			e.notice = "Структура обновляется…"
			return e.highlight()
		}
		e.mode = "outline"
		e.outlineIndex = e.activeSymbol()
		return nil
	case "ctrl+a":
		e.anchor, e.caret = 0, e.end()
		return nil
	case "ctrl+c", "ctrl+x":
		if a == b {
			e.notice = "Выделите текст с Shift"
			return nil
		}
		m.editorClip = e.part(a, b)
		if key == "ctrl+x" && !e.readonly {
			_ = e.replace(a, b, "")
		}
		text := m.editorClip
		e.notice = "Скопировано"
		return func() tea.Msg { _ = clipboard.WriteAll(text); return nil }
	}
	extend := strings.Contains(key, "shift+")
	movement := strings.Replace(key, "shift+", "", 1)
	row := e.row(e.caret)
	switch movement {
	case "up", "down", "pgup", "pgdown":
		delta := 1
		if strings.HasPrefix(movement, "pg") {
			delta = max(1, m.height-6)
		}
		if movement == "up" || movement == "pgup" {
			delta = -delta
		}
		e.vertical(delta, extend)
		return nil
	case "left", "right", "home", "end", "ctrl+home", "ctrl+end", "ctrl+left", "ctrl+right":
		pos := e.caret
		switch movement {
		case "left":
			pos--
			if !extend && a != b {
				pos = a
			}
		case "right":
			pos++
			if !extend && a != b {
				pos = b
			}
		case "home":
			pos = e.lines[row]
		case "end":
			pos = e.lineEnd(row)
		case "ctrl+home":
			pos = 0
		case "ctrl+end":
			pos = e.end()
		case "ctrl+left":
			pos = e.word(pos, -1)
		case "ctrl+right":
			pos = e.word(pos, 1)
		}
		e.move(pos, extend)
		e.preferred = -1
		return nil
	}
	if e.readonly {
		e.notice = "Файл открыт для чтения"
		return nil
	}
	var err error
	switch key {
	case "ctrl+z", "ctrl+y":
		e.history(key == "ctrl+z")
	case "ctrl+v":
		version, sel, fallback := e.version, e.selection, m.editorClip
		return func() tea.Msg {
			text, err := clipboard.ReadAll()
			if err != nil && fallback != "" {
				text, err = fallback, nil
			}
			return pasteReady{e, version, sel, text, err}
		}
	case "backspace", "ctrl+h":
		if a == b {
			a = max(0, a-1)
		}
		err = e.replace(a, b, "")
	case "delete":
		if a == b {
			b = min(e.end(), b+1)
		}
		err = e.replace(a, b, "")
	case "enter":
		prefix := e.part(e.lines[e.row(a)], a)
		indent := prefix[:len(prefix)-len(strings.TrimLeft(prefix, " \t"))]
		err = e.insert(e.lineBreak() + indent)
	case "tab", "shift+tab":
		err = e.indent(key == "shift+tab")
	case " ":
		err = e.insert(" ")
	default:
		if k.Type == tea.KeyRunes && !k.Alt {
			s := string(k.Runes)
			if k.Paste {
				s = e.pasteText(s)
			}
			err = e.insert(s)
		}
	}
	if err != nil {
		e.notice = err.Error()
	}
	return e.highlight()
}
func (e *codeEditor) pasteText(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.ReplaceAll(s, "\n", e.lineBreak())
}
func (e *codeEditor) activeSymbol() int {
	row := e.row(e.caret)
	next := sort.Search(len(e.syntax.symbols), func(i int) bool { return e.syntax.symbols[i].row > row })
	return max(0, next-1)
}

func (m *model) editorFieldMsg(msg tea.Msg) tea.Cmd {
	switch m.editor.mode {
	case "search", "goto", "needle", "replacement":
		var cmd tea.Cmd
		m.editor.field, cmd = m.editor.field.Update(msg)
		return cmd
	}
	return nil
}
