package plugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInstallRejectsUnsafePluginName(t *testing.T) {
	err := Install("owner/gw-../../bin/sh")
	if err == nil || !strings.Contains(err.Error(), "invalid plugin name") {
		t.Fatalf("error = %v, want invalid plugin name", err)
	}
}

func TestUpgradeRejectsUnsafePluginName(t *testing.T) {
	err := Upgrade("../../bin/sh")
	if err == nil || !strings.Contains(err.Error(), "invalid plugin name") {
		t.Fatalf("error = %v, want invalid plugin name", err)
	}
}

func TestParseRepo(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{"nicksenap/gw-dash", "nicksenap", "gw-dash", false},
		{"github.com/nicksenap/gw-dash", "nicksenap", "gw-dash", false},
		{"https://github.com/nicksenap/gw-dash", "nicksenap", "gw-dash", false},
		{"http://github.com/nicksenap/gw-dash", "nicksenap", "gw-dash", false},
		{"github.com/nicksenap/gw-dash/", "nicksenap", "gw-dash", false},
		{"invalid", "", "", true},
		{"", "", "", true},
		{"/only-name", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			owner, name, err := parseRepo(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseRepo(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if owner != tt.wantOwner {
				t.Errorf("parseRepo(%q) owner = %q, want %q", tt.input, owner, tt.wantOwner)
			}
			if name != tt.wantName {
				t.Errorf("parseRepo(%q) name = %q, want %q", tt.input, name, tt.wantName)
			}
		})
	}
}

func TestFetchReleaseWithoutTokenUsesPublicReleaseRoute(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	assetName := fmt.Sprintf("gw-test_0.1.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	apiCalled := false
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/owner/gw-test/releases/latest":
			http.Redirect(w, r, srv.URL+"/owner/gw-test/releases/tag/v0.1.0", http.StatusFound)
		case "/owner/gw-test/releases/tag/v0.1.0":
			w.WriteHeader(http.StatusOK)
		case "/owner/gw-test/releases/download/v0.1.0/" + assetName:
			w.WriteHeader(http.StatusOK)
		case "/repos/owner/gw-test/releases/latest":
			apiCalled = true
			http.Error(w, "API should not be needed", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	release, err := (releaseFetcher{
		client:     srv.Client(),
		webBaseURL: srv.URL,
		apiBaseURL: srv.URL,
	}).fetch("owner", "gw-test")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if apiCalled {
		t.Fatal("GitHub API should not be called for a conventional public release")
	}
	if release.TagName != "v0.1.0" {
		t.Fatalf("TagName = %q, want v0.1.0", release.TagName)
	}
	if len(release.Assets) != 2 || release.Assets[0].Name != assetName || release.Assets[1].Name != "checksums.txt" {
		t.Fatalf("Assets = %#v, want archive and checksums", release.Assets)
	}
}

func TestFetchReleaseFallsBackToAPIForNonstandardAssets(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")

	assetName := fmt.Sprintf("gw-test-custom-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	apiCalled := false
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/owner/gw-test/releases/latest":
			http.Redirect(w, r, srv.URL+"/owner/gw-test/releases/tag/v0.1.0", http.StatusFound)
		case r.URL.Path == "/owner/gw-test/releases/tag/v0.1.0":
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/owner/gw-test/releases/download/"):
			http.NotFound(w, r)
		case r.URL.Path == "/repos/owner/gw-test/releases/latest":
			apiCalled = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"tag_name":"v0.1.0","assets":[{"name":%q,"browser_download_url":%q}]}`,
				assetName, srv.URL+"/assets/custom.tar.gz")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	release, err := (releaseFetcher{
		client:     srv.Client(),
		webBaseURL: srv.URL,
		apiBaseURL: srv.URL,
	}).fetch("owner", "gw-test")
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if !apiCalled {
		t.Fatal("GitHub API fallback was not called")
	}
	if len(release.Assets) != 1 || release.Assets[0].Name != assetName {
		t.Fatalf("Assets = %#v, want API assets", release.Assets)
	}
}

func TestFindAsset(t *testing.T) {
	release := &ghRelease{
		TagName: "v0.1.0",
		Assets: []ghAsset{
			{Name: "gw-dash_0.1.0_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/darwin_arm64.tar.gz"},
			{Name: "gw-dash_0.1.0_darwin_amd64.tar.gz", BrowserDownloadURL: "https://example.com/darwin_amd64.tar.gz"},
			{Name: "gw-dash_0.1.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux_amd64.tar.gz"},
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		},
	}

	asset, err := findAsset(release, "gw-dash")
	if err != nil {
		t.Fatalf("findAsset() error: %v", err)
	}
	// Should find something for the current GOOS/GOARCH
	if asset == nil {
		t.Fatal("expected an asset, got nil")
	}
	if asset.BrowserDownloadURL == "" {
		t.Error("expected a download URL")
	}
}

func TestFindAssetNoMatch(t *testing.T) {
	release := &ghRelease{
		TagName: "v0.1.0",
		Assets: []ghAsset{
			{Name: "gw-dash_0.1.0_plan9_mips.tar.gz", BrowserDownloadURL: "https://example.com/plan9.tar.gz"},
		},
	}

	_, err := findAsset(release, "gw-dash")
	if err == nil {
		t.Fatal("expected error when no matching asset")
	}
}

// makeTarGz creates a .tar.gz archive containing a single file with the given name and content.
func makeTarGz(t *testing.T, fileName string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: fileName,
		Size: int64(len(content)),
		Mode: 0o755,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func TestDownloadAndExtractWithValidChecksum(t *testing.T) {
	archive := makeTarGz(t, "gw-test", []byte("#!/bin/sh\necho ok\n"))

	h := sha256.Sum256(archive)
	checksum := hex.EncodeToString(h[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "gw-test")
	checksums := map[string]string{"test.tar.gz": checksum}

	err := downloadAndExtract(srv.URL+"/test.tar.gz", "gw-test", dest, "test.tar.gz", checksums)
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}

	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Error("binary should exist after extraction")
	}
}

func TestDownloadAndExtractWithBadChecksum(t *testing.T) {
	archive := makeTarGz(t, "gw-test", []byte("#!/bin/sh\necho ok\n"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "gw-test")
	checksums := map[string]string{"test.tar.gz": "0000000000000000000000000000000000000000000000000000000000000000"}

	err := downloadAndExtract(srv.URL+"/test.tar.gz", "gw-test", dest, "test.tar.gz", checksums)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("checksum mismatch")) {
		t.Errorf("expected 'checksum mismatch' in error, got: %v", err)
	}

	// Binary should NOT exist
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("binary should not exist after checksum failure")
	}
}

func TestDownloadAndExtractWithNoChecksums(t *testing.T) {
	archive := makeTarGz(t, "gw-test", []byte("#!/bin/sh\necho ok\n"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "gw-test")

	// nil checksums — should still work (no verification)
	err := downloadAndExtract(srv.URL+"/test.tar.gz", "gw-test", dest, "test.tar.gz", nil)
	if err != nil {
		t.Fatalf("expected success without checksums, got: %v", err)
	}

	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Error("binary should exist")
	}
}

func TestFetchChecksumsFromRelease(t *testing.T) {
	checksumContent := fmt.Sprintf("%s  gw-test_0.1.0_darwin_arm64.tar.gz\n%s  gw-test_0.1.0_linux_amd64.tar.gz\n",
		"abc123def456abc123def456abc123def456abc123def456abc123def456abcd",
		"789012fed789012fed789012fed789012fed789012fed789012fed789012fedc",
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(checksumContent))
	}))
	defer srv.Close()

	release := &ghRelease{
		TagName: "v0.1.0",
		Assets: []ghAsset{
			{Name: "checksums.txt", BrowserDownloadURL: srv.URL + "/checksums.txt"},
			{Name: "gw-test_0.1.0_darwin_arm64.tar.gz", BrowserDownloadURL: srv.URL + "/darwin.tar.gz"},
		},
	}

	checksums := fetchChecksums(release)
	if checksums == nil {
		t.Fatal("expected checksums map")
	}
	if len(checksums) != 2 {
		t.Errorf("expected 2 entries, got %d", len(checksums))
	}
	if checksums["gw-test_0.1.0_darwin_arm64.tar.gz"] != "abc123def456abc123def456abc123def456abc123def456abc123def456abcd" {
		t.Errorf("wrong checksum for darwin: %s", checksums["gw-test_0.1.0_darwin_arm64.tar.gz"])
	}
}

func TestFetchChecksumsNoChecksumAsset(t *testing.T) {
	release := &ghRelease{
		TagName: "v0.1.0",
		Assets: []ghAsset{
			{Name: "gw-test_0.1.0_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/x.tar.gz"},
		},
	}

	checksums := fetchChecksums(release)
	if checksums != nil {
		t.Errorf("expected nil when no checksums.txt, got %v", checksums)
	}
}
