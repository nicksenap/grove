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
	"testing"
)

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
