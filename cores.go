package main

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
)

const paperAPI = "https://fill.papermc.io/v3/projects/paper"
const fabricAPI = "https://meta.fabricmc.net/v2/versions"

type fabricVersion struct {
	Version string
	Stable  bool
}
type paperDownload struct {
	URL       string
	Size      int64
	Checksums struct{ SHA256 string }
}
type paperBuild struct {
	ID        int
	Channel   string
	Downloads map[string]paperDownload
}

func coreOptions(ctx context.Context, version string) ([]string, error) {
	type availability struct {
		name      string
		supported bool
		err       error
	}
	ch := make(chan availability, 6)
	go func() { builds, err := quiltBuilds(ctx, version); ch <- availability{"Quilt", len(builds) > 0, err} }()
	go func() {
		var res struct{ Versions []string }
		err := fetchJSON(ctx, purpurURL, &res)
		ch <- availability{"Purpur", slices.Contains(res.Versions, version), err}
	}()
	go func() {
		var res struct{ Versions map[string][]string }
		err := fetchJSON(ctx, paperAPI, &res)
		ok := false
		for _, v := range res.Versions {
			if slices.Contains(v, version) {
				ok = true
				break
			}
		}
		ch <- availability{"Paper", ok, err}
	}()
	go func() {
		var res []fabricVersion
		err := fetchJSON(ctx, fabricAPI+"/game", &res)
		ok := slices.ContainsFunc(res, func(item fabricVersion) bool { return item.Version == version })
		ch <- availability{"Fabric", ok, err}
	}()
	options := []string{"Vanilla"}
	var missing []string
	for _, core := range []string{"Forge", "NeoForge"} {
		go func() { builds, err := forgeBuilds(ctx, version, core); ch <- availability{core, len(builds) > 0, err} }()
	}
	for i := 0; i < 6; i++ {
		r := <-ch
		if r.err != nil {
			missing = append(missing, r.name)
		} else if r.supported {
			options = append(options, r.name)
		}
	}
	slices.Sort(options[1:])
	if len(missing) > 0 {
		return options, fmt.Errorf("недоступны каталоги: %s", strings.Join(missing, ", "))
	}
	return options, nil
}
func paperBuilds(ctx context.Context, v string) ([]paperBuild, error) {
	var out []paperBuild
	err := fetchJSON(ctx, paperAPI+"/versions/"+url.PathEscape(v)+"/builds", &out)
	return out, err
}
func extraBuilds(ctx context.Context, v, core string) ([]string, error) {
	var result []string
	if core == "Paper" {
		builds, err := paperBuilds(ctx, v)
		if err != nil {
			return nil, err
		}
		for _, b := range builds {
			if b.Channel == "STABLE" && b.Downloads["server:default"].Checksums.SHA256 != "" {
				result = append(result, strconv.Itoa(b.ID))
			}
		}
	} else {
		var builds []struct{ Loader fabricVersion }
		if err := fetchJSON(ctx, fabricAPI+"/loader/"+url.PathEscape(v), &builds); err != nil {
			return nil, err
		}
		for _, b := range builds {
			if b.Loader.Stable {
				result = append(result, b.Loader.Version)
			}
		}
	}
	return result, nil
}
func paperArtifact(ctx context.Context, p profile) (artifact, error) {
	builds, err := paperBuilds(ctx, p.Version)
	if err != nil {
		return artifact{}, err
	}
	for _, b := range builds {
		if b.Channel == "STABLE" && strconv.Itoa(b.ID) == p.Build {
			d := b.Downloads["server:default"]
			if len(d.Checksums.SHA256) == 64 && d.URL != "" {
				return artifact{URL: d.URL, SHA256: d.Checksums.SHA256, Size: d.Size}, nil
			}
		}
	}
	return artifact{}, errors.New("выбранная сборка Paper недоступна")
}
func downloadFabric(ctx context.Context, dest string, p profile) error {
	var installers []fabricVersion
	if err := fetchJSON(ctx, fabricAPI+"/installer", &installers); err != nil {
		return err
	}
	for _, installer := range installers {
		if !installer.Stable {
			continue
		}
		link := fabricAPI + "/loader/" + url.PathEscape(p.Version) + "/" + url.PathEscape(p.Build) + "/" + url.PathEscape(installer.Version) + "/server/jar"
		body, err := get(ctx, link)
		if err != nil {
			return err
		}
		defer body.Close()
		file, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		const limit = 32 << 20
		n, err := io.Copy(file, io.LimitReader(body, limit+1))
		if err = errors.Join(err, file.Close()); err != nil {
			return err
		}
		if n > limit {
			return errors.New("слишком большой Fabric launcher")
		}
		jar, err := zip.OpenReader(dest)
		if err != nil {
			return err
		}
		defer jar.Close()
		_, err = jar.Open("META-INF/MANIFEST.MF")
		return err
	}
	return errors.New("стабильный установщик Fabric недоступен")
}
