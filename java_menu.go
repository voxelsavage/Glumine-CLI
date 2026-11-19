package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type javaDialog struct {
	manual, browse bool
	path           textinput.Model
	dir            string
	names          []string
	row            int // -1 selects the editable path
}

func (m *model) openJava() {
	input := textinput.New()
	input.Prompt = "› "
	input.CharLimit = 4096
	input.Width = max(8, m.formWidth()-5)
	input.Placeholder = "Путь к Java или папке JDK"
	input.SetValue(m.javaPath)
	m.javaMenu = &javaDialog{manual: m.javaMode == javaManual, path: input, row: -1}
	m.failure = ""
}

func (d *javaDialog) list(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	names := []string{".."}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		isDir := e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			info, err := os.Stat(path)
			isDir = err == nil && info.IsDir()
		}
		if isDir {
			names = append(names, e.Name()+string(os.PathSeparator))
		} else if strings.EqualFold(e.Name(), javaFilename()) {
			names = append(names, e.Name())
		}
	}
	d.dir, d.names, d.row = dir, names, -1
	return nil
}

func manualJavaPath(input, base string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", errors.New("укажите путь к Java")
	}
	if s == "~" || strings.HasPrefix(s, "~/") || strings.HasPrefix(s, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		s = filepath.Join(home, strings.TrimLeft(s[1:], `/\`))
	}
	if !filepath.IsAbs(s) {
		s = filepath.Join(base, s)
	}
	return filepath.Abs(s)
}

func (m *model) javaKey(key tea.KeyMsg) tea.Cmd {
	d := m.javaMenu
	if !d.browse {
		switch key.String() {
		case "esc":
			m.javaMenu = nil
			m.failure = ""
		case "up", "down", "tab", "shift+tab":
			d.manual = !d.manual
		case "enter":
			if !d.manual {
				m.javaMode, m.javaPath = javaAuto, ""
				m.javaMenu = nil
				m.failure = ""
				return m.focusField(submitField)
			}
			dir, _ := os.UserHomeDir()
			if m.javaPath != "" {
				dir = filepath.Dir(m.javaPath)
			} else if home := os.Getenv("JAVA_HOME"); home != "" {
				dir = home
			}
			d.browse = true
			if err := d.list(dir); err != nil {
				m.report(err)
			}
			return d.path.Focus()
		}
		return nil
	}
	switch key.String() {
	case "esc":
		d.browse = false
		m.failure = ""
		return nil
	case "up", "down", "tab", "shift+tab":
		if key.String() == "up" || key.String() == "shift+tab" {
			d.row = max(-1, d.row-1)
		} else {
			d.row = min(len(d.names)-1, d.row+1)
		}
		if d.row < 0 {
			return d.path.Focus()
		}
		d.path.Blur()
		return nil
	case "pgdown":
		d.row = min(len(d.names)-1, d.row+m.javaPage())
		d.path.Blur()
		return nil
	case "pgup":
		d.row = max(-1, d.row-m.javaPage())
		if d.row < 0 {
			return d.path.Focus()
		}
		return nil
	case "enter":
		raw := d.path.Value()
		if d.row >= 0 {
			raw = d.names[d.row]
		}
		path, err := manualJavaPath(raw, d.dir)
		if err != nil {
			m.report(err)
			return nil
		}
		info, err := os.Stat(path)
		if err != nil {
			m.report(err)
			return nil
		}
		if info.IsDir() {
			found := false
			for _, bin := range []string{"bin", "Contents/Home/bin"} {
				candidate := filepath.Join(path, bin, javaFilename())
				if stat, err := os.Stat(candidate); err == nil && stat.Mode().IsRegular() {
					path, info, found = candidate, stat, true
					break
				}
			}
			if !found {
				if err := d.list(path); err != nil {
					m.report(err)
					return nil
				}
				d.path.Reset()
				m.failure = ""
				return d.path.Focus()
			}
		}
		if !info.Mode().IsRegular() || runtime.GOOS != "windows" && info.Mode().Perm()&0111 == 0 {
			m.report(errors.New("выберите исполняемый файл Java"))
			return nil
		}
		m.javaMode, m.javaPath = javaManual, path
		m.javaMenu = nil
		m.failure = ""
		return m.focusField(submitField)
	default:
		if d.row == -1 {
			var cmd tea.Cmd
			d.path, cmd = d.path.Update(key)
			return cmd
		}
	}
	return nil
}

func (m *model) javaPage() int { return max(1, m.bodyHeight()-5) }
func (m *model) javaView(w int) []string {
	d := m.javaMenu
	rows := []string{heading("Выбрать Java"), ""}
	if !d.browse {
		for i, name := range []string{"Скачать автоматически", "Выбрать путь к Java"} {
			bg, mark := panel, "  "
			if (i == 1) == d.manual {
				bg, mark = "#234C83", tint("▌ ", "#A8D5FF", true)
			}
			rows = append(rows, surface(mark+name, w, bg))
		}
		return rows
	}
	rows = append(rows, surface(d.path.View(), w, selected), label(oneLine(d.dir)), "")
	n := m.javaPage()
	first := max(0, d.row) / n * n
	for i := first; i < min(first+n, len(d.names)); i++ {
		bg, mark := panel, "  "
		if i == d.row {
			bg, mark = selected, "› "
		}
		rows = append(rows, surface(mark+oneLine(d.names[i]), w, bg))
	}
	return rows
}
