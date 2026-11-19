package main

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestScreenFitsTerminal(t *testing.T) {
	for _, size := range [][2]int{{36, 13}, {48, 13}, {64, 18}, {80, 24}, {86, 20}, {112, 28}} {
		m := newModel(context.Background(), serverStore{}, nil, "")
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		check := func(want string) {
			t.Helper()
			v := ansi.Strip(m.View())
			lines := strings.Split(v, "\n")
			if len(lines) != size[1] || !strings.Contains(v, want) {
				t.Fatalf("%v missing %q or incorrect height\n%s", size, want, v)
			}
			for _, line := range lines {
				if lipgloss.Width(line) != size[0] {
					t.Fatalf("%v incorrect width: %q", size, line)
				}
			}
		}
		check("Создать")
		m.listKey("n")
		check("Сборка ядра")
		p := profile{Name: strings.Repeat("Большой мир ", 12), Version: "1.21.1", Java: 21, Memory: 2048, Port: 25565, dir: "world"}
		m.profiles = []profile{p}
		m.screen = listScreen
		check("Остановлен")
		m.listKey("enter")
		check("журнал сервера")
	}
}
