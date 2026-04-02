package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	CacheDir = dir
	return dir
}

func TestGetNewerVersionNoCacheReturnsNil(t *testing.T) {
	setupTestEnv(t)

	// No cache file — should return empty (triggers background fetch)
	v := GetNewerVersion("0.1.0")
	if v != "" {
		t.Errorf("expected empty with no cache, got %q", v)
	}
}

func TestGetNewerVersionCacheHit(t *testing.T) {
	dir := setupTestEnv(t)

	// Write cache with a newer version
	cache := CacheData{
		LastCheck: time.Now().Unix(),
		Latest:   "0.2.0",
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(filepath.Join(dir, "update-check.json"), data, 0o644)

	v := GetNewerVersion("0.1.0")
	if v != "0.2.0" {
		t.Errorf("expected '0.2.0', got %q", v)
	}
}

func TestGetNewerVersionSameVersion(t *testing.T) {
	dir := setupTestEnv(t)

	cache := CacheData{
		LastCheck: time.Now().Unix(),
		Latest:   "0.1.0",
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(filepath.Join(dir, "update-check.json"), data, 0o644)

	v := GetNewerVersion("0.1.0")
	if v != "" {
		t.Errorf("expected empty for same version, got %q", v)
	}
}

func TestGetNewerVersionOlderCache(t *testing.T) {
	dir := setupTestEnv(t)

	cache := CacheData{
		LastCheck: time.Now().Unix(),
		Latest:   "0.0.9",
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(filepath.Join(dir, "update-check.json"), data, 0o644)

	v := GetNewerVersion("0.1.0")
	if v != "" {
		t.Errorf("expected empty for older cached version, got %q", v)
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		a, b string
		want int // -1 = a < b, 0 = equal, 1 = a > b
	}{
		{"0.1.0", "0.2.0", -1},
		{"0.2.0", "0.1.0", 1},
		{"0.1.0", "0.1.0", 0},
		{"1.0.0", "0.99.99", 1},
		{"0.12.13", "0.13.0", -1},
	}
	for _, tt := range tests {
		got := compareVersions(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestStaleCacheTriggersRefresh(t *testing.T) {
	dir := setupTestEnv(t)

	// Write cache with old timestamp (25h ago)
	cache := CacheData{
		LastCheck: time.Now().Add(-25 * time.Hour).Unix(),
		Latest:   "0.2.0",
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(filepath.Join(dir, "update-check.json"), data, 0o644)

	// Should still return the cached value (refresh happens in background)
	v := GetNewerVersion("0.1.0")
	if v != "0.2.0" {
		t.Errorf("stale cache should still return value, got %q", v)
	}
}
