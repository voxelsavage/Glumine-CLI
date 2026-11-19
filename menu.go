package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const purpurURL = "https://api.purpurmc.org/v2/purpur"

const (
	nameField = iota
	versionField
	coreField
	buildField
	offlineField
	portField
	memoryField
	submitField
	javaField
	formCount
)

var fieldNames = [formCount]string{"Название", "Версия Minecraft", "Ядро", "Сборка ядра", "Пиратский режим", "Порт", "Память, МиБ", "Создать сервер", "Выбрать Java"}

func editable(field int) bool {
	return field == nameField || field == portField || field == memoryField
}

type choiceMenu struct {
	options []string
	index   int
	loading bool
	cancel  context.CancelFunc
}
type choicesLoaded struct {
	menu    *choiceMenu
	options []string
	err     error
}

func catalog(ctx context.Context, field int, version, core string) ([]string, error) {
	switch field {
	case versionField:
		var list releaseManifest
		if err := fetchJSON(ctx, manifestURL, &list); err != nil {
			return nil, err
		}
		var releases []string
		for _, r := range list.Versions {
			if r.Type == "release" {
				releases = append(releases, r.ID)
			}
		}
		return releases, nil
	case coreField:
		return coreOptions(ctx, version)
	case buildField:
		if core == "Vanilla" {
			return []string{version}, nil
		}
		if core == "Quilt" {
			return quiltBuilds(ctx, version)
		}
		if installerCore(core) {
			return forgeBuilds(ctx, version, core)
		}
		if core == "Paper" || core == "Fabric" {
			return extraBuilds(ctx, version, core)
		}
		var catalog struct{ Builds struct{ All []string } }
		if err := fetchJSON(ctx, purpurURL+"/"+url.PathEscape(version), &catalog); err != nil {
			return nil, err
		}
		slices.Reverse(catalog.Builds.All)
		return catalog.Builds.All, nil
	}
	return nil, errors.New("неизвестный каталог")
}

func (m *model) openPicker() tea.Cmd {
	field := m.field
	version, core := m.fields[versionField].Value(), m.fields[coreField].Value()
	if field != versionField && version == "" {
		m.report(errors.New("сначала выберите версию Minecraft"))
		return nil
	}
	ctx, cancel := context.WithTimeout(m.ctx, 20*time.Second)
	menu := &choiceMenu{loading: true, cancel: cancel}
	m.picker, m.failure = menu, ""
	return func() tea.Msg {
		defer cancel()
		options, err := catalog(ctx, field, version, core)
		return choicesLoaded{menu, options, err}
	}
}
func (m *model) pickerKey(key string) tea.Cmd {
	menu := m.picker
	maximum := max(0, len(menu.options)-1)
	switch key {
	case "esc":
		menu.cancel()
		m.picker = nil
		m.failure = ""
	case "up":
		menu.index = max(0, menu.index-1)
	case "down":
		menu.index = min(maximum, menu.index+1)
	case "pgup":
		menu.index = max(0, menu.index-m.choicePage())
	case "pgdown":
		menu.index = min(maximum, menu.index+m.choicePage())
	case "home":
		menu.index = 0
	case "end":
		menu.index = maximum
	case "enter":
		if menu.loading {
			return nil
		}
		if len(menu.options) == 0 {
			return m.openPicker()
		}
		value := menu.options[menu.index]
		old := m.fields[m.field].Value()
		m.fields[m.field].SetValue(value)
		if value != old {
			switch m.field {
			case versionField:
				m.fields[coreField].SetValue("Vanilla")
				m.fields[buildField].SetValue(value)
			case coreField:
				build := ""
				if value == "Vanilla" {
					build = m.fields[versionField].Value()
				}
				m.fields[buildField].SetValue(build)
			}
		}
		m.picker = nil
		m.failure = ""
		return m.focusField(m.field + 1)
	}
	return nil
}
func hasEULA(dir string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, "eula.txt"))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	accepted := false
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == "eula" {
			accepted = strings.EqualFold(strings.TrimSpace(value), "true")
		}
	}
	return accepted, nil
}
func (m *model) eulaKey(key string) tea.Cmd {
	switch key {
	case "esc":
		m.eulaOpen = false
	case "tab", "shift+tab", "up", "down", "left", "right":
		m.eulaAccept = !m.eulaAccept
	case "enter":
		if !m.eulaAccept {
			m.eulaOpen = false
			return nil
		}
		err := os.WriteFile(filepath.Join(m.profiles[m.cursor].dir, "eula.txt"), []byte("# "+eulaURL+"\neula=true\n"), 0600)
		if err != nil {
			m.report(err)
			return nil
		}
		m.eulaOpen = false
		return m.start()
	}
	return nil
}
