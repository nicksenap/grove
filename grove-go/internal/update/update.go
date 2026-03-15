// Package update provides background version checking against GitHub releases.
package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/config"
)

const (
	cacheTTL   = 24 * time.Hour
	releaseURL = "https://api.github.com/repos/nicksenap/grove/releases/latest"
	cacheFile  = "update-check.json"
)

type updateCache struct {
	Version   string `json:"version"`
	CheckedAt int64  `json:"checked_at"`
}

func cachePath() string {
	return filepath.Join(config.GroveDir(), cacheFile)
}

// GetNewerVersion returns the latest version if it's newer than current, or "".
// Never blocks, never raises. Kicks off a background refresh if cache is stale.
func GetNewerVersion(current string) string {
	cache := loadCache()

	// Kick off background refresh if stale
	if cache == nil || time.Since(time.Unix(cache.CheckedAt, 0)) > cacheTTL {
		go fetchAndCache()
	}

	if cache == nil || cache.Version == "" {
		return ""
	}

	if isNewer(cache.Version, current) {
		return cache.Version
	}
	return ""
}

func loadCache() *updateCache {
	data, err := os.ReadFile(cachePath())
	if err != nil {
		return nil
	}
	var cache updateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	return &cache
}

func fetchAndCache() {
	version := fetchLatest()
	if version == "" {
		return
	}
	cache := updateCache{
		Version:   version,
		CheckedAt: time.Now().Unix(),
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(cachePath(), data, 0o644)
}

func fetchLatest() string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(releaseURL)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return ""
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}

	return strings.TrimPrefix(release.TagName, "v")
}

// isNewer returns true if latest > current using simple string comparison.
func isNewer(latest, current string) bool {
	latestParts := parseVersion(latest)
	currentParts := parseVersion(current)

	for i := 0; i < len(latestParts) && i < len(currentParts); i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}
	return len(latestParts) > len(currentParts)
}

func parseVersion(v string) []int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	result := make([]int, len(parts))
	for i, p := range parts {
		fmt.Sscanf(p, "%d", &result[i])
	}
	return result
}
