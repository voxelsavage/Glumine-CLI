package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"modernc.org/sqlite"
)

func databaseList(p profile) pageLoader {
	return func(ctx context.Context) (panelPage, error) {
		page := panelPage{title: "Базы данных", description: "SQLite · только чтение"}
		page.rows = append(page.rows, panelRow{"Открыть по пути…", "Внешняя база", func(m *model) tea.Cmd {
			return m.ask("Абсолютный путь к файлу SQLite", "", false, func(path string) tea.Cmd {
				if !filepath.IsAbs(path) {
					m.report(errors.New("укажите абсолютный путь"))
					return nil
				}
				return openPage(tableList(path))(m)
			})
		}})
		err := inRoot(p.dir, func(r *os.Root) error {
			return fs.WalkDir(r.FS(), ".", func(path string, e fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				switch strings.ToLower(filepath.Ext(path)) {
				case ".db", ".sqlite", ".sqlite3":
				default:
					return nil
				}
				if !e.Type().IsRegular() {
					return nil
				}
				full := filepath.Join(p.dir, filepath.FromSlash(path))
				page.rows = append(page.rows, panelRow{title: path, open: openPage(func(ctx context.Context) (panelPage, error) {
					actual, err := filepath.EvalSymlinks(full)
					if err != nil {
						return panelPage{}, err
					}
					base, err := filepath.EvalSymlinks(p.dir)
					if err != nil {
						return panelPage{}, err
					}
					rel, err := filepath.Rel(base, actual)
					if err != nil || !filepath.IsLocal(rel) {
						return panelPage{}, errors.New("файл за пределами сервера")
					}
					return tableList(actual)(ctx)
				})})
				return nil
			})
		})
		return page, err
	}
}
func queryReadOnly(ctx context.Context, path, statement string, consume func([]string, []any) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	var magic [16]byte
	_, err = io.ReadFull(file, magic[:])
	file.Close()
	if err != nil || string(magic[:]) != "SQLite format 3\x00" {
		return errors.New("выберите базу SQLite")
	}
	name := filepath.ToSlash(path)
	if !strings.HasPrefix(name, "/") {
		name = "/" + name
	}
	u := url.URL{Scheme: "file", Path: name, RawQuery: "mode=ro&_pragma=query_only(1)&_pragma=trusted_schema(0)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	// SQLITE_LIMIT_LENGTH caps a single row or value before it enters Go memory.
	if _, err = sqlite.Limit(conn, 0, 4<<20); err != nil {
		return err
	}
	rows, err := conn.QueryContext(ctx, statement)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		values := make([]any, len(cols))
		targets := make([]any, len(cols))
		for i := range values {
			targets[i] = &values[i]
		}
		if err = rows.Scan(targets...); err != nil {
			return err
		}
		if err = consume(cols, values); err != nil {
			return err
		}
	}
	return rows.Err()
}
func readonlySQL(s string) (string, error) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ";"))
	words := strings.Fields(strings.ToLower(s))
	if len(words) == 0 || (words[0] != "select" && words[0] != "with") || strings.ContainsAny(s, ";\x00") || strings.Contains(s, "--") || strings.Contains(s, "/*") {
		return "", errors.New("разрешён один SELECT или WITH без комментариев")
	}
	tokens := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	for _, token := range tokens {
		switch token {
		case "pragma", "attach", "detach", "vacuum", "insert", "update", "delete", "drop", "alter", "create", "replace", "reindex", "load_extension", "readfile", "writefile":
			return "", fmt.Errorf("операция %s недоступна", token)
		}
	}
	return s, nil
}
func tableList(path string) pageLoader {
	return func(ctx context.Context) (panelPage, error) {
		page := panelPage{title: filepath.Base(path), description: "Таблицы SQLite"}
		page.rows = append(page.rows, panelRow{"Запрос SELECT…", "Только чтение", func(m *model) tea.Cmd {
			return m.ask("SQL-запрос", "SELECT ", false, func(input string) tea.Cmd {
				q, err := readonlySQL(input)
				if err != nil {
					m.report(err)
					return nil
				}
				return openPage(queryPage(path, "Результаты", q, 0))(m)
			})
		}})
		err := queryReadOnly(ctx, path, "SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name LIMIT 5000", func(_ []string, v []any) error {
			name := fmt.Sprint(v[0])
			q := `SELECT * FROM "` + strings.ReplaceAll(name, `"`, `""`) + `"`
			page.rows = append(page.rows, panelRow{title: name, open: openPage(queryPage(path, name, q, 0))})
			return nil
		})
		return page, err
	}
}
func queryPage(path, title, q string, offset int) pageLoader {
	return func(ctx context.Context) (panelPage, error) {
		page := panelPage{title: title, description: fmt.Sprintf("Строки с %d · Enter для просмотра полей", offset+1)}
		err := queryReadOnly(ctx, path, fmt.Sprintf("SELECT * FROM (%s) LIMIT 101 OFFSET %d", q, offset), func(cols []string, v []any) error {
			if len(page.rows) == 100 {
				page.rows = append(page.rows, panelRow{"Следующие 100 строк", "→", openPage(queryPage(path, title, q, offset+100))})
				return nil
			}
			fields := make([]panelRow, len(cols))
			values := make([]string, len(cols))
			for i, val := range v {
				var s string
				switch value := val.(type) {
				case nil:
					s = "NULL"
				case []byte:
					s = "BLOB · " + byteSize(int64(len(value)))
				default:
					s = fmt.Sprint(value)
				}
				values[i] = ansi.Truncate(oneLine(s), 160, "…")
				fields[i] = panelRow{title: cols[i] + "   " + values[i]}
			}
			page.rows = append(page.rows, panelRow{title: strings.Join(values, "  │  "), open: openPage(func(context.Context) (panelPage, error) {
				return panelPage{title: title, description: "Поля записи", rows: fields}, nil
			})})
			return nil
		})
		return page, err
	}
}
