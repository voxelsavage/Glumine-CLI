package main

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	manifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	eulaURL     = "https://www.minecraft.net/eula"
)

var downloadClient = &http.Client{Timeout: 5 * time.Minute}

type profile struct {
	MinMemory int    `json:"minMemoryMiB,omitempty"`
	JVMArgs   string `json:"jvmArgs,omitempty"`
	JavaMode  string `json:"javaMode,omitempty"`
	JavaPath  string `json:"javaPath,omitempty"`
	Name      string `json:"name"`
	Core      string `json:"core,omitempty"`
	Build     string `json:"build,omitempty"`
	Offline   bool   `json:"offline,omitempty"`
	Version   string `json:"version"`
	Java      int    `json:"java"`
	Port      int    `json:"port"`
	Memory    int    `json:"memoryMiB"`
	dir       string
}

func (p profile) coreName() string {
	if p.Core == "" {
		return "Vanilla"
	}
	return p.Core
}
func (p profile) edition() string {
	if p.Build != "" && p.coreName() != "Vanilla" {
		return p.coreName() + " " + p.Version + " #" + p.Build
	}
	return p.coreName() + " " + p.Version
}

type serverStore struct {
	root string
}

func (s serverStore) list() ([]profile, string, error) {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return nil, "", err
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, "", err
	}
	var profiles []profile
	var skipped []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(s.root, entry.Name())
		data, err := os.ReadFile(filepath.Join(dir, "profile.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			skipped = append(skipped, entry.Name())
			continue
		}
		p := profile{dir: dir}
		if err := json.Unmarshal(data, &p); err != nil || strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Version) == "" {
			skipped = append(skipped, entry.Name())
			continue
		}
		profiles = append(profiles, effectiveProfile(p))
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].Name) < strings.ToLower(profiles[j].Name)
	})
	warning := ""
	if len(skipped) > 0 {
		warning = "Не удалось прочитать profile.json: " + oneLine(strings.Join(skipped, ", "))
	}
	return profiles, warning, nil
}

type release struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

type releaseManifest struct {
	Latest struct {
		Release string `json:"release"`
	} `json:"latest"`
	Versions []release `json:"versions"`
}

type artifact struct {
	SHA256 string `json:"-"`
	URL    string `json:"url"`
	SHA1   string `json:"sha1"`
	Size   int64  `json:"size"`
	MD5    string `json:"-"`
}

type versionDetails struct {
	Downloads struct {
		Server artifact `json:"server"`
	} `json:"downloads"`
	JavaVersion struct {
		Major int `json:"majorVersion"`
	} `json:"javaVersion"`
}

func get(ctx context.Context, url string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "Glumine-CLI (https://disk.guloxy.com/d/glumine-latest)")
	response, err := downloadClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("%s: %s", request.URL.Host, response.Status)
	}
	return response.Body, nil
}

func fetchJSON(ctx context.Context, url string, dest any) error {
	body, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer body.Close()
	return json.NewDecoder(io.LimitReader(body, 8<<20)).Decode(dest)
}

func vanillaVersion(ctx context.Context, version string) (release, versionDetails, error) {
	var manifest releaseManifest
	var details versionDetails
	if err := fetchJSON(ctx, manifestURL, &manifest); err != nil {
		return release{}, details, fmt.Errorf("load Minecraft release list: %w", err)
	}
	if strings.EqualFold(version, "latest") {
		version = manifest.Latest.Release
	}
	for _, item := range manifest.Versions {
		if item.ID == version && item.Type == "release" {
			if err := fetchJSON(ctx, item.URL, &details); err != nil {
				return item, details, fmt.Errorf("load Minecraft version metadata: %w", err)
			}
			if details.Downloads.Server.URL == "" {
				return item, details, fmt.Errorf("Minecraft %s has no downloadable server", version)
			}
			return item, details, nil
		}
	}
	return release{}, details, fmt.Errorf("Minecraft release %q not found", version)
}

func downloadJar(ctx context.Context, dest string, jar artifact) error {
	var digest hash.Hash = sha1.New()
	expected := jar.SHA1
	limit := jar.Size + 1
	if jar.Size == 0 && jar.SHA1 != "" {
		limit = 512 << 20
	}
	if jar.SHA256 != "" {
		digest = sha256.New()
		expected = jar.SHA256
		if jar.Size == 0 {
			limit = 512 << 20
		}
	}
	if jar.MD5 != "" {
		digest = md5.New()
		expected = jar.MD5
		limit = 512 << 20
	}
	if limit <= 1 || len(expected) != digest.Size()*2 {
		return errors.New("invalid server JAR metadata")
	}
	body, err := get(ctx, jar.URL)
	if err != nil {
		return err
	}
	defer body.Close()
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}

	size, copyErr := io.Copy(io.MultiWriter(file, digest), io.LimitReader(body, limit))
	if err := errors.Join(copyErr, file.Close()); err != nil {
		return err
	}
	if (jar.Size > 0 && size != jar.Size) || size >= limit || !strings.EqualFold(hex.EncodeToString(digest.Sum(nil)), expected) {
		return errors.New("контрольная сумма или размер JAR не совпадает")
	}
	return nil
}

func (s serverStore) create(ctx context.Context, p profile) (profile, error) {
	version, details, err := vanillaVersion(ctx, p.Version)
	if err != nil {
		return p, err
	}
	p.Version = version.ID
	p.Java = details.JavaVersion.Major
	if p.Java <= 0 {
		p.Java = 8
	}

	if p.JavaMode == javaManual {
		if p.JavaPath == "" {
			return p, errors.New("не указан путь к Java")
		}
		valid, err := findJava(ctx, p.JavaPath, p.Java)
		if err != nil {
			return p, err
		}
		p.JavaPath = valid
	}

	jar := details.Downloads.Server
	switch p.coreName() {
	case "Vanilla":
		if p.Build != "" && p.Build != p.Version {
			return p, errors.New("сборка Vanilla должна совпадать с версией Minecraft")
		}
		p.Build = p.Version
	case "Forge", "NeoForge", "Quilt":
		jar, err = installerArtifact(ctx, p)
		if err != nil {
			return p, err
		}
	case "Paper":
		jar, err = paperArtifact(ctx, p)
		if err != nil {
			return p, err
		}
	case "Fabric":
	// The launcher is served by Fabric Meta.
	case "Purpur":
		base := purpurURL + "/" + url.PathEscape(p.Version) + "/" + url.PathEscape(p.Build)
		var meta struct{ Build, Version, Result, MD5 string }
		if err := fetchJSON(ctx, base, &meta); err != nil {
			return p, err
		}
		if meta.Result != "SUCCESS" || meta.Build != p.Build || meta.Version != p.Version || len(meta.MD5) != 32 {
			return p, errors.New("сборка Purpur недоступна")
		}
		jar = artifact{URL: base + "/download", MD5: meta.MD5}
	default:
		return p, fmt.Errorf("неизвестное ядро %q", p.Core)
	}
	staging, err := os.MkdirTemp(s.root, ".install-")
	if err != nil {
		return p, err
	}
	defer os.RemoveAll(staging)
	dest := filepath.Join(staging, p.packageName())
	if p.Core == "Fabric" {
		err = downloadFabric(ctx, dest, p)
	} else {
		err = downloadJar(ctx, dest, jar)
	}
	if err != nil {
		return p, fmt.Errorf("download Minecraft server: %w", err)
	}

	if err := os.WriteFile(filepath.Join(staging, "eula.txt"), []byte("# "+eulaURL+"\neula=false\n"), 0o600); err != nil {
		return p, err
	}
	properties := "server-port=" + strconv.Itoa(p.Port) + "\nonline-mode=" + strconv.FormatBool(!p.Offline) + "\nmotd=Glumine server\n"
	if err := os.WriteFile(filepath.Join(staging, "server.properties"), []byte(properties), 0o600); err != nil {
		return p, err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return p, err
	}
	if err := os.WriteFile(filepath.Join(staging, "profile.json"), data, 0o600); err != nil {
		return p, err
	}

	if err := ctx.Err(); err != nil {
		return p, err
	}
	p.dir = filepath.Join(s.root, "server-"+strings.TrimPrefix(filepath.Base(staging), ".install-"))
	return p, os.Rename(staging, p.dir)
}

// Minecraft reads server.properties on each start. Reflect edits made in the
// file manager or outside the CLI when showing connection details.
func effectiveProfile(p profile) profile {
	_ = inRoot(p.dir, func(r *os.Root) error {
		b, err := smallFile(r, "server.properties")
		if err != nil {
			return err
		}
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.TrimSpace(k) {
			case "server-port":
				if n, e := strconv.Atoi(v); e == nil && n > 0 && n <= 65535 {
					p.Port = n
				}
			case "online-mode":
				if online, e := strconv.ParseBool(v); e == nil {
					p.Offline = !online
				}
			}
		}
		return nil
	})
	return p
}
