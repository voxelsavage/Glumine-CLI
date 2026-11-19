package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
)

func validName(name string) error {
	if strings.TrimSpace(name) == "" || !filepath.IsLocal(name) || name == "." || filepath.Base(name) != name || strings.ContainsAny(name, `/\:`) || strings.HasPrefix(name, ".glumine-") {
		return errors.New("укажите одно имя внутри каталога")
	}
	return nil
}
func protected(rel string) bool {
	switch strings.ToLower(filepath.ToSlash(rel)) {
	case "profile.json", "server.jar", "core-installer.jar", ".glumine-installed", "eula.txt":
		return true
	}
	return false
}
func inRoot(dir string, fn func(*os.Root) error) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	return fn(root)
}
func smallFile(r *os.Root, rel string) ([]byte, error) {
	info, err := r.Stat(rel)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("для редактора нужен обычный файл")
	}
	file, err := r.Open(rel)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > editorLimit {
		return nil, errors.New("редактор открывает текстовые файлы до 256 КиБ")
	}
	data, err := io.ReadAll(io.LimitReader(file, (editorLimit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > editorLimit || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, errors.New("файл не является текстом UTF-8 до 256 КиБ")
	}
	return data, nil
}
func atomicFile(r *os.Root, rel string, data []byte, mode fs.FileMode) error {
	tmp := filepath.Join(filepath.Dir(rel), ".glumine-"+rand.Text())
	file, err := r.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode.Perm())
	if err != nil {
		return err
	}
	defer r.Remove(tmp)
	_, err = file.Write(data)
	if err = errors.Join(err, file.Close()); err != nil {
		return err
	}
	return r.Rename(tmp, rel)
}
func directoryPage(p profile, rel string) pageLoader {
	return func(ctx context.Context) (panelPage, error) {
		page := panelPage{title: "Файлы", description: filepath.ToSlash(rel)}
		page.rows = []panelRow{
			{"+ Файл", "", newEntry(p, rel, false)}, {"+ Папка", "", newEntry(p, rel, true)},
			{"Удалить запись…", "В этом каталоге", deleteEntry(p, rel)},
		}
		err := inRoot(p.dir, func(r *os.Root) error {
			entries, err := fs.ReadDir(r.FS(), filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			for _, e := range entries {
				if e.Type()&os.ModeSymlink != 0 {
					continue
				}
				path := filepath.Join(rel, e.Name())
				if e.IsDir() {
					page.rows = append(page.rows, panelRow{e.Name() + "/", "Папка", openPage(directoryPage(p, path))})
					continue
				}
				info, err := e.Info()
				if err != nil {
					return err
				}
				if !info.Mode().IsRegular() {
					continue
				}
				page.rows = append(page.rows, panelRow{e.Name(), byteSize(info.Size()), openCode(p, path)})
			}
			return nil
		})
		return page, err
	}
}
func byteSize(n int64) string {
	for _, unit := range []struct {
		n int64
		s string
	}{{1 << 30, "ГиБ"}, {1 << 20, "МиБ"}, {1 << 10, "КиБ"}} {
		if n >= unit.n {
			return fmt.Sprintf("%.1f %s", float64(n)/float64(unit.n), unit.s)
		}
	}
	return fmt.Sprintf("%d Б", n)
}
func newEntry(p profile, dir string, folder bool) action {
	return func(m *model) tea.Cmd {
		return m.ask("Имя новой записи", "", false, func(name string) tea.Cmd {
			if err := validName(name); err != nil {
				m.report(err)
				return nil
			}
			return m.change("Создание…", func(context.Context) error {
				return inRoot(p.dir, func(r *os.Root) error {
					path := filepath.Join(dir, name)
					if protected(path) {
						return errors.New("служебный файл Glumine")
					}
					if folder {
						return r.Mkdir(path, 0700)
					}
					f, err := r.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
					if err != nil {
						return err
					}
					return f.Close()
				})
			})
		})
	}
}
func deleteEntry(p profile, dir string) action {
	return func(m *model) tea.Cmd {
		if !m.stopped(p) {
			return nil
		}
		return m.ask("Имя файла или папки для удаления", "", false, func(name string) tea.Cmd {
			if err := validName(name); err != nil {
				m.report(err)
				return nil
			}
			path := filepath.Join(dir, name)
			if protected(path) {
				m.report(errors.New("служебный файл Glumine"))
				return nil
			}
			return m.confirm("Удалить "+path+" со всем содержимым?", func(m *model) tea.Cmd {
				return m.change("Удаление…", func(context.Context) error {
					return inRoot(p.dir, func(r *os.Root) error {
						if _, err := r.Lstat(path); err != nil {
							return err
						}
						return r.RemoveAll(path)
					})
				})
			})
		})
	}
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(buf []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(buf)
}
func backupDir(p profile, s serverStore) string {
	return filepath.Join(filepath.Dir(s.root), "backups", filepath.Base(p.dir))
}
func createArchive(ctx context.Context, p profile, s serverStore) error {
	dir := backupDir(p, s)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".backup-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	archive := zip.NewWriter(tmp)
	err = inRoot(p.dir, func(r *os.Root) error {
		return fs.WalkDir(r.FS(), ".", func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if path == "." || e.Type()&os.ModeSymlink != 0 {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
			if !(info.IsDir() || info.Mode().IsRegular()) {
				return nil
			}
			head, err := zip.FileInfoHeader(info)
			if err != nil {
				return err
			}
			head.Name = path
			if e.IsDir() {
				head.Name += "/"
			} else {
				head.Method = zip.Deflate
			}
			writer, err := archive.CreateHeader(head)
			if err != nil || e.IsDir() {
				return err
			}
			file, err := r.Open(filepath.FromSlash(path))
			if err != nil {
				return err
			}
			defer file.Close()
			_, err = io.Copy(writer, ctxReader{ctx, file})
			return err
		})
	})
	if err = errors.Join(err, archive.Close(), tmp.Close(), ctx.Err()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, time.Now().Format("2006-01-02_15-04-05.000000000")+".zip"))
}
func backupPage(p profile, s serverStore) pageLoader {
	return func(context.Context) (panelPage, error) {
		dir := backupDir(p, s)
		page := panelPage{title: "Резервные копии", description: dir, rows: []panelRow{{"+ Создать ZIP", "Мир и настройки", func(m *model) tea.Cmd {
			if !m.stopped(p) {
				return nil
			}
			return m.change("Архивация…", func(ctx context.Context) error { return createArchive(ctx, p, s) })
		}}}}
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			return page, nil
		}
		if err != nil {
			return page, err
		}
		for i := len(entries) - 1; i >= 0; i-- {
			e := entries[i]
			if !strings.HasSuffix(e.Name(), ".zip") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				return page, err
			}
			page.rows = append(page.rows, panelRow{title: e.Name(), detail: byteSize(info.Size())})
		}
		return page, nil
	}
}

func storagePage(p profile) pageLoader {
	return func(ctx context.Context) (panelPage, error) {
		var files, total int64
		err := inRoot(p.dir, func(r *os.Root) error {
			return fs.WalkDir(r.FS(), ".", func(_ string, e fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if err = ctx.Err(); err != nil {
					return err
				}
				if e.Type().IsRegular() {
					info, err := e.Info()
					if err != nil {
						return err
					}
					total += info.Size()
					files++
				}
				return nil
			})
		})
		return panelPage{title: "Хранилище", description: p.dir, rows: []panelRow{{title: "Файлов", detail: fmt.Sprint(files)}, {title: "Занято", detail: byteSize(total)}}}, err
	}
}

func saveEditedFile(p profile, rel string, original, updated []byte) error {
	return inRoot(p.dir, func(r *os.Root) error {
		current, err := smallFile(r, rel)
		if err != nil {
			return err
		}
		if !bytes.Equal(original, current) {
			return errors.New("файл изменился на диске, обновите страницу")
		}
		if bytes.Equal(original, updated) {
			return nil
		}
		info, err := r.Lstat(rel)
		if err != nil {
			return err
		}
		return atomicFile(r, rel, updated, info.Mode())
	})
}
