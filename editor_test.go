package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestDocumentPreservesBytesAndGraphemes(t *testing.T) {
	raw := "\ufeff\tе\u0301🐈‍⬛\r\nраз\n\rпосле"
	d := loadDocument(raw)
	if d.bom+d.text != raw || d.endings != "MIX" || len(d.lines) != 4 || d.lineBreak() != "\r\n" {
		t.Fatalf("import: %q %s %v", d.text, d.endings, d.lines)
	}
	if d.part(1, 2) != "е\u0301" || d.part(2, 3) != "🐈‍⬛" || d.glyphs[2].column != 5 || d.part(3, 4) != "\r\n" {
		t.Fatal("grapheme or tab boundaries")
	}
	d.selection = selection{3, 1}
	if err := d.insert("мир"); err != nil {
		t.Fatal(err)
	}
	edited := d.text
	d.history(true)
	if d.bom+d.text != raw || d.selection != (selection{3, 1}) {
		t.Fatal("undo")
	}
	d.history(false)
	if d.text != edited {
		t.Fatal("redo")
	}
	if err := d.insert(strings.Repeat("z", editorLimit)); err == nil || d.text != edited {
		t.Fatal("size limit")
	}
}

func TestEditorSearchReplaceIndentAndLayout(t *testing.T) {
	e := &codeEditor{document: loadDocument("alpha:\r\n  name: кот\n  info: кот\nbeta:\n" + strings.Repeat("界", 110)), path: "config.yml", lexer: fileLexer("config.yml")}
	if !e.find("кот", false) {
		t.Fatal("find")
	}
	first := e.selection
	if !e.find("кот", false) || e.selection == first || !e.find("кот", false) || e.selection != first {
		t.Fatal("wrap forward")
	}
	if !e.find("кот", true) || e.selection == first {
		t.Fatal("wrap back")
	}
	e.prompt("needle", "кот")
	e.submitPrompt()
	if e.mode != "replacement" {
		t.Fatal("replace prompt")
	}
	e.field.SetValue("тест")
	e.submitPrompt()
	if !strings.Contains(e.text, "info: тест") || !strings.Contains(e.text, "name: кот") {
		t.Fatal(e.text)
	}
	e.selection = selection{e.lines[3], e.lines[1]}
	if err := e.indent(false); err != nil {
		t.Fatal(err)
	}
	after := e.selection
	if !strings.Contains(e.text, "\t  name:") || e.anchor <= e.caret {
		t.Fatal("indent selection")
	}
	e.history(true)
	e.history(false)
	if e.selection != after {
		t.Fatal("redo selection")
	}
	e.syntax = scanSyntax(e, e.version, e.text, e.lexer)
	if len(e.syntax.spans) < 3 || e.syntax.spans[len(e.syntax.spans)-1].end != len(e.text) || len(e.syntax.symbols) != 2 {
		t.Fatalf("syntax: %d spans, %#v", len(e.syntax.spans), e.syntax.symbols)
	}
	e.move(e.end(), false)
	for _, size := range [][2]int{{104, 28}, {80, 18}, {36, 9}} {
		rows := strings.Split(e.view(size[0], size[1], ""), "\n")
		if len(rows) != size[1] {
			t.Fatalf("height %d", len(rows))
		}
		for _, line := range rows {
			if n := ansi.StringWidth(line); n != size[0] {
				t.Fatalf("width %d, want %d: %q", n, size[0], line)
			}
		}
	}
}

func TestEditorSaveConflictAndReadonly(t *testing.T) {
	m, p := fixtureManager(t)
	m.listKey("f2")
	path := filepath.Join(p.dir, "config.yml")
	raw := "\ufeff\tname: да\r\nworld: one\n"
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	run := func(cmd tea.Cmd) {
		for cmd != nil {
			_, cmd = m.Update(cmd())
		}
	}
	write(raw)
	run(openCode(p, "config.yml")(m))
	e := m.editor
	e.move(e.end(), false)
	run(m.editorKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}))
	run(m.saveCode(false))
	b, err := os.ReadFile(path)
	if err != nil || string(b) != raw+"x" || e.saved != e.text {
		t.Fatal("save bytes", err)
	}
	write("external")
	run(m.editorKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft")}))
	m.editorKey(tea.KeyMsg{Type: tea.KeyEsc})
	if e.mode != "close" {
		t.Fatal("missing unsaved prompt")
	}
	run(m.editorKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}))
	b, _ = os.ReadFile(path)
	if string(b) != "external" || m.failure == "" || e.mode != "" || m.editor != e || !strings.HasSuffix(e.text, "draft") {
		t.Fatal("conflicting edit was lost")
	}
	run(openCode(p, "profile.json")(m))
	if !m.editor.readonly {
		t.Fatal("managed file writable")
	}
	original := m.editor.text
	run(m.editorKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("edit")}))
	if m.editor.text != original {
		t.Fatal("read-only input")
	}
}
