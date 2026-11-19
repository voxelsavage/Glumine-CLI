package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gofrs/flock"
)

func main() {
	initializeLocale()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Glumine CLI:", err)
		os.Exit(1)
	}
}

func run() error {
	dataDir := flag.String("data-dir", "", tr("каталог данных Glumine CLI"))
	java := flag.String("java", "", tr("путь к исполняемому файлу Java"))
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New(tr("лишние аргументы (см. -help)"))
	}

	if *dataDir == "" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return fmt.Errorf(tr("поиск каталога конфигурации: %w"), err)
		}
		*dataDir = filepath.Join(configDir, "Glumine-CLI")
	}
	root, err := filepath.Abs(*dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}

	lock := flock.New(filepath.Join(root, "glumine.lock"))
	acquired, err := lock.TryLock()
	if err != nil {
		return fmt.Errorf(tr("блокировка каталога данных: %w"), err)
	}
	if !acquired {
		return errors.New(tr("другой экземпляр Glumine CLI использует этот каталог данных"))
	}
	defer lock.Close()

	store := serverStore{root: filepath.Join(root, "servers")}
	profiles, warning, err := store.list()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	model := newModel(ctx, store, profiles, *java)
	model.libraryWarning = warning
	defer func() {
		cancel()
		if model.panel.done != nil {
			<-model.panel.done
		}
		if model.javaDone != nil {
			<-model.javaDone
		}
		if model.installDone != nil {
			<-model.installDone
		}
		if model.process != nil {
			model.process.stop()
			<-model.process.done
		}
	}()

	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	return err
}
