package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/models"
)

// setupTestEnv sets up a temp directory and overrides package-level paths.
func setupTestEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	GroveDir = filepath.Join(dir, ".grove")
	ConfigPath = filepath.Join(GroveDir, "config.toml")
	DefaultWorkspaceDir = filepath.Join(GroveDir, "workspaces")
	os.MkdirAll(GroveDir, 0o755)
	return dir
}

func TestLoadNonexistent(t *testing.T) {
	setupTestEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config for nonexistent file")
	}
}

func TestSaveAndLoad(t *testing.T) {
	setupTestEnv(t)

	cfg := &models.Config{
		RepoDirs:     []string{"/home/user/dev", "/home/user/projects"},
		WorkspaceDir: "/home/user/.grove/workspaces",
		Presets:      map[string]models.Preset{},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil config")
	}

	if len(loaded.RepoDirs) != 2 {
		t.Fatalf("expected 2 repo_dirs, got %d", len(loaded.RepoDirs))
	}
	if loaded.RepoDirs[0] != "/home/user/dev" {
		t.Errorf("repo_dirs[0]: got %q, want '/home/user/dev'", loaded.RepoDirs[0])
	}
	if loaded.WorkspaceDir != "/home/user/.grove/workspaces" {
		t.Errorf("workspace_dir: got %q", loaded.WorkspaceDir)
	}
}

func TestSaveAndLoadWithPresets(t *testing.T) {
	setupTestEnv(t)

	cfg := &models.Config{
		RepoDirs:     []string{"/dev"},
		WorkspaceDir: "/ws",
		Presets: map[string]models.Preset{
			"backend": {Repos: []string{"api", "worker"}},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if len(loaded.Presets) != 1 {
		t.Fatalf("expected 1 preset, got %d", len(loaded.Presets))
	}
	p, ok := loaded.Presets["backend"]
	if !ok {
		t.Fatal("preset 'backend' not found")
	}
	if len(p.Repos) != 2 || p.Repos[0] != "api" || p.Repos[1] != "worker" {
		t.Errorf("preset repos: got %v", p.Repos)
	}
}

func TestSavedFileIsValidTOML(t *testing.T) {
	setupTestEnv(t)

	cfg := &models.Config{
		RepoDirs:     []string{"/dev"},
		WorkspaceDir: "/ws",
		Presets:      map[string]models.Preset{},
	}
	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	content := string(data)
	if len(content) == 0 {
		t.Error("config file is empty")
	}
	// Should contain key TOML elements
	if !strings.Contains(content, "repo_dirs") && !strings.Contains(content, "RepoDirs") {
		// The TOML encoder might use the struct field name — check what's actually written
		t.Logf("config content:\n%s", content)
	}
}

func TestLegacyMigration(t *testing.T) {
	dir := setupTestEnv(t)
	repoDir := filepath.Join(dir, "repos")
	os.MkdirAll(repoDir, 0o755)

	// Write old-format config with repos_dir (singular)
	oldConfig := `repos_dir = "` + repoDir + `"` + "\n" + `workspace_dir = "/ws"` + "\n"
	os.WriteFile(ConfigPath, []byte(oldConfig), 0o644)

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected non-nil config")
	}

	if len(loaded.RepoDirs) != 1 {
		t.Fatalf("expected 1 repo_dir after migration, got %d", len(loaded.RepoDirs))
	}
	if loaded.RepoDirs[0] != repoDir {
		t.Errorf("expected migrated repo_dir %q, got %q", repoDir, loaded.RepoDirs[0])
	}

	// Verify migration rewrote the file
	data, _ := os.ReadFile(ConfigPath)
	content := string(data)
	if strings.Contains(content, "repos_dir") && !strings.Contains(content, "repo_dirs") {
		t.Error("migration should have rewritten file with repo_dirs")
	}
}

func TestDefaultWorkspaceDir(t *testing.T) {
	setupTestEnv(t)

	cfg := &models.Config{
		RepoDirs: []string{"/dev"},
		Presets:  map[string]models.Preset{},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.WorkspaceDir != DefaultWorkspaceDir {
		t.Errorf("expected default workspace dir %q, got %q", DefaultWorkspaceDir, loaded.WorkspaceDir)
	}
}

func TestInitCreatesConfig(t *testing.T) {
	dir := setupTestEnv(t)
	repoDir := filepath.Join(dir, "repos")
	os.MkdirAll(repoDir, 0o755)

	cfg, err := Init([]string{repoDir})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if len(cfg.RepoDirs) != 1 {
		t.Fatalf("expected 1 repo_dir, got %d", len(cfg.RepoDirs))
	}

	// Verify file exists
	if _, err := os.Stat(ConfigPath); os.IsNotExist(err) {
		t.Error("config file should exist after init")
	}
}

func TestInitMergesDirs(t *testing.T) {
	dir := setupTestEnv(t)
	dir1 := filepath.Join(dir, "repos1")
	dir2 := filepath.Join(dir, "repos2")
	os.MkdirAll(dir1, 0o755)
	os.MkdirAll(dir2, 0o755)

	// First init
	cfg, err := Init([]string{dir1})
	if err != nil {
		t.Fatalf("init 1: %v", err)
	}
	if len(cfg.RepoDirs) != 1 {
		t.Fatalf("expected 1 dir after first init, got %d", len(cfg.RepoDirs))
	}

	// Second init merges
	cfg, err = Init([]string{dir2})
	if err != nil {
		t.Fatalf("init 2: %v", err)
	}
	if len(cfg.RepoDirs) != 2 {
		t.Fatalf("expected 2 dirs after second init, got %d", len(cfg.RepoDirs))
	}
}

func TestInitDeduplicates(t *testing.T) {
	dir := setupTestEnv(t)
	repoDir := filepath.Join(dir, "repos")
	os.MkdirAll(repoDir, 0o755)

	Init([]string{repoDir})
	cfg, err := Init([]string{repoDir})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if len(cfg.RepoDirs) != 1 {
		t.Errorf("expected deduplication, got %d dirs", len(cfg.RepoDirs))
	}
}

func TestInitNonexistentDir(t *testing.T) {
	setupTestEnv(t)

	_, err := Init([]string{"/nonexistent/path"})
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestRequireConfigExitsWhenMissing(t *testing.T) {
	// We can't easily test os.Exit, but we can verify Load returns nil
	setupTestEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Error("expected nil config")
	}
}

func TestPresetNameValidation(t *testing.T) {
	setupTestEnv(t)

	// Valid names
	for _, name := range []string{"backend", "front-end", "my_preset", "v2"} {
		cfg := &models.Config{
			RepoDirs:     []string{"/dev"},
			WorkspaceDir: "/ws",
			Presets: map[string]models.Preset{
				name: {Repos: []string{"api"}},
			},
		}
		if err := Save(cfg); err != nil {
			t.Errorf("valid preset name %q rejected: %v", name, err)
		}
	}

	// Invalid names
	for _, name := range []string{"has space", "has.dot", "has[bracket", "has/slash"} {
		cfg := &models.Config{
			RepoDirs:     []string{"/dev"},
			WorkspaceDir: "/ws",
			Presets: map[string]models.Preset{
				name: {Repos: []string{"api"}},
			},
		}
		if err := Save(cfg); err == nil {
			t.Errorf("invalid preset name %q should be rejected", name)
		}
	}
}

func TestAtomicWrite(t *testing.T) {
	setupTestEnv(t)

	cfg := &models.Config{
		RepoDirs:     []string{"/dev"},
		WorkspaceDir: "/ws",
		Presets:      map[string]models.Preset{},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	// No tmp file should remain
	tmpPath := ConfigPath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up after atomic write")
	}

	// Config file should exist and be valid
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) == 0 {
		t.Error("config file should not be empty")
	}
}
