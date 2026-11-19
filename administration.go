package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func networkPage(p profile) pageLoader {
	return func(context.Context) (panelPage, error) {
		p := effectiveProfile(p)
		addr := func(ip string) string { return net.JoinHostPort(ip, strconv.Itoa(p.Port)) }
		page := panelPage{title: "Сеть", description: "Адреса для подключения к серверу", rows: []panelRow{{title: addr("127.0.0.1"), detail: "На этом ПК"}}}
		ifaces, err := net.Interfaces()
		if err != nil {
			return page, err
		}
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, a := range addrs {
				ip, _, err := net.ParseCIDR(a.String())
				if err == nil && ip.IsGlobalUnicast() {
					page.rows = append(page.rows, panelRow{title: addr(ip.String()), detail: iface.Name})
				}
			}
		}
		page.rows = append(page.rows, panelRow{"Узнать внешний IP", "api.ipify.org", openPage(func(ctx context.Context) (panelPage, error) {
			body, err := get(ctx, "https://api.ipify.org")
			if err != nil {
				return panelPage{}, err
			}
			defer body.Close()
			data, err := io.ReadAll(io.LimitReader(body, 128))
			if err != nil {
				return panelPage{}, err
			}
			ip := net.ParseIP(strings.TrimSpace(string(data)))
			if ip == nil {
				return panelPage{}, errors.New("не удалось определить внешний IP")
			}
			return panelPage{title: "Внешний адрес", description: "Доступность извне зависит от маршрутизатора", rows: []panelRow{{title: addr(ip.String())}}}, nil
		})})
		return page, nil
	}
}

var username = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

func commandAction(p profile, command string, needsName bool) action {
	return func(m *model) tea.Cmd {
		if m.owner != p.dir || m.process == nil {
			m.report(errors.New("запустите сервер для отправки команды"))
			return nil
		}
		send := func(text string) tea.Cmd {
			if err := m.process.send(text); err != nil {
				m.report(err)
				return nil
			}
			m.panel.prompt = nil
			m.status = "Команда отправлена · ответ в консоли"
			return nil
		}
		if !needsName {
			send(command)
			if m.failure == "" {
				m.exitPanel()
				if m.screen == listScreen {
					return m.listKey("enter")
				}
			}
			return nil
		}
		return m.ask(command+" · имя игрока", "", false, func(name string) tea.Cmd {
			if !username.MatchString(name) {
				m.report(errors.New("введите имя Minecraft из 1–16 букв, цифр или символов _"))
				return nil
			}
			if m.process == nil {
				m.report(errors.New("сервер остановлен"))
				return nil
			}
			return send(command + " " + name)
		})
	}
}
func playerPage(p profile) pageLoader {
	return func(context.Context) (panelPage, error) {
		page := panelPage{title: "Игроки", description: "Модерация через консоль Minecraft"}
		for _, entry := range [][2]string{{"Список онлайн → консоль", "list"}, {"Отключить игрока", "kick"}, {"Заблокировать", "ban"}, {"Снять блокировку", "pardon"}, {"Добавить в белый список", "whitelist add"}, {"Убрать из белого списка", "whitelist remove"}} {
			page.rows = append(page.rows, panelRow{entry[0], entry[1], commandAction(p, entry[1], entry[1] != "list")})
		}
		for _, file := range [][2]string{{"Известные игроки", "usercache.json"}, {"Заблокированные", "banned-players.json"}, {"Белый список", "whitelist.json"}, {"Операторы", "ops.json"}} {
			page.rows = append(page.rows, panelRow{file[0], "", openPage(func(context.Context) (panelPage, error) {
				result := panelPage{title: file[0], description: file[1]}
				err := inRoot(p.dir, func(r *os.Root) error {
					b, err := smallFile(r, file[1])
					if errors.Is(err, os.ErrNotExist) {
						return nil
					}
					if err != nil {
						return err
					}
					var entries []struct{ Name, Reason string }
					if err := json.Unmarshal(b, &entries); err != nil {
						return err
					}
					for _, e := range entries {
						result.rows = append(result.rows, panelRow{title: e.Name, detail: e.Reason})
					}
					return nil
				})
				return result, err
			})})
		}
		return page, nil
	}
}

func (m *model) refreshLibrary(prefer string) error {
	if prefer == "" && m.cursor < len(m.profiles) {
		prefer = m.profiles[m.cursor].dir
	}
	profiles, warning, err := m.store.list()
	if err != nil {
		return err
	}
	m.profiles, m.libraryWarning = profiles, warning
	m.cursor = 0
	for i, p := range profiles {
		if p.dir == prefer {
			m.cursor = i
			return nil
		}
	}
	if m.screen == panelScreen && len(m.panel.pages) > 0 && m.panel.pages[0].overview {
		m.exitPanel()
		m.screen = listScreen
		return errors.New("профиль больше недоступен · библиотека обновлена")
	}

	return nil
}
func settingsPage(p profile, s serverStore) pageLoader {
	return func(context.Context) (panelPage, error) {
		data, err := os.ReadFile(filepath.Join(p.dir, "profile.json"))
		if err != nil {
			return panelPage{}, err
		}
		current := profile{dir: p.dir}
		if err = json.Unmarshal(data, &current); err != nil {
			return panelPage{}, err
		}
		page := panelPage{title: "Настройки сервера", description: current.edition()}
		save := func(m *model, updated profile) tea.Cmd {
			b, err := json.MarshalIndent(updated, "", "  ")
			if err == nil {
				err = inRoot(p.dir, func(r *os.Root) error { return atomicFile(r, "profile.json", b, 0600) })
			}
			if err == nil {
				err = m.refreshLibrary(p.dir)
			}
			if err != nil {
				m.report(err)
				return nil
			}
			m.panel.prompt = nil
			return m.reload()
		}
		for _, minimum := range []bool{false, true} {
			name, value := "Максимум памяти", current.Memory
			if minimum {
				name, value = "Начальная память", current.minRAM()
			}
			page.rows = append(page.rows, panelRow{name, fmt.Sprintf("%d МиБ", value), func(m *model) tea.Cmd {
				if !m.stopped(p) {
					return nil
				}
				return m.ask(name+" Java, МиБ", strconv.Itoa(value), false, func(input string) tea.Cmd {
					n, err := strconv.Atoi(input)
					if err != nil || n < 256 || n > 65536 || (minimum && n > current.Memory) || (!minimum && n < current.minRAM()) {
						m.report(errors.New("нужно 256–65536 МиБ, минимум не выше максимума"))
						return nil
					}
					updated := current
					if minimum {
						updated.MinMemory = n
					} else {
						updated.Memory = n
					}
					return save(m, updated)
				})
			}})
		}
		page.rows = append(page.rows, panelRow{"Аргументы JVM", "Один на строку", func(m *model) tea.Cmd {
			if !m.stopped(p) {
				return nil
			}
			return m.ask("Параметры JVM · один на строку", current.JVMArgs, true, func(input string) tea.Cmd {
				for _, arg := range strings.Split(input, "\n") {
					arg = strings.TrimSpace(arg)
					if arg != "" && (!strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "-Xm") || arg == "-jar") {
						m.report(errors.New("укажите флаги JVM, память задаётся отдельно"))
						return nil
					}
				}
				updated := current
				updated.JVMArgs = input
				return save(m, updated)
			})
		}}, panelRow{"Свойства Minecraft", "server.properties", openCode(p, "server.properties")},
			panelRow{"Место на диске", "Подсчитать", openPage(storagePage(p))},
			panelRow{"Переустановить ядро", current.coreName(), func(m *model) tea.Cmd {
				if !m.stopped(p) {
					return nil
				}
				return m.confirm("Заново скачать выбранную сборку и заменить пакет ядра?", func(m *model) tea.Cmd {
					return m.change("Переустановка ядра…", func(ctx context.Context) error {
						tmp, err := os.MkdirTemp(s.root, ".repair-")
						if err != nil {
							return err
						}
						defer os.RemoveAll(tmp)
						fresh, err := (serverStore{root: tmp}).create(ctx, current)
						if err != nil {
							return err
						}
						if installerCore(p.Core) {
							if err := os.Remove(filepath.Join(p.dir, ".glumine-installed")); err != nil && !errors.Is(err, os.ErrNotExist) {
								return err
							}
						}
						return os.Rename(filepath.Join(fresh.dir, p.packageName()), filepath.Join(p.dir, p.packageName()))
					})
				})
			}}, panelRow{"В корзину", "Профиль и мир", func(m *model) tea.Cmd {
				if !m.stopped(p) {
					return nil
				}
				return m.confirm("Переместить "+p.Name+" в корзину?", func(m *model) tea.Cmd {
					trash := filepath.Join(filepath.Dir(s.root), "trash")
					if err := os.MkdirAll(trash, 0700); err != nil {
						m.report(err)
						return nil
					}
					if err := moveProfile(p.dir, filepath.Join(trash, filepath.Base(p.dir))); err != nil {
						m.report(err)
						return nil
					}
					m.exitPanel()
					m.screen = listScreen
					if err := m.refreshLibrary(""); err != nil {
						m.report(err)
					}
					return nil
				})
			}})
		return page, nil
	}
}
func moveProfile(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return errors.New("папка назначения уже существует")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}
func trashPage(s serverStore) pageLoader {
	return func(context.Context) (panelPage, error) {
		trash := serverStore{root: filepath.Join(filepath.Dir(s.root), "trash")}
		profiles, warning, err := trash.list()
		page := panelPage{title: "Корзина", description: "Выберите сервер для восстановления или удаления"}
		if warning != "" {
			page.description = warning
		}
		for _, p := range profiles {
			page.rows = append(page.rows, panelRow{p.Name, p.edition(), openPage(func(context.Context) (panelPage, error) {
				return panelPage{title: p.Name, description: "Корзина", rows: []panelRow{
					{"Восстановить сервер", "", func(m *model) tea.Cmd {
						dst := filepath.Join(s.root, filepath.Base(p.dir))
						if err := moveProfile(p.dir, dst); err != nil {
							m.report(err)
							return nil
						}
						m.exitPanel()
						if err := m.refreshLibrary(""); err != nil {
							m.report(err)
						}
						return nil
					}},
					{"Удалить навсегда", "", func(m *model) tea.Cmd {
						return m.confirm("Безвозвратно удалить "+p.Name+"?", func(m *model) tea.Cmd {
							return m.schedule("Очистка…", func(context.Context) (panelPage, error) { return panelPage{}, os.RemoveAll(p.dir) }, func(panelPage) tea.Cmd { m.exitPanel(); return nil })
						})
					}},
				}}, nil
			})})
		}
		return page, err
	}
}
