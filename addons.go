package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const modrinthAPI = "https://api.modrinth.com/v2"

func addonTarget(p profile) (string, []string) {
	switch p.coreName() {
	case "Forge", "NeoForge", "Quilt":
		return "mods", []string{strings.ToLower(p.Core)}
	case "Fabric":
		return "mods", []string{"fabric"}
	case "Paper", "Purpur":
		return "plugins", []string{"paper", "spigot", "bukkit", "purpur"}
	}
	return "", nil
}
func addonPage(p profile) pageLoader {
	return func(context.Context) (panelPage, error) {
		dir, loaders := addonTarget(p)
		page := panelPage{title: tr("Дополнения"), description: "Modrinth · " + p.edition()}
		if len(loaders) == 0 {
			page.description = tr("Для дополнений выберите ядро с поддержкой плагинов или модов")
			return page, nil
		}
		page.rows = append(page.rows, panelRow{tr("Поиск на Modrinth"), tr("По версии и ядру"), func(m *model) tea.Cmd {
			return m.ask(tr("Название дополнения"), "", false, func(text string) tea.Cmd { return openPage(addonSearch(p, text))(m) })
		}})
		if dir == "plugins" {
			page.rows = append(page.rows, panelRow{tr("Поиск на Hangar"), tr("Плагины Paper"), func(m *model) tea.Cmd {
				return m.ask(tr("Название плагина"), "", false, func(text string) tea.Cmd { return openPage(hangarSearch(p, text))(m) })
			}})
		}
		err := inRoot(p.dir, func(r *os.Root) error {
			entries, err := fs.ReadDir(r.FS(), dir)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			for _, e := range entries {
				if !e.Type().IsRegular() {
					continue
				}
				name := e.Name()
				to := name + ".disabled"
				state := tr("Включено")
				if strings.HasSuffix(name, ".jar.disabled") {
					state = tr("Отключено")
					to = strings.TrimSuffix(name, ".disabled")
				} else if !strings.HasSuffix(name, ".jar") {
					continue
				}
				page.rows = append(page.rows, panelRow{name, state, func(m *model) tea.Cmd {
					if !m.stopped(p) {
						return nil
					}
					return m.confirm(tr("Переименовать ")+name+tr(" в ")+to+"?", func(m *model) tea.Cmd {
						return m.change(tr("Обновление…"), func(context.Context) error {
							return inRoot(p.dir, func(r *os.Root) error {
								target := filepath.Join(dir, to)
								if _, err := r.Lstat(target); err == nil {
									return os.ErrExist
								} else if !errors.Is(err, os.ErrNotExist) {
									return err
								}
								return r.Rename(filepath.Join(dir, name), target)
							})
						})
					})
				}})
			}
			return nil
		})
		return page, err
	}
}
func jsonList(v any) string { b, _ := json.Marshal(v); return string(b) }
func addonSearch(p profile, text string) pageLoader {
	return func(ctx context.Context) (panelPage, error) {
		_, loaders := addonTarget(p)
		categories := make([]string, len(loaders))
		for i, v := range loaders {
			categories[i] = "categories:" + v
		}
		args := url.Values{"query": {text}, "limit": {"30"}, "facets": {jsonList([][]string{{"versions:" + p.Version}, categories, {"server_side:required", "server_side:optional"}})}}
		var result struct {
			Hits []struct {
				Project       string `json:"project_id"`
				Title, Author string
			}
		}
		err := fetchJSON(ctx, modrinthAPI+"/search?"+args.Encode(), &result)
		page := panelPage{title: tr("Каталог Modrinth"), description: text + " · " + p.edition()}
		for _, hit := range result.Hits {
			page.rows = append(page.rows, panelRow{hit.Title, hit.Author, openPage(addonVersions(p, hit.Project, hit.Title))})
		}
		return page, err
	}
}

type addonFile struct {
	Filename, URL string
	Primary       bool
	Size          int64
	Hashes        struct{ SHA512, SHA256 string }
}

func addonVersions(p profile, id, title string) pageLoader {
	return func(ctx context.Context) (panelPage, error) {
		_, loaders := addonTarget(p)
		args := url.Values{"loaders": {jsonList(loaders)}, "game_versions": {jsonList([]string{p.Version})}}
		var versions []struct {
			Name         string
			Number       string `json:"version_number"`
			Files        []addonFile
			Dependencies []struct {
				Type string `json:"dependency_type"`
			}
		}
		err := fetchJSON(ctx, modrinthAPI+"/project/"+url.PathEscape(id)+"/version?"+args.Encode(), &versions)
		page := panelPage{title: title, description: tr("Выберите совместимую версию для установки")}
		for _, v := range versions {
			var file addonFile
			for _, f := range v.Files {
				if strings.HasSuffix(strings.ToLower(f.Filename), ".jar") && (file.Filename == "" || f.Primary) {
					file = f
				}
			}
			if file.Filename == "" {
				continue
			}
			required := 0
			for _, d := range v.Dependencies {
				if d.Type == "required" {
					required++
				}
			}
			message := tr("Установить ") + file.Filename + "?"
			if required > 0 {
				message += fmt.Sprintf(tr(" Требуемые зависимости (%d) устанавливаются отдельно."), required)
			}
			page.rows = append(page.rows, panelRow{v.Name, byteSize(file.Size), func(m *model) tea.Cmd {
				if !m.stopped(p) {
					return nil
				}
				return m.confirm(message, func(m *model) tea.Cmd {
					return m.change(tr("Загрузка дополнения…"), func(ctx context.Context) error { return installAddon(ctx, p, file) })
				})
			}})
		}
		return page, err
	}
}
func installAddon(ctx context.Context, p profile, f addonFile) error {
	dir, _ := addonTarget(p)
	link, err := url.Parse(f.URL)
	var digest hash.Hash = sha512.New()
	expected := f.Hashes.SHA512
	allowedHost := "cdn.modrinth.com"
	if f.Hashes.SHA256 != "" {
		digest = sha256.New()
		expected = f.Hashes.SHA256
		allowedHost = "hangarcdn.papermc.io"
		if link != nil && link.Host == "hangar.papermc.io" {
			allowedHost = link.Host
		}
	}
	if dir == "" || validName(f.Filename) != nil || !strings.HasSuffix(strings.ToLower(f.Filename), ".jar") || link == nil || err != nil || link.Scheme != "https" || link.Host != allowedHost || len(expected) != digest.Size()*2 || f.Size <= 0 || f.Size > 512<<20 {
		return errors.New(tr("некорректные метаданные дополнения"))
	}
	return inRoot(p.dir, func(r *os.Root) error {
		if err := r.MkdirAll(dir, 0700); err != nil {
			return err
		}
		dest := filepath.Join(dir, f.Filename)
		for _, name := range []string{dest, dest + ".disabled"} {
			if _, err := r.Lstat(name); err == nil {
				return errors.New(tr("эта версия уже установлена"))
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		body, err := get(ctx, f.URL)
		if err != nil {
			return err
		}
		defer body.Close()
		tmp := filepath.Join(dir, ".glumine-"+rand.Text()+".part")
		file, err := r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		defer r.Remove(tmp)

		n, err := io.Copy(io.MultiWriter(file, digest), io.LimitReader(body, f.Size+1))
		if err = errors.Join(err, file.Close()); err != nil {
			return err
		}
		if n != f.Size || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), expected) {
			return errors.New(tr("JAR не прошёл проверку контрольной суммы"))
		}
		return r.Rename(tmp, dest)
	})
}

const hangarAPI = "https://hangar.papermc.io/api/v1/projects"

func hangarSearch(p profile, text string) pageLoader {
	return func(ctx context.Context) (panelPage, error) {
		q := url.Values{"q": {text}, "platform": {"PAPER"}, "version": {p.Version}, "sort": {"-downloads"}, "limit": {"25"}}
		var result struct {
			Result []struct {
				Name      string
				Namespace struct{ Slug, Owner string }
			}
		}
		err := fetchJSON(ctx, hangarAPI+"?"+q.Encode(), &result)
		page := panelPage{title: tr("Каталог Hangar"), description: text + " · " + p.edition()}
		for _, h := range result.Result {
			page.rows = append(page.rows, panelRow{h.Name, h.Namespace.Owner, openPage(hangarVersions(p, h.Namespace.Slug))})
		}
		return page, err
	}
}
func hangarVersions(p profile, slug string) pageLoader {
	return func(ctx context.Context) (panelPage, error) {
		q := url.Values{"platform": {"PAPER"}, "platformVersion": {p.Version}, "limit": {"25"}}
		type download struct {
			DownloadURL string
			FileInfo    struct {
				Name, Sha256Hash string
				SizeBytes        int64
			}
		}
		var result struct {
			Result []struct {
				Name      string
				Downloads map[string]download
			}
		}
		err := fetchJSON(ctx, hangarAPI+"/"+url.PathEscape(slug)+"/versions?"+q.Encode(), &result)
		page := panelPage{title: slug, description: tr("Совместимые версии Hangar")}
		for _, v := range result.Result {
			d := v.Downloads["PAPER"]
			if d.DownloadURL == "" || d.FileInfo.Sha256Hash == "" {
				continue
			}
			file := addonFile{URL: d.DownloadURL, Filename: d.FileInfo.Name, Size: d.FileInfo.SizeBytes}
			file.Hashes.SHA256 = d.FileInfo.Sha256Hash
			page.rows = append(page.rows, panelRow{v.Name, byteSize(file.Size), func(m *model) tea.Cmd {
				if !m.stopped(p) {
					return nil
				}
				return m.confirm(tr("Установить ")+file.Filename+"?", func(m *model) tea.Cmd {
					return m.change(tr("Загрузка с Hangar…"), func(ctx context.Context) error { return installAddon(ctx, p, file) })
				})
			}})
		}
		return page, err
	}
}
