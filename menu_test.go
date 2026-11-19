package main

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestStartRequiresConsentForThisServer(t *testing.T) {
	p := profile{Name: "Мир", dir: t.TempDir()}
	m := newModel(context.Background(), serverStore{}, []profile{p}, "")
	m.listKey("enter")
	for _, key := range []tea.KeyType{tea.KeyEsc, tea.KeyEnter} {
		if cmd := m.consoleKey(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil || !m.eulaOpen || m.eulaAccept {
			t.Fatal("start skipped consent")
		}
		m.consoleKey(tea.KeyMsg{Type: key})
		if m.eulaOpen || m.starting {
			t.Fatal("cancel started a server")
		}
		if yes, err := hasEULA(p.dir); err != nil || yes {
			t.Fatal("cancel saved consent")
		}
	}
	m.start()
	m.eulaKey("down")
	if cmd := m.eulaKey("enter"); cmd == nil || !m.starting {
		t.Fatal("acceptance did not resume start")
	}
	if yes, err := hasEULA(p.dir); err != nil || !yes {
		t.Fatal("acceptance was not saved")
	}
	m.starting = false
	if cmd := m.start(); cmd == nil || m.eulaOpen {
		t.Fatal("existing consent ignored")
	}
	if yes, err := hasEULA(t.TempDir()); err != nil || yes {
		t.Fatal("consent leaked between profiles")
	}
}

func TestMenuDependencies(t *testing.T) {
	m := newModel(context.Background(), serverStore{}, nil, "")
	m.listKey("n")
	choose := func(field int, value string) {
		m.focusField(field)
		m.picker = &choiceMenu{options: []string{value}}
		m.pickerKey("enter")
	}
	choose(versionField, "1.21.1")
	choose(coreField, "Purpur")
	if m.fields[buildField].Value() != "" {
		t.Fatal("Vanilla build reused for Purpur")
	}
	choose(buildField, "2329")
	choose(versionField, "1.20.4")
	if m.fields[coreField].Value() != "Vanilla" || m.fields[buildField].Value() != "1.20.4" {
		t.Fatal("incompatible selection survived version change")
	}
	m.focusField(offlineField)
	m.createKey(tea.KeyMsg{Type: tea.KeySpace})
	if !m.offline {
		t.Fatal("offline switch ignored")
	}
}
