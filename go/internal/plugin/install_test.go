package plugin

import (
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
