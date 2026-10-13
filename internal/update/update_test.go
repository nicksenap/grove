package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func testChecker(t *testing.T) *Checker {
	t.Helper()
	dir := t.TempDir()
	return &Checker{
		CachePath:     filepath.Join(dir, "update-check.json"),
		NowFn:         time.Now,
		FetchLatestFn: func() (string, error) { return "", nil },
	}
}

func TestGetNewerVersionNoCacheReturnsEmpty(t *testing.T) {
	c := testChecker(t)
	v := c.GetNewerVersion("0.1.0")
	if v != "" {
		t.Errorf("expected empty with no cache, got %q", v)
	}
}

func TestGetNewerVersionCacheHit(t *testing.T) {
	c := testChecker(t)

	cache := CacheData{LastCheck: time.Now().Unix(), Latest: "0.2.0"}
	data, _ := json.Marshal(cache)
	os.WriteFile(c.CachePath, data, 0o644)

	v := c.GetNewerVersion("0.1.0")
	if v != "0.2.0" {
		t.Errorf("expected '0.2.0', got %q", v)
	}
}

func TestGetNewerVersionSameVersion(t *testing.T) {
	c := testChecker(t)

	cache := CacheData{LastCheck: time.Now().Unix(), Latest: "0.1.0"}
	data, _ := json.Marshal(cache)
	os.WriteFile(c.CachePath, data, 0o644)

	v := c.GetNewerVersion("0.1.0")
	if v != "" {
		t.Errorf("expected empty for same version, got %q", v)
	}
}

func TestGetNewerVersionOlderCache(t *testing.T) {
	c := testChecker(t)

	cache := CacheData{LastCheck: time.Now().Unix(), Latest: "0.0.9"}
	data, _ := json.Marshal(cache)
	os.WriteFile(c.CachePath, data, 0o644)

	v := c.GetNewerVersion("0.1.0")
	if v != "" {
		t.Errorf("expected empty for older cached version, got %q", v)
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		a, b string
		want int
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
	c := testChecker(t)

	// Write cache with old timestamp (25h ago)
	cache := CacheData{
		LastCheck: time.Now().Add(-25 * time.Hour).Unix(),
		Latest:    "0.2.0",
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(c.CachePath, data, 0o644)

	// Should still return the cached value (refresh happens in background)
	v := c.GetNewerVersion("0.1.0")
	if v != "0.2.0" {
		t.Errorf("stale cache should still return value, got %q", v)
	}
}

func TestStaleCacheTriggersBackgroundFetch(t *testing.T) {
	c := testChecker(t)

	// Track if fetcher was called
	var called int32
	done := make(chan struct{})
	c.FetchLatestFn = func() (string, error) {
		atomic.StoreInt32(&called, 1)
		close(done)
		return "0.3.0", nil
	}

	// Write stale cache
	cache := CacheData{
		LastCheck: time.Now().Add(-25 * time.Hour).Unix(),
		Latest:    "0.2.0",
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(c.CachePath, data, 0o644)

	c.GetNewerVersion("0.1.0")

	// Wait for background goroutine
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background fetch was not triggered")
	}

	if atomic.LoadInt32(&called) != 1 {
		t.Error("expected fetcher to be called for stale cache")
	}
}

func TestFreshCacheDoesNotFetch(t *testing.T) {
	c := testChecker(t)

	var called int32
	c.FetchLatestFn = func() (string, error) {
		atomic.StoreInt32(&called, 1)
		return "0.3.0", nil
	}

	// Write fresh cache
	cache := CacheData{
		LastCheck: time.Now().Unix(),
		Latest:    "0.2.0",
	}
	data, _ := json.Marshal(cache)
	os.WriteFile(c.CachePath, data, 0o644)

	c.GetNewerVersion("0.1.0")

	// Give goroutine a moment (should NOT be triggered)
	time.Sleep(50 * time.Millisecond)

	if atomic.LoadInt32(&called) != 0 {
		t.Error("fetcher should NOT be called for fresh cache")
	}
}

func TestFetchAndCacheWritesToDisk(t *testing.T) {
	c := testChecker(t)
	fixed := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	c.NowFn = func() time.Time { return fixed }
	c.FetchLatestFn = func() (string, error) {
		return "1.2.3", nil
	}

	c.fetchAndCache()

	data, err := os.ReadFile(c.CachePath)
	if err != nil {
		t.Fatal(err)
	}

	var cache CacheData
	json.Unmarshal(data, &cache)

	if cache.Latest != "1.2.3" {
		t.Errorf("latest: got %q", cache.Latest)
	}
	if cache.LastCheck != fixed.Unix() {
		t.Errorf("last_check: got %d, want %d", cache.LastCheck, fixed.Unix())
	}
}

func TestFormatNoticeUsesHomebrewFormulaName(t *testing.T) {
	c := testChecker(t)
	c.ExePathFn = func() (string, error) { return "/opt/homebrew/Cellar/grove/1.1.0/bin/gw", nil }
	cache := CacheData{LastCheck: time.Now().Unix(), Latest: "1.2.0"}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.CachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got := c.FormatNotice("1.1.0")
	want := "A newer version of gw is available: 1.1.0 → 1.2.0. Update with: brew update && brew upgrade grove"
	if got != want {
		t.Errorf("FormatNotice() = %q, want %q", got, want)
	}
}

func TestUpgradeHintByInstallMethod(t *testing.T) {
	t.Setenv("GOPATH", "/gopath")
	t.Setenv("GOBIN", "")
	tests := []struct {
		exe  string
		want string
	}{
		{"/opt/homebrew/Cellar/grove/1.1.0/bin/gw", "brew update && brew upgrade grove"},
		{"/home/linuxbrew/.linuxbrew/Cellar/grove/1.1.0/bin/gw", "brew update && brew upgrade grove"},
		{"/gopath/bin/gw", "go install github.com/nicksenap/grove/cmd/gw@latest"},
		{"/usr/local/bin/gw", "curl -fsSL https://raw.githubusercontent.com/nicksenap/grove/master/scripts/install.sh | sh"},
		{"", "curl -fsSL https://raw.githubusercontent.com/nicksenap/grove/master/scripts/install.sh | sh"},
	}
	for _, tt := range tests {
		c := testChecker(t)
		c.ExePathFn = func() (string, error) { return tt.exe, nil }
		if got := c.UpgradeHint(); got != tt.want {
			t.Errorf("UpgradeHint(%q) = %q, want %q", tt.exe, got, tt.want)
		}
	}
}

func TestFetchLatestFromRedirectReadsTagFromLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s, want HEAD", r.Method)
		}
		w.Header().Set("Location", "https://github.com/nicksenap/grove/releases/tag/v1.2.3")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	got, err := fetchLatestFromRedirect(srv.URL)
	if err != nil || got != "1.2.3" {
		t.Fatalf("got %q, %v; want 1.2.3", got, err)
	}
}

func TestFetchLatestFromRedirectRejectsNonRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := fetchLatestFromRedirect(srv.URL); err == nil {
		t.Fatal("expected error for 200 response")
	}
}

func TestVersionFromTagURL(t *testing.T) {
	good := map[string]string{
		"https://github.com/nicksenap/grove/releases/tag/v1.2.3": "1.2.3",
		"/nicksenap/grove/releases/tag/1.2.3":                    "1.2.3",
	}
	for in, want := range good {
		if got, err := versionFromTagURL(in); err != nil || got != want {
			t.Errorf("versionFromTagURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "https://github.com/nicksenap/grove/releases", "https://github.com/nicksenap/grove/releases/tag/", "https://github.com/x/releases/tag/v1/extra"} {
		if _, err := versionFromTagURL(bad); err == nil {
			t.Errorf("versionFromTagURL(%q) should fail", bad)
		}
	}
}
