package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	javaAuto   = "auto"
	javaManual = "path"
)

func javaFilename() string {
	if runtime.GOOS == "windows" {
		return "java.exe"
	}
	return "java"
}

func javaPackage(ctx context.Context, major int) (string, string, int64, error) {
	arch := map[string]string{"amd64": "x64", "arm64": "aarch64", "386": "x32", "arm": "arm"}[runtime.GOARCH]
	system := runtime.GOOS
	if system == "darwin" {
		system = "mac"
	}
	if arch == "" {
		return "", "", 0, fmt.Errorf(tr("нет автоматической установки для %s"), runtime.GOARCH)
	}
	for _, kind := range []string{"jre", "jdk"} {
		params := url.Values{"architecture": {arch}, "os": {system}, "image_type": {kind}, "vendor": {"eclipse"}}
		endpoint := fmt.Sprintf("https://api.adoptium.net/v3/assets/latest/%d/hotspot?%s", major, params.Encode())
		var assets []struct {
			Binary struct {
				Package struct {
					Link, Checksum string
					Size           int64
				}
			}
		}
		if err := fetchJSON(ctx, endpoint, &assets); err != nil {
			return "", "", 0, err
		}
		if len(assets) == 0 {
			continue
		}
		p := assets[0].Binary.Package
		if !strings.HasPrefix(p.Link, "https://") || len(p.Checksum) != 64 || p.Size <= 0 {
			return "", "", 0, errors.New(tr("некорректные данные пакета Java"))
		}
		return p.Link, p.Checksum, p.Size, nil
	}
	return "", "", 0, fmt.Errorf(tr("Temurin Java %d недоступна для %s/%s"), major, system, arch)
}

func installedJava(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, bin := range []string{"bin", "Contents/Home/bin"} {
			path := filepath.Join(dir, entry.Name(), bin, javaFilename())
			if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
				return path, nil
			}
		}
	}
	return "", os.ErrNotExist
}

func downloadRuntime(ctx context.Context, dir string, major int) error {
	link, sum, size, err := javaPackage(ctx, major)
	if err != nil {
		return err
	}
	body, err := get(ctx, link)
	if err != nil {
		return err
	}
	defer body.Close()
	archive := filepath.Join(dir, "runtime.archive")
	file, err := os.Create(archive)
	if err != nil {
		return err
	}
	digest := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(body, size+1))
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return err
	}
	if n != size || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), sum) {
		return errors.New(tr("пакет Java не прошёл проверку SHA-256"))
	}
	// Both BSD tar (Windows/macOS) and GNU tar (Linux) auto-detect their
	// platform's Temurin archive format when reading from a file.
	if out, err := exec.CommandContext(ctx, "tar", "-xf", archive, "-C", dir).CombinedOutput(); err != nil {
		return fmt.Errorf(tr("распаковка Java: %w %s"), err, strings.TrimSpace(string(out)))
	}
	return os.Remove(archive)
}

func resolveJava(ctx context.Context, p profile, override, cache string) (string, error) {
	switch p.JavaMode {
	case "":
		return findJava(ctx, override, p.Java)
	case javaManual:
		if p.JavaPath == "" {
			return "", errors.New(tr("не указан путь к Java"))
		}
		return findJava(ctx, p.JavaPath, p.Java)
	case javaAuto:
	default:
		return "", fmt.Errorf(tr("неизвестный режим Java %q"), p.JavaMode)
	}
	if err := os.MkdirAll(cache, 0700); err != nil {
		return "", err
	}
	dest := filepath.Join(cache, strconv.Itoa(p.Java)+"-"+runtime.GOOS+"-"+runtime.GOARCH)
	if path, err := installedJava(dest); err == nil {
		if valid, err := findJava(ctx, path, p.Java); err == nil {
			return valid, nil
		}
	}
	staging, err := os.MkdirTemp(cache, ".install-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if err := downloadRuntime(ctx, staging, p.Java); err != nil {
		return "", err
	}
	path, err := installedJava(staging)
	if err != nil {
		return "", fmt.Errorf(tr("в архиве отсутствует bin/%s"), javaFilename())
	}
	if _, err = findJava(ctx, path, p.Java); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(staging, path)
	if err != nil {
		return "", err
	}
	// The per-data-directory lock serializes cache repairs. Only replace
	// an unusable installation after verifying the replacement.
	if _, err := os.Lstat(dest); err == nil {
		backup := staging + ".old"
		if err = os.Rename(dest, backup); err != nil {
			return "", err
		}
		if err = os.Rename(staging, dest); err != nil {
			return "", errors.Join(err, os.Rename(backup, dest))
		}
		_ = os.RemoveAll(backup)
	} else if err = os.Rename(staging, dest); err != nil {
		return "", err
	}
	return filepath.Join(dest, rel), nil
}
