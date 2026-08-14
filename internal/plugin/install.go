package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
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
	if err := validatePluginName(pluginCmd); err != nil {
		return err
	}

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

	// Fetch expected checksums from release
	checksums := fetchChecksums(release)

	if err := downloadAndExtract(asset.BrowserDownloadURL, binName, destPath, asset.Name, checksums); err != nil {
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
	if err := validatePluginName(name); err != nil {
		return err
	}
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

const (
	githubWebBaseURL = "https://github.com"
	githubAPIBaseURL = "https://api.github.com"
)

type releaseFetcher struct {
	client     *http.Client
	webBaseURL string
	apiBaseURL string
}

func fetchRelease(owner, repo string) (*ghRelease, error) {
	fetcher := releaseFetcher{
		client:     &http.Client{Timeout: 15 * time.Second},
		webBaseURL: githubWebBaseURL,
		apiBaseURL: githubAPIBaseURL,
	}
	return fetcher.fetch(owner, repo)
}

func (f releaseFetcher) fetch(owner, repo string) (*ghRelease, error) {
	release, publicErr := f.fetchPublicGoReleaserRelease(owner, repo)
	if publicErr == nil {
		return release, nil
	}

	release, apiErr := f.fetchFromAPI(owner, repo)
	if apiErr != nil {
		return nil, fmt.Errorf("public release discovery failed: %v; GitHub API fallback failed: %w", publicErr, apiErr)
	}
	return release, nil
}

func (f releaseFetcher) fetchPublicGoReleaserRelease(owner, repo string) (*ghRelease, error) {
	latestURL := fmt.Sprintf("%s/%s/%s/releases/latest", strings.TrimRight(f.webBaseURL, "/"), owner, repo)
	req, err := http.NewRequest(http.MethodHead, latestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "grove-plugin-installer")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("latest release page returned HTTP %d", resp.StatusCode)
	}

	tagPrefix := fmt.Sprintf("/%s/%s/releases/tag/", owner, repo)
	if !strings.HasPrefix(resp.Request.URL.Path, tagPrefix) {
		return nil, fmt.Errorf("latest release did not redirect to a release tag")
	}
	tag := strings.TrimPrefix(resp.Request.URL.Path, tagPrefix)
	version := strings.TrimPrefix(tag, "v")
	if tag == "" || version == "" {
		return nil, fmt.Errorf("latest release has an empty tag")
	}

	assetName := fmt.Sprintf("%s_%s_%s_%s.tar.gz", repo, version, runtime.GOOS, runtime.GOARCH)
	downloadBaseURL := fmt.Sprintf("%s/%s/%s/releases/download/%s", strings.TrimRight(f.webBaseURL, "/"), owner, repo, tag)
	assetURL := downloadBaseURL + "/" + assetName

	assetReq, err := http.NewRequest(http.MethodHead, assetURL, nil)
	if err != nil {
		return nil, err
	}
	assetReq.Header.Set("User-Agent", "grove-plugin-installer")
	assetResp, err := f.client.Do(assetReq)
	if err != nil {
		return nil, err
	}
	assetResp.Body.Close()
	if assetResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("conventional release asset %s returned HTTP %d", assetName, assetResp.StatusCode)
	}

	return &ghRelease{
		TagName: tag,
		Assets: []ghAsset{
			{Name: assetName, BrowserDownloadURL: assetURL},
			{Name: "checksums.txt", BrowserDownloadURL: downloadBaseURL + "/checksums.txt"},
		},
	}, nil
}

func (f releaseFetcher) fetchFromAPI(owner, repo string) (*ghRelease, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases/latest", strings.TrimRight(f.apiBaseURL, "/"), owner, repo)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "grove-plugin-installer")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository %s/%s not found or has no releases", owner, repo)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	// Limit response body to 1 MiB to prevent memory exhaustion.
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

// fetchChecksums downloads and parses checksums.txt from a release.
// Returns a map of filename → sha256 hex string, or nil if not available.
func fetchChecksums(release *ghRelease) map[string]string {
	for _, a := range release.Assets {
		if a.Name == "checksums.txt" {
			client := &http.Client{Timeout: 15 * time.Second}
			req, err := http.NewRequest("GET", a.BrowserDownloadURL, nil)
			if err != nil {
				return nil
			}
			req.Header.Set("User-Agent", "grove-plugin-installer")
			if token := os.Getenv("GITHUB_TOKEN"); token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}

			resp, err := client.Do(req)
			if err != nil {
				return nil
			}
			defer resp.Body.Close()

			if resp.StatusCode != 200 {
				return nil
			}

			// GoReleaser format: "sha256  filename\n"
			data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10)) // 64 KiB max
			if err != nil {
				return nil
			}

			result := make(map[string]string)
			for _, line := range strings.Split(string(data), "\n") {
				parts := strings.Fields(line)
				if len(parts) == 2 && len(parts[0]) == 64 {
					result[parts[1]] = parts[0]
				}
			}
			return result
		}
	}
	return nil
}

// downloadAndExtract downloads a .tar.gz, verifies its checksum, and extracts the binary.
func downloadAndExtract(url, binName, destPath, assetName string, checksums map[string]string) error {
	archiveData, err := downloadArchive(url)
	if err != nil {
		return err
	}

	if err := verifyChecksum(archiveData, assetName, checksums); err != nil {
		return err
	}

	return extractBinary(archiveData, binName, destPath)
}

// downloadArchive fetches a URL and returns the response body.
func downloadArchive(url string) ([]byte, error) {
	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "grove-plugin-installer")

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxBinarySize))
}

// verifyChecksum checks the SHA256 of data against the expected hash from checksums.
// Returns nil if no checksum is available for the asset.
func verifyChecksum(data []byte, assetName string, checksums map[string]string) error {
	expectedHash, ok := checksums[assetName]
	if !ok {
		return nil
	}
	h := sha256.Sum256(data)
	actualHash := hex.EncodeToString(h[:])
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch for %s:\n  expected: %s\n  got:      %s", assetName, expectedHash, actualHash)
	}
	return nil
}

// extractBinary extracts a named binary from a .tar.gz archive and writes it to destPath.
func extractBinary(archiveData []byte, binName, destPath string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archiveData))
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

		base := filepath.Base(hdr.Name)
		if base == binName && hdr.Typeflag == tar.TypeReg {
			return atomicWriteBinary(tr, destPath)
		}
	}

	return fmt.Errorf("binary %q not found in archive", binName)
}

// atomicWriteBinary writes from r to destPath via a temp file + rename.
func atomicWriteBinary(r io.Reader, destPath string) error {
	tmp := destPath + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r, maxBinarySize+1))
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
