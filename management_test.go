package main

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func fixtureManager(t *testing.T) (*model, profile) {
	t.Helper()
	store := serverStore{root: filepath.Join(t.TempDir(), "servers")}
	p := profile{Name: "Тестовый мир", Version: "1.21.1", Core: "Purpur", Build: "2329", Memory: 1024, Port: 25565, dir: filepath.Join(store.root, "sample")}
	if err := os.MkdirAll(p.dir, 0700); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(p.dir, "profile.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	return newModel(context.Background(), store, []profile{p}, ""), p
}

func TestPanelEditBackupTrashRestore(t *testing.T) {
	m, p := fixtureManager(t)
	m.listKey("f2")
	if len(m.page().rows) != 8 {
		t.Fatal("server sections")
	}
	run := func(cmd tea.Cmd) {
		t.Helper()
		for cmd != nil && m.panel.done != nil {
			_, cmd = m.Update(cmd())
		}
		if m.failure != "" {
			t.Fatal(m.failure)
		}
	}
	open := func(title string) {
		t.Helper()
		for _, r := range m.page().rows {
			if r.title == title {
				run(r.open(m))
				return
			}
		}
		t.Fatalf("missing %s on %s", title, m.page().title)
	}
	back := func() { run(m.panelKey(tea.KeyMsg{Type: tea.KeyEsc})) }
	submit := func(value string) {
		t.Helper()
		if m.panel.prompt == nil {
			t.Fatal("missing prompt")
		}
		run(m.panel.prompt.submit(value))
	}
	open("Файлы")
	open("+ Файл")
	submit("notes.txt")
	open("notes.txt")
	run(m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("первая строка\nвторая\n"), Paste: true}))
	run(m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlS}))
	data, err := os.ReadFile(filepath.Join(p.dir, "notes.txt"))
	if err != nil || string(data) != "первая строка\nвторая\n" {
		t.Fatal("write", err)
	}
	run(m.handleKey(tea.KeyMsg{Type: tea.KeyEsc}))
	back()
	open("Резервные копии")
	open("+ Создать ZIP")
	archives, _ := filepath.Glob(filepath.Join(backupDir(p, m.store), "*.zip"))
	if len(archives) != 1 {
		t.Fatal(archives)
	}
	z, err := zip.OpenReader(archives[0])
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	if _, err = z.Open("notes.txt"); err != nil {
		t.Fatal(err)
	}
	back()
	open("Настройки")
	open("Максимум памяти")
	submit("3072")
	if m.profiles[0].Memory != 3072 {
		t.Fatal("RAM not persisted")
	}
	open("В корзину")
	submit("")
	if len(m.profiles) != 0 || m.screen != listScreen {
		t.Fatal("trash")
	}
	run(m.listKey("t"))
	open(p.Name)
	open("Восстановить сервер")
	if len(m.profiles) != 1 {
		t.Fatal("restore")
	}
	if _, err := os.Stat(filepath.Join(p.dir, "notes.txt")); err != nil {
		t.Fatal(err)
	}
}
func TestDatabaseViewerReadsAndPaginates(t *testing.T) {
	m, p := fixtureManager(t)
	file := filepath.Join(p.dir, "players.db")
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("CREATE TABLE players(name TEXT); WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<103) INSERT INTO players SELECT 'player '||x FROM n")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	page, err := databaseList(p)(m.ctx)
	if err != nil || len(page.rows) != 2 {
		t.Fatal(err)
	}
	page, err = tableList(file)(m.ctx)
	if err != nil || page.rows[1].title != "players" {
		t.Fatal(err, page)
	}
	for _, q := range []string{"DELETE FROM players", "SELECT 1; SELECT 2", "WITH data AS (SELECT 1) UPDATE players SET name='x'"} {
		if _, err := readonlySQL(q); err == nil {
			t.Fatal(q)
		}
	}
	page, err = queryPage(file, "players", "SELECT name FROM players", 0)(m.ctx)
	if err != nil || len(page.rows) != 101 {
		t.Fatal("page 1", err, len(page.rows))
	}
	page, err = queryPage(file, "players", "SELECT name FROM players", 100)(m.ctx)
	if err != nil || len(page.rows) != 3 || !strings.Contains(page.rows[0].title, "101") {
		t.Fatal("page 2", err)
	}
}
func TestManagedFilesStayInsideProfile(t *testing.T) {
	_, p := fixtureManager(t)
	if validName("../note") == nil || validName(`a\b`) == nil {
		t.Fatal("relative names")
	}
	out := t.TempDir()
	if err := os.Symlink(out, filepath.Join(p.dir, "outside")); err != nil {
		t.Skip(err)
	}
	err := inRoot(p.dir, func(r *os.Root) error { return atomicFile(r, "outside/escaped", []byte("abc"), 0600) })
	if err == nil {
		t.Fatal("escaped profile")
	}
}
