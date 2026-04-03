// Package update provides non-blocking version checking against GitHub releases.
// The check uses a 24h-cached result and refreshes in the background.
package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// CacheData is the on-disk cache format.
type CacheData struct {
	LastCheck int64  `json:"last_check"`
	Latest    string `json:"latest"`
}

// Checker performs version checks with injectable dependencies.
type Checker struct {
	CachePath    string
	NowFn        func() time.Time
	FetchLatestFn func() (string, error) // returns latest version string
}

// NewChecker creates a Checker with real dependencies.
func NewChecker(groveDir string) *Checker {
	return &Checker{
		CachePath:    filepath.Join(groveDir, "update-check.json"),
		NowFn:        time.Now,
		FetchLatestFn: fetchLatestFromGitHub,
	}
}

// GetNewerVersion returns the latest version if it's newer than current,
// or "" if current is up-to-date or unknown. Never blocks on network.
func (c *Checker) GetNewerVersion(current string) string {
	cache := c.loadCache()

	// Trigger background refresh if stale (>24h) or missing
	if cache == nil || c.NowFn().Unix()-cache.LastCheck >= 86400 {
		go c.fetchAndCache()
	}

	if cache == nil || cache.Latest == "" {
		return ""
	}

	if compareVersions(cache.Latest, current) > 0 {
		return cache.Latest
	}
	return ""
}

func (c *Checker) loadCache() *CacheData {
	data, err := os.ReadFile(c.CachePath)
	if err != nil {
		return nil
	}
	var cache CacheData
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	return &cache
}

func (c *Checker) fetchAndCache() {
	version, err := c.FetchLatestFn()
	if err != nil || version == "" {
		return
	}

	cache := CacheData{
		LastCheck: c.NowFn().Unix(),
		Latest:    version,
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return
	}

	os.MkdirAll(filepath.Dir(c.CachePath), 0o755)
	tmp := c.CachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	os.Rename(tmp, c.CachePath)
}

func fetchLatestFromGitHub() (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/nicksenap/grove/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "grove")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return strings.TrimPrefix(release.TagName, "v"), nil
}

// compareVersions compares two semver strings.
// Returns -1 if a < b, 0 if equal, 1 if a > b.
func compareVersions(a, b string) int {
	av := parseVersion(a)
	bv := parseVersion(b)
	for i := 0; i < len(av) && i < len(bv); i++ {
		if av[i] < bv[i] {
			return -1
		}
		if av[i] > bv[i] {
			return 1
		}
	}
	return 0
}

func parseVersion(v string) []int {
	v = strings.TrimPrefix(v, "v")
	// Strip suffixes like "-go" or "-rc1"
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		v = v[:idx]
	}
	parts := strings.Split(v, ".")
	result := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		result[i] = n
	}
	return result
}

// FormatNotice returns a user-facing update notice, or "" if up-to-date.
func (c *Checker) FormatNotice(current string) string {
	newer := c.GetNewerVersion(current)
	if newer == "" {
		return ""
	}
	return fmt.Sprintf("A newer version of gw is available: %s → %s. Update with: brew upgrade gw", current, newer)
}
