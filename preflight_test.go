package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestLibraryRefreshWithBrokenProfile(t *testing.T) {
	m, p := fixtureManager(t)
	dir := filepath.Join(m.store.root, "broken")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "profile.json")
	if err := os.WriteFile(file, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	m.listKey("r")
	if len(m.profiles) != 1 || m.profiles[0].dir != p.dir || !strings.Contains(m.View(), "broken") {
		t.Fatal("broken profile blocked the library or warning was lost")
	}
	b, err := os.ReadFile(filepath.Join(p.dir, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, b, 0600); err != nil {
		t.Fatal(err)
	}
	m.listKey("r")
	if len(m.profiles) != 2 || m.profiles[m.cursor].dir != p.dir || m.libraryWarning != "" {
		t.Fatal("repair or selection was not preserved")
	}
}

func TestMissingProfileOnEditorClose(t *testing.T) {
	m, p := fixtureManager(t)
	m.listKey("f2")
	_, _ = m.Update(openCode(p, "profile.json")(m)())
	if m.editor == nil {
		t.Fatal("failed to open file")
	}
	if err := os.Rename(p.dir, filepath.Join(t.TempDir(), "moved")); err != nil {
		t.Fatal(err)
	}
	m.handleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != listScreen || m.editor != nil || m.failure == "" || len(m.profiles) != 0 {
		t.Fatal("stale panel")
	}
	_ = m.View()
}

func TestStoppedServerRejectsCommands(t *testing.T) {
	p := &serverProcess{input: make(chan string, 16), done: make(chan struct{}), stopping: make(chan struct{})}
	close(p.stopping)
	for i := 0; i < 32; i++ {
		if err := p.send("list"); err == nil {
			t.Fatal("command accepted after stop")
		}
	}
}

func TestConsoleProcessLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell")
	}
	m, _ := fixtureManager(t)
	path := filepath.Join(t.TempDir(), "java")
	script := "#!/bin/sh\nif [ \"$1\" = -version ]; then echo 'openjdk version \"21\"' >&2; exit; fi\nwhile IFS= read -r cmd; do echo \"received: $cmd\"; [ \"$cmd\" = stop ] && exit 0; done\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	m.profiles[0].Java = 21
	m.java = path
	m.listKey("enter")
	m.start()
	if !m.eulaOpen {
		t.Fatal("missing first-start confirmation")
	}
	m.eulaKey("down")
	cmd := m.eulaKey("enter")
	if cmd == nil {
		t.Fatal("did not prepare runtime")
	}
	m.Update(cmd())
	proc := m.process
	if proc == nil {
		t.Fatal(m.failure)
	}
	t.Cleanup(func() { proc.stop(); <-proc.done })
	m.command.SetValue("list")
	m.consoleKey(tea.KeyMsg{Type: tea.KeyEnter})
	deadline := time.Now().Add(3 * time.Second)
	for {
		log, _ := proc.log.snapshot(0)
		if strings.Contains(log, "received: list") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("command not delivered")
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.consoleKey(tea.KeyMsg{Type: tea.KeyCtrlS})
	select {
	case <-proc.done:
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not reach server")
	}
	m.refreshProcess()
	if m.process != nil || !strings.Contains(m.logText, "received: stop") || proc.err != nil {
		t.Fatal("incomplete shutdown", m.logText, proc.err)
	}
}
