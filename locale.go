package main

import (
	"os"
	"strings"
)

// Set once at startup, before starting the UI or background workers.
var russianUI bool

func tr(original string) string {
	if !russianUI {
		if text, ok := englishText[original]; ok {
			return text
		}
	}
	return original
}

func russianLocale(getenv func(string) string, native func() string) bool {
	var locale string
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale = strings.TrimSpace(getenv(key)); locale != "" {
			break
		}
	}
	if locale == "" {
		locale = native()
	}
	lang, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(locale)), ".")
	for _, sep := range []string{"@", "_", "-"} {
		lang, _, _ = strings.Cut(lang, sep)
	}
	return lang == "ru"
}

func initializeLocale() { russianUI = russianLocale(os.Getenv, nativeLocale) }
