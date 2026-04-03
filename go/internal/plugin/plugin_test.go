package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/config"
)

func setupPluginDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	config.GroveDir = dir
	pluginsDir := filepath.Join(dir, "plugins")
	os.MkdirAll(pluginsDir, 0o755)
	return pluginsDir
}

func createFakePlugin(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, "gw-"+name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho hello\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDirUsesGroveDir(t *testing.T) {
	config.GroveDir = "/tmp/test-grove"
	got := Dir()
	want := "/tmp/test-grove/plugins"
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestFindInPluginsDir(t *testing.T) {
	dir := setupPluginDir(t)
	createFakePlugin(t, dir, "hello")

	path, err := Find("hello")
	if err != nil {
		t.Fatalf("Find(hello) error: %v", err)
	}
	if path != filepath.Join(dir, "gw-hello") {
		t.Errorf("Find(hello) = %q, want %q", path, filepath.Join(dir, "gw-hello"))
	}
}

func TestFindNotFound(t *testing.T) {
	setupPluginDir(t)

	_, err := Find("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing plugin")
	}
}

func TestListEmpty(t *testing.T) {
	setupPluginDir(t)

	plugins, err := List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("expected empty list, got %d", len(plugins))
	}
}

func TestListFindsPlugins(t *testing.T) {
	dir := setupPluginDir(t)
	createFakePlugin(t, dir, "alpha")
	createFakePlugin(t, dir, "beta")

	// Also create a non-plugin file (should be ignored)
	os.WriteFile(filepath.Join(dir, "not-a-plugin"), []byte("x"), 0o755)

	// And a non-executable file with gw- prefix (should be ignored)
	os.WriteFile(filepath.Join(dir, "gw-noexec"), []byte("x"), 0o644)

	plugins, err := List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugins, got %d", len(plugins))
	}

	names := map[string]bool{}
	for _, p := range plugins {
		names[p.Name] = true
	}
	if !names["alpha"] || !names["beta"] {
		t.Errorf("expected alpha and beta, got %v", names)
	}
}

func TestListNoDirReturnsNil(t *testing.T) {
	config.GroveDir = filepath.Join(t.TempDir(), "nonexistent")

	plugins, err := List()
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if plugins != nil {
		t.Errorf("expected nil, got %v", plugins)
	}
}

func TestRemovePlugin(t *testing.T) {
	dir := setupPluginDir(t)
	createFakePlugin(t, dir, "removeme")

	if err := Remove("removeme"); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	// Verify it's gone
	if _, err := os.Stat(filepath.Join(dir, "gw-removeme")); !os.IsNotExist(err) {
		t.Error("plugin file should have been removed")
	}
}

func TestRemoveNotInstalled(t *testing.T) {
	setupPluginDir(t)

	err := Remove("nothere")
	if err == nil {
		t.Fatal("expected error removing non-existent plugin")
	}
}

func TestRemoveCleansMeta(t *testing.T) {
	dir := setupPluginDir(t)
	createFakePlugin(t, dir, "withmeta")
	saveMeta("withmeta", "user/gw-withmeta", "v1.0.0")

	if err := Remove("withmeta"); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	if _, err := os.Stat(metaPath("withmeta")); !os.IsNotExist(err) {
		t.Error("metadata file should have been removed")
	}
}

func TestMetaRoundTrip(t *testing.T) {
	setupPluginDir(t)
	saveMeta("dash", "nicksenap/gw-dash", "v0.1.0")

	meta, err := loadMeta("dash")
	if err != nil {
		t.Fatalf("loadMeta() error: %v", err)
	}
	if meta.Repo != "nicksenap/gw-dash" {
		t.Errorf("repo = %q, want %q", meta.Repo, "nicksenap/gw-dash")
	}
	if meta.Version != "v0.1.0" {
		t.Errorf("version = %q, want %q", meta.Version, "v0.1.0")
	}
}

func TestUpgradeWithoutMeta(t *testing.T) {
	dir := setupPluginDir(t)
	createFakePlugin(t, dir, "manual")

	err := Upgrade("manual")
	if err == nil {
		t.Fatal("expected error upgrading plugin without metadata")
	}
}
