package main

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	initializeLocale()
	os.Exit(m.Run())
}

func TestSystemLocalePrecedence(t *testing.T) {
	for _, c := range []struct {
		all, msg, lang, native string
		ru                     bool
	}{
		{"", "ru_RU.UTF-8", "en_US", "", true},
		{"C", "ru", "ru_RU", "ru", false},
		{"", "", "ru-RU", "en", true},
		{"", "", "", "ru", true},
		{"", "", "", "de", false},
		{"", "", "", "", false},
	} {
		env := map[string]string{"LC_ALL": c.all, "LC_MESSAGES": c.msg, "LANG": c.lang}
		got := russianLocale(func(k string) string { return env[k] }, func() string { return c.native })
		if got != c.ru {
			t.Errorf("%+v: got %t", c, got)
		}
	}
}

func TestLocalizedDashboardPreservesProfileName(t *testing.T) {
	m, p := fixtureManager(t)
	// A user can give a profile the same name as a menu item.
	p.Name = "Настройки"
	m.serverPanel(p)
	want := "Settings"
	if russianUI {
		want = "Настройки"
	}
	if got := m.page().rows[7].title; got != want {
		t.Errorf("settings label = %q, want %q", got, want)
	}
	if got := m.page().title; got != p.Name {
		t.Errorf("profile name changed to %q", got)
	}
}
