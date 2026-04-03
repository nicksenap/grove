package discover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const CacheTTLSeconds = 86400 // 24 hours

// CacheEntry represents a cached remote URL for a repo.
type CacheEntry struct {
	URL   string  `json:"url"`
	Mtime float64 `json:"mtime"`
	Ts    float64 `json:"ts"`
}

// LoadRemoteCache reads the on-disk remote URL cache.
// Returns empty map on any error (missing file, corrupt JSON).
func LoadRemoteCache(path string) map[string]CacheEntry {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]CacheEntry{}
	}
	var cache map[string]CacheEntry
	if err := json.Unmarshal(data, &cache); err != nil {
		return map[string]CacheEntry{}
	}
	return cache
}

// SaveRemoteCache persists the cache to disk, creating parent dirs as needed.
func SaveRemoteCache(path string, cache map[string]CacheEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// GitConfigMtime returns the mtime of .git/config as a float64 unix timestamp.
// Returns 0.0 if the file doesn't exist.
func GitConfigMtime(repoPath string) float64 {
	info, err := os.Stat(filepath.Join(repoPath, ".git", "config"))
	if err != nil {
		return 0.0
	}
	return float64(info.ModTime().Unix())
}

// ResolveCached looks up a remote URL from cache.
// Returns (url, true) on hit, ("", false) on miss.
// mtimeFn is called to get the current .git/config mtime for invalidation.
func ResolveCached(repoPath string, cache map[string]CacheEntry, now float64, mtimeFn func(string) float64) (string, bool) {
	entry, ok := cache[repoPath]
	if !ok {
		return "", false
	}
	if entry.Mtime != mtimeFn(repoPath) {
		return "", false
	}
	if now-entry.Ts > float64(CacheTTLSeconds) {
		return "", false
	}
	return entry.URL, true
}

// BatchResolveRemotes resolves remote URLs for repos using cache + parallel fetching.
// The fetcher function is called for cache misses. Cache is mutated in-place with new entries.
// nowFn returns the current time; mtimeFn returns the .git/config mtime for a repo path.
func BatchResolveRemotes(
	repoPaths []string,
	cache map[string]CacheEntry,
	fetcher func(path string) string,
	nowFn func() float64,
	mtimeFn func(string) float64,
) map[string]string {
	now := nowFn()
	results := make(map[string]string, len(repoPaths))
	var toFetch []string

	for _, p := range repoPaths {
		if url, hit := ResolveCached(p, cache, now, mtimeFn); hit {
			results[p] = url
		} else {
			toFetch = append(toFetch, p)
		}
	}

	if len(toFetch) == 0 {
		return results
	}

	// Parallel fetch for cache misses
	type fetchResult struct {
		path string
		url  string
	}
	ch := make(chan fetchResult, len(toFetch))
	var wg sync.WaitGroup

	// Limit concurrency to 16 goroutines
	sem := make(chan struct{}, 16)
	for _, p := range toFetch {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			url := fetcher(path)
			ch <- fetchResult{path: path, url: url}
		}(p)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	for r := range ch {
		results[r.path] = r.url
		cache[r.path] = CacheEntry{
			URL:   r.url,
			Mtime: mtimeFn(r.path),
			Ts:    now,
		}
	}

	return results
}

// defaultNow returns the current unix timestamp as float64.
func defaultNow() float64 {
	return float64(time.Now().Unix())
}
