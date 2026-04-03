package plugin

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// ghRelease is a subset of the GitHub release API response.
type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

// ghAsset is a single release asset.
type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Install downloads the latest release of a plugin from GitHub.
// repo should be like "github.com/nicksenap/gw-dash" or "nicksenap/gw-dash".
func Install(repo string) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}

	// Plugin name is the repo name (e.g. "gw-dash" → command name "dash")
	pluginCmd := strings.TrimPrefix(name, "gw-")

	// Fetch latest release
	release, err := fetchRelease(owner, name)
	if err != nil {
		return fmt.Errorf("fetching release for %s/%s: %w", owner, name, err)
	}

	// Find matching asset for this OS/arch
	asset, err := findAsset(release, name)
	if err != nil {
		return err
	}

	// Ensure plugins dir exists
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating plugins dir: %w", err)
	}

	// Download and extract
	binName := "gw-" + pluginCmd
	destPath := filepath.Join(dir, binName)

	if !strings.HasPrefix(asset.BrowserDownloadURL, "https://") {
		return fmt.Errorf("asset URL is not HTTPS: %s", asset.BrowserDownloadURL)
	}

	if err := downloadAndExtract(asset.BrowserDownloadURL, binName, destPath); err != nil {
		return fmt.Errorf("downloading %s: %w", asset.Name, err)
	}

	// Save metadata so we know where to upgrade from
	saveMeta(pluginCmd, owner+"/"+name, release.TagName)

	fmt.Fprintf(os.Stderr, "\033[1;32mok:\033[0m Installed %s %s → %s\n",
		pluginCmd, release.TagName, destPath)
	return nil
}

// pluginMeta is the metadata stored alongside an installed plugin.
type pluginMeta struct {
	Repo    string `json:"repo"`
	Version string `json:"version"`
}

func metaPath(pluginCmd string) string {
	return filepath.Join(Dir(), ".gw-"+pluginCmd+".json")
}

func saveMeta(pluginCmd, repo, version string) {
	m := pluginMeta{Repo: repo, Version: version}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	os.WriteFile(metaPath(pluginCmd), data, 0o644)
}

func loadMeta(pluginCmd string) (*pluginMeta, error) {
	data, err := os.ReadFile(metaPath(pluginCmd))
	if err != nil {
		return nil, err
	}
	var m pluginMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Upgrade re-fetches the latest release for an installed plugin.
func Upgrade(name string) error {
	meta, err := loadMeta(name)
	if err != nil {
		return fmt.Errorf("plugin %q has no install metadata — reinstall with: gw plugin install <repo>", name)
	}
	return Install(meta.Repo)
}

// UpgradeAll upgrades all installed plugins that have metadata.
func UpgradeAll() ([]string, error) {
	plugins, err := List()
	if err != nil {
		return nil, err
	}

	var upgraded []string
	for _, p := range plugins {
		if _, err := loadMeta(p.Name); err != nil {
			continue // manually installed, skip
		}
		if err := Upgrade(p.Name); err != nil {
			fmt.Fprintf(os.Stderr, "\033[1;33mwarn:\033[0m %s: %s\n", p.Name, err)
			continue
		}
		upgraded = append(upgraded, p.Name)
	}
	return upgraded, nil
}

// parseRepo extracts owner/name from various formats.
func parseRepo(repo string) (owner, name string, err error) {
	// Strip scheme and host prefix
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	repo = strings.TrimPrefix(repo, "github.com/")
	repo = strings.TrimSuffix(repo, "/")

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format %q (expected owner/repo or github.com/owner/repo)", repo)
	}
	return parts[0], parts[1], nil
}

func fetchRelease(owner, repo string) (*ghRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "grove-plugin-installer")

	// Use GITHUB_TOKEN if available for higher rate limits
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("repository %s/%s not found or has no releases", owner, repo)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	// Limit response body to 1 MiB to prevent memory exhaustion
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return nil, fmt.Errorf("parsing release response: %w", err)
	}
	return &release, nil
}

// findAsset finds the right asset for the current OS/arch.
// Expects goreleaser naming: {name}_{version}_{os}_{arch}.tar.gz
func findAsset(release *ghRelease, _ string) (*ghAsset, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// Try exact match first, then common variations
	patterns := []string{
		fmt.Sprintf("%s_%s_%s.tar.gz", goos, goarch, ""),
		fmt.Sprintf("_%s_%s.tar.gz", goos, goarch),
	}

	for i := range release.Assets {
		a := &release.Assets[i]
		lower := strings.ToLower(a.Name)
		for _, p := range patterns {
			if strings.Contains(lower, strings.ToLower(p)) {
				return a, nil
			}
		}
	}

	// More flexible: just check os and arch are in the name
	for i := range release.Assets {
		a := &release.Assets[i]
		lower := strings.ToLower(a.Name)
		if strings.Contains(lower, goos) && strings.Contains(lower, goarch) && strings.HasSuffix(lower, ".tar.gz") {
			return a, nil
		}
	}

	available := make([]string, len(release.Assets))
	for i, a := range release.Assets {
		available[i] = a.Name
	}
	return nil, fmt.Errorf("no asset found for %s/%s in release %s\navailable: %s",
		goos, goarch, release.TagName, strings.Join(available, ", "))
}

const maxBinarySize = 256 << 20 // 256 MiB

// downloadAndExtract downloads a .tar.gz and extracts the binary.
func downloadAndExtract(url, binName, destPath string) error {
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "grove-plugin-installer")

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("decompressing: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		// Look for the binary — it may be at the root or in a subdirectory
		base := filepath.Base(hdr.Name)
		if base == binName && hdr.Typeflag == tar.TypeReg {
			// Write to temp file then rename (atomic install)
			tmp := destPath + ".tmp"
			f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			n, err := io.Copy(f, io.LimitReader(tr, maxBinarySize+1))
			if err != nil {
				f.Close()
				os.Remove(tmp)
				return err
			}
			if n > maxBinarySize {
				f.Close()
				os.Remove(tmp)
				return fmt.Errorf("binary exceeds maximum size (%d MiB)", maxBinarySize>>20)
			}
			f.Close()
			return os.Rename(tmp, destPath)
		}
	}

	return fmt.Errorf("binary %q not found in archive", binName)
}
