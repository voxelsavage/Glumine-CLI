package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestJavaChoiceInCompactForm(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "jdk", "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	java := filepath.Join(bin, javaFilename())
	if err := os.WriteFile(java, nil, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JAVA_HOME", root)
	m := newModel(context.Background(), serverStore{}, nil, "")
	m.Update(tea.WindowSizeMsg{Width: 36, Height: 13})
	m.listKey("n")
	body := m.creator()
	if last := ansi.Strip(body[len(body)-1]); !strings.Contains(last, "Выбрать Java") {
		t.Fatal(last)
	}
	press := func(k tea.KeyType) { m.createKey(tea.KeyMsg{Type: k}) }
	press(tea.KeyUp)
	press(tea.KeyEnter)
	if m.javaMenu == nil || m.javaMenu.manual {
		t.Fatal("auto should be selected")
	}
	press(tea.KeyDown)
	press(tea.KeyEnter)
	press(tea.KeyDown) // parent directory
	press(tea.KeyDown) // jdk
	press(tea.KeyEnter)
	if m.javaMenu != nil || m.javaMode != javaManual || m.javaPath != java {
		t.Fatalf("manual selection: %q", m.javaPath)
	}
	m.focusField(javaField)
	press(tea.KeyEnter)
	press(tea.KeyEnter)
	m.javaMenu.path.SetValue(filepath.Join(root, "missing"))
	press(tea.KeyEnter)
	if m.javaMenu == nil || m.failure == "" {
		t.Fatal("missing executable accepted")
	}
	press(tea.KeyEsc)
	press(tea.KeyEsc)
	if m.javaPath != java {
		t.Fatal("cancel discarded saved path")
	}
	press(tea.KeyEnter)
	press(tea.KeyUp)
	press(tea.KeyEnter)
	if m.javaMenu != nil || m.javaMode != javaAuto || m.javaPath != "" {
		t.Fatal("old path survived automatic choice")
	}
}
