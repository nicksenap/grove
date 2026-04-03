package discover

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Cache entry format ---

func TestCacheEntryRoundtrip(t *testing.T) {
	entry := CacheEntry{
		URL:   "git@github.com:owner/repo.git",
		Mtime: 1705312200.0,
		Ts:    1705312200.0,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}

	var got CacheEntry
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}

	if got != entry {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", got, entry)
	}
}

func TestCacheEntryEmptyURL(t *testing.T) {
	entry := CacheEntry{URL: "", Mtime: 1.0, Ts: 1.0}
	data, _ := json.Marshal(entry)

	var got CacheEntry
	json.Unmarshal(data, &got)

	if got.URL != "" {
		t.Errorf("expected empty URL, got %q", got.URL)
	}
}

// --- Load / Save ---

func TestLoadCacheEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cache", "remotes.json")

	cache := LoadRemoteCache(path)
	if len(cache) != 0 {
		t.Errorf("expected empty cache, got %d entries", len(cache))
	}
}

func TestLoadCacheCorrupt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "remotes.json")
	os.WriteFile(path, []byte("{invalid json"), 0o644)

	cache := LoadRemoteCache(path)
	if len(cache) != 0 {
		t.Errorf("corrupt file should return empty cache, got %d entries", len(cache))
	}
}

func TestSaveAndLoadCache(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	path := filepath.Join(cacheDir, "remotes.json")

	cache := map[string]CacheEntry{
		"/path/to/repo": {
			URL:   "git@github.com:owner/repo.git",
			Mtime: 100.0,
			Ts:    200.0,
		},
	}

	if err := SaveRemoteCache(path, cache); err != nil {
		t.Fatal(err)
	}

	loaded := LoadRemoteCache(path)
	if len(loaded) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded))
	}

	entry := loaded["/path/to/repo"]
	if entry.URL != "git@github.com:owner/repo.git" {
		t.Errorf("URL: got %q", entry.URL)
	}
	if entry.Mtime != 100.0 {
		t.Errorf("Mtime: got %f", entry.Mtime)
	}
}

func TestSaveCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "remotes.json")

	err := SaveRemoteCache(path, map[string]CacheEntry{})
	if err != nil {
		t.Fatalf("should create parent dirs: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("file should exist after save")
	}
}

// --- Cache hit/miss logic (fully injectable — no filesystem needed) ---

func stubMtime(val float64) func(string) float64 {
	return func(string) float64 { return val }
}

func TestResolveCachedHit(t *testing.T) {
	now := float64(time.Now().Unix())
	cache := map[string]CacheEntry{
		"/repo": {URL: "git@github.com:owner/repo.git", Mtime: 42.0, Ts: now},
	}

	url, hit := ResolveCached("/repo", cache, now, stubMtime(42.0))
	if !hit {
		t.Fatal("expected cache hit")
	}
	if url != "git@github.com:owner/repo.git" {
		t.Errorf("got %q", url)
	}
}

func TestResolveCachedMissNotInCache(t *testing.T) {
	cache := map[string]CacheEntry{}
	_, hit := ResolveCached("/nonexistent", cache, 1000.0, stubMtime(0))
	if hit {
		t.Error("expected miss for unknown path")
	}
}

func TestResolveCachedMissMtimeChanged(t *testing.T) {
	now := float64(time.Now().Unix())
	cache := map[string]CacheEntry{
		"/repo": {URL: "git@github.com:owner/repo.git", Mtime: 42.0, Ts: now},
	}

	// mtime function returns different value than cached
	_, hit := ResolveCached("/repo", cache, now, stubMtime(99.0))
	if hit {
		t.Error("expected miss when mtime changed")
	}
}

func TestResolveCachedMissTTLExpired(t *testing.T) {
	expired := float64(time.Now().Add(-25 * time.Hour).Unix())
	cache := map[string]CacheEntry{
		"/repo": {URL: "git@github.com:owner/repo.git", Mtime: 42.0, Ts: expired},
	}

	now := float64(time.Now().Unix())
	_, hit := ResolveCached("/repo", cache, now, stubMtime(42.0))
	if hit {
		t.Error("expected miss when TTL expired")
	}
}

func TestResolveCachedEmptyURLIsHit(t *testing.T) {
	now := float64(time.Now().Unix())
	cache := map[string]CacheEntry{
		"/repo": {URL: "", Mtime: 42.0, Ts: now},
	}

	url, hit := ResolveCached("/repo", cache, now, stubMtime(42.0))
	if !hit {
		t.Fatal("empty URL should still be a cache hit")
	}
	if url != "" {
		t.Errorf("expected empty URL, got %q", url)
	}
}

func TestGitConfigMtime(t *testing.T) {
	repoDir := t.TempDir()
	gitConfig := filepath.Join(repoDir, ".git", "config")
	os.MkdirAll(filepath.Dir(gitConfig), 0o755)
	os.WriteFile(gitConfig, []byte("[core]"), 0o644)

	mtime := GitConfigMtime(repoDir)
	if mtime == 0.0 {
		t.Error("expected non-zero mtime for existing .git/config")
	}
}

func TestGitConfigMtimeNonexistent(t *testing.T) {
	mtime := GitConfigMtime("/nonexistent/repo")
	if mtime != 0.0 {
		t.Errorf("expected 0 for missing .git/config, got %f", mtime)
	}
}

// --- Batch resolve (injectable clock + mtime) ---

func TestBatchResolveCacheHitsSkipFetch(t *testing.T) {
	now := float64(time.Now().Unix())
	mtime := stubMtime(42.0)

	cache := map[string]CacheEntry{
		"/repo1": {URL: "git@github.com:owner/repo1.git", Mtime: 42.0, Ts: now},
		// /repo2 is NOT cached
	}

	fetchCalls := 0
	fetcher := func(path string) string {
		fetchCalls++
		return "git@github.com:owner/repo2.git"
	}

	results := BatchResolveRemotes(
		[]string{"/repo1", "/repo2"},
		cache, fetcher,
		func() float64 { return now }, mtime,
	)

	if fetchCalls != 1 {
		t.Errorf("expected 1 fetch call (only repo2), got %d", fetchCalls)
	}
	if results["/repo1"] != "git@github.com:owner/repo1.git" {
		t.Errorf("repo1 URL: got %q", results["/repo1"])
	}
	if results["/repo2"] != "git@github.com:owner/repo2.git" {
		t.Errorf("repo2 URL: got %q", results["/repo2"])
	}
}

func TestBatchResolveUpdatesCache(t *testing.T) {
	now := float64(time.Now().Unix())
	cache := map[string]CacheEntry{}

	fetcher := func(path string) string {
		return "git@github.com:owner/repo.git"
	}

	BatchResolveRemotes(
		[]string{"/repo"}, cache, fetcher,
		func() float64 { return now }, stubMtime(42.0),
	)

	entry, ok := cache["/repo"]
	if !ok {
		t.Fatal("cache should be updated with new entry")
	}
	if entry.URL != "git@github.com:owner/repo.git" {
		t.Errorf("cached URL: got %q", entry.URL)
	}
	if entry.Ts != now {
		t.Errorf("cached Ts should be now (%f), got %f", now, entry.Ts)
	}
}

func TestBatchResolveAllCached(t *testing.T) {
	now := float64(time.Now().Unix())
	cache := map[string]CacheEntry{
		"/repo": {URL: "git@github.com:cached/repo.git", Mtime: 42.0, Ts: now},
	}

	fetchCalls := 0
	fetcher := func(path string) string {
		fetchCalls++
		return ""
	}

	results := BatchResolveRemotes(
		[]string{"/repo"}, cache, fetcher,
		func() float64 { return now }, stubMtime(42.0),
	)

	if fetchCalls != 0 {
		t.Errorf("expected 0 fetches for fully cached, got %d", fetchCalls)
	}
	if results["/repo"] != "git@github.com:cached/repo.git" {
		t.Errorf("got %q", results["/repo"])
	}
}
