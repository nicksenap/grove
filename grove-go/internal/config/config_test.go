package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/models"
)

func TestSaveAndLoad(t *testing.T) {
	// Use a temp dir as grove home
	tmp := t.TempDir()
	origDir := groveDir
	origPath := configPath
	groveDir = tmp
	configPath = filepath.Join(tmp, "config.toml")
	t.Cleanup(func() {
		groveDir = origDir
		configPath = origPath
	})

	cfg := &models.Config{
		RepoDirs:      []string{"/home/user/repos", "/home/user/work"},
		WorkspaceDir:  "/home/user/.grove/workspaces",
		ClaudeMemSync: true,
		Presets: map[string][]string{
			"backend": {"svc-api", "svc-auth"},
		},
	}

	if err := Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.RepoDirs) != 2 {
		t.Errorf("RepoDirs count = %d, want 2", len(loaded.RepoDirs))
	}
	if loaded.RepoDirs[0] != "/home/user/repos" {
		t.Errorf("RepoDirs[0] = %q, want %q", loaded.RepoDirs[0], "/home/user/repos")
	}
	if loaded.WorkspaceDir != "/home/user/.grove/workspaces" {
		t.Errorf("WorkspaceDir = %q", loaded.WorkspaceDir)
	}
	if !loaded.ClaudeMemSync {
		t.Error("ClaudeMemSync = false, want true")
	}
	if len(loaded.Presets["backend"]) != 2 {
		t.Errorf("Presets[backend] = %v", loaded.Presets["backend"])
	}
}

func TestLoadMissing(t *testing.T) {
	origPath := configPath
	configPath = filepath.Join(t.TempDir(), "nonexistent.toml")
	t.Cleanup(func() { configPath = origPath })

	_, err := Load()
	if err == nil {
		t.Error("expected error for missing config")
	}
}

func TestLegacyReposDirMigration(t *testing.T) {
	tmp := t.TempDir()
	origDir := groveDir
	origPath := configPath
	groveDir = tmp
	configPath = filepath.Join(tmp, "config.toml")
	t.Cleanup(func() {
		groveDir = origDir
		configPath = origPath
	})

	// Write a config with the legacy repos_dir field
	content := `repos_dir = "/home/user/repos"
workspace_dir = "/home/user/.grove/workspaces"
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.RepoDirs) != 1 || loaded.RepoDirs[0] != "/home/user/repos" {
		t.Errorf("RepoDirs = %v, want [/home/user/repos]", loaded.RepoDirs)
	}
}

func TestValidatePresetName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{"backend", false},
		{"my-preset", false},
		{"my_preset", false},
		{"Backend123", false},
		{"bad name", true},
		{"bad/name", true},
		{"", true},
	}
	for _, tt := range tests {
		err := ValidatePresetName(tt.name)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidatePresetName(%q) err=%v, wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Presets == nil {
		t.Error("Presets should not be nil")
	}
	if cfg.WorkspaceDir == "" {
		t.Error("WorkspaceDir should not be empty")
	}
}

func TestExpandHome(t *testing.T) {
	home, _ := os.UserHomeDir()
	got := expandHome("~/test")
	want := filepath.Join(home, "test")
	if got != want {
		t.Errorf("expandHome(~/test) = %q, want %q", got, want)
	}

	// Non-home path should be unchanged
	got = expandHome("/absolute/path")
	if got != "/absolute/path" {
		t.Errorf("expandHome(/absolute/path) = %q", got)
	}
}
