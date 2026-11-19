package main

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const forgeMaven = "https://maven.minecraftforge.net/net/minecraftforge/forge"
const neoMaven = "https://maven.neoforged.net/releases/net/neoforged/neoforge"

func installerCore(core string) bool { return core == "Forge" || core == "NeoForge" || core == "Quilt" }
func (p profile) packageName() string {
	if installerCore(p.Core) {
		return "core-installer.jar"
	}
	return "server.jar"
}
func forgeRepository(core string) (string, string) {
	if core == "NeoForge" {
		return neoMaven, "neoforge"
	}
	return forgeMaven, "forge"
}
func forgeBuilds(ctx context.Context, game, core string) ([]string, error) {
	base, _ := forgeRepository(core)
	prefix := game + "-"
	if core == "NeoForge" {
		parts := strings.Split(game, ".")
		if len(parts) < 2 {
			return nil, nil
		}
		if len(parts) == 2 {
			parts = append(parts, "0")
		}
		if parts[0] == "1" {
			parts = parts[1:]
		}
		prefix = strings.Join(parts, ".") + "."
	}
	body, err := get(ctx, base+"/maven-metadata.xml")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var meta struct {
		Versions []string `xml:"versioning>versions>version"`
	}
	if err = xml.NewDecoder(io.LimitReader(body, 8<<20)).Decode(&meta); err != nil {
		return nil, err
	}
	var builds []string
	for _, v := range meta.Versions {
		if strings.HasPrefix(v, prefix) && !strings.Contains(v, "+") && !strings.Contains(v, "-alpha") && validName(v) == nil {
			builds = append(builds, v)
		}
	}
	slices.Reverse(builds)
	return builds, nil
}
func installerArtifact(ctx context.Context, p profile) (artifact, error) {
	if err := validName(p.Build); err != nil {
		return artifact{}, err
	}
	if p.Core == "Quilt" {
		return quiltInstaller(ctx)
	}
	base, name := forgeRepository(p.Core)
	link := fmt.Sprintf("%s/%s/%s-%s-installer.jar", base, p.Build, name, p.Build)
	return mavenArtifact(ctx, link)
}
func mavenArtifact(ctx context.Context, link string) (artifact, error) {
	body, err := get(ctx, link+".sha1")
	if err != nil {
		return artifact{}, err
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, 256))
	if err != nil {
		return artifact{}, err
	}
	words := strings.Fields(string(b))
	if len(words) == 0 || len(words[0]) != 40 {
		return artifact{}, errors.New("контрольная сумма установщика недоступна")
	}
	return artifact{URL: link, SHA1: words[0]}, nil
}
func serverArgs(p profile) ([]string, error) {
	if !installerCore(p.Core) {
		return []string{"-jar", "server.jar", "nogui"}, nil
	}
	if err := validName(p.Build); err != nil {
		return nil, err
	}
	if p.Core == "Quilt" {
		const launcher = "quilt-server-launch.jar"
		info, err := os.Stat(filepath.Join(p.dir, launcher))
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("не найден Quilt launcher")
		}
		return []string{"-jar", launcher, "nogui"}, nil
	}
	group := "net/minecraftforge/forge"
	if p.Core == "NeoForge" {
		group = "net/neoforged/neoforge"
	}
	argsFile := "unix_args.txt"
	if runtime.GOOS == "windows" {
		argsFile = "win_args.txt"
	}
	rel := filepath.Join("libraries", group, p.Build, argsFile)
	if info, err := os.Stat(filepath.Join(p.dir, rel)); err == nil && info.Mode().IsRegular() {
		return []string{"@" + rel, "nogui"}, nil
	}
	for _, suffix := range []string{".jar", "-universal.jar"} {
		name := "forge-" + p.Build + suffix
		if info, err := os.Stat(filepath.Join(p.dir, name)); err == nil && info.Mode().IsRegular() {
			return []string{"-jar", name, "nogui"}, nil
		}
	}
	return nil, errors.New("установщик не создал файлы запуска ядра")
}
func prepareCore(ctx context.Context, p profile, java string) error {
	if !installerCore(p.Core) {
		return nil
	}
	marker := filepath.Join(p.dir, ".glumine-installed")
	if b, err := os.ReadFile(marker); err == nil && string(b) == p.Build {
		if _, err = serverArgs(p); err == nil {
			return nil
		}
	}
	args := []string{"-jar", p.packageName()}
	if p.Core == "Quilt" {
		args = append(args, "install", "server", p.Version, p.Build, "--install-dir=.", "--download-server")
		if p.Java < 17 {
			cache := filepath.Join(filepath.Dir(filepath.Dir(p.dir)), "java")
			setup := profile{Java: 17, JavaMode: javaAuto}
			var err error
			java, err = resolveJava(ctx, setup, "", cache)
			if err != nil {
				return err
			}
		}
	} else {
		args = append(args, "--installServer")
	}
	cmd := exec.CommandContext(ctx, java, args...)
	cmd.Dir = p.dir
	log := &consoleLog{}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Run(); err != nil {
		output, _ := log.snapshot(0)
		lines := strings.Split(strings.TrimSpace(cleanTerminalText(output)), "\n")
		return fmt.Errorf("установка %s: %w · %s", p.Core, err, lines[len(lines)-1])
	}
	if _, err := serverArgs(p); err != nil {
		return err
	}
	return os.WriteFile(marker, []byte(p.Build), 0600)
}

func quiltInstaller(ctx context.Context) (artifact, error) {
	const base = "https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-installer"
	body, err := get(ctx, base+"/maven-metadata.xml")
	if err != nil {
		return artifact{}, err
	}
	defer body.Close()
	var meta struct {
		Version string `xml:"versioning>release"`
	}
	if err := xml.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&meta); err != nil {
		return artifact{}, err
	}
	if err := validName(meta.Version); err != nil {
		return artifact{}, err
	}
	return mavenArtifact(ctx, base+"/"+meta.Version+"/quilt-installer-"+meta.Version+".jar")
}
func quiltBuilds(ctx context.Context, version string) ([]string, error) {
	var builds []struct{ Loader struct{ Version string } }
	err := fetchJSON(ctx, "https://meta.quiltmc.org/v3/versions/loader/"+url.PathEscape(version), &builds)
	var options []string
	for _, b := range builds {
		if b.Loader.Version != "" && !strings.Contains(b.Loader.Version, "-") {
			options = append(options, b.Loader.Version)
		}
	}
	return options, err
}
