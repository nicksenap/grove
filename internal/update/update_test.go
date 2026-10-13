package update

import (
	"encoding/json"
	"errors"
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

// Every way the redirect lookup can fail must surface as an error so the
// checker leaves the cache untouched and shows no notice.
func TestFetchLatestFromRedirectFailureModes(t *testing.T) {
	cases := map[string]http.HandlerFunc{
		"200 instead of redirect": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
		"404 repo missing": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
		"429 rate limited": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		},
		"503 outage": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		},
		"302 without Location": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusFound)
		},
		"302 to /releases (no releases yet)": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://github.com/nicksenap/grove/releases")
			w.WriteHeader(http.StatusFound)
		},
		"302 to login page": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "https://github.com/login?return_to=%2Fnicksenap%2Fgrove")
			w.WriteHeader(http.StatusFound)
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(h)
			defer srv.Close()
			if v, err := fetchLatestFromRedirect(srv.URL); err == nil {
				t.Fatalf("got %q, want error", v)
			}
		})
	}
}

func TestFetchLatestFromRedirectNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // connection refused from here on

	if v, err := fetchLatestFromRedirect(url); err == nil {
		t.Fatalf("got %q, want error", v)
	}
}

func TestFetchLatestFromRedirectTimeout(t *testing.T) {
	old := fetchTimeout
	fetchTimeout = 50 * time.Millisecond
	t.Cleanup(func() { fetchTimeout = old })

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	start := time.Now()
	if v, err := fetchLatestFromRedirect(srv.URL); err == nil {
		t.Fatalf("got %q, want timeout error", v)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("took %v, timeout not honored", d)
	}
}

// The tag lives in the Location header; following the redirect would waste a
// request and lose it.
func TestFetchLatestFromRedirectDoesNotFollow(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/latest" {
			w.Header().Set("Location", "/nicksenap/grove/releases/tag/v2.0.0")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	got, err := fetchLatestFromRedirect(srv.URL + "/latest")
	if err != nil || got != "2.0.0" {
		t.Fatalf("got %q, %v; want 2.0.0", got, err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("server hit %d times, want 1", n)
	}
}

func TestFetchAndCacheLeavesCacheOnFetchError(t *testing.T) {
	c := testChecker(t)
	prev := CacheData{LastCheck: 1, Latest: "1.0.0"}
	data, _ := json.Marshal(prev)
	if err := os.WriteFile(c.CachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	c.FetchLatestFn = func() (string, error) { return "", errors.New("boom") }

	c.fetchAndCache()

	got := c.loadCache()
	if got == nil || *got != prev {
		t.Fatalf("cache = %+v, want unchanged %+v", got, prev)
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
