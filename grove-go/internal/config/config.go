// Package config handles Grove's TOML configuration file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/nicksenap/grove/internal/models"
)

var (
	groveDir         string
	configPath       string
	defaultWSDir     string
	presetNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

func init() {
	home, _ := os.UserHomeDir()
	groveDir = filepath.Join(home, ".grove")
	configPath = filepath.Join(groveDir, "config.toml")
	defaultWSDir = filepath.Join(groveDir, "workspaces")
}

// GroveDir returns the path to ~/.grove.
func GroveDir() string { return groveDir }

// ConfigPath returns the path to ~/.grove/config.toml.
func ConfigPath() string { return configPath }

// DefaultWorkspaceDir returns the default workspace directory.
func DefaultWorkspaceDir() string { return defaultWSDir }

// EnsureGroveDir creates ~/.grove if it doesn't exist.
func EnsureGroveDir() error {
	return os.MkdirAll(groveDir, 0o755)
}

// tomlConfig is the on-disk TOML structure (supports legacy repos_dir).
type tomlConfig struct {
	RepoDirs      []string            `toml:"repo_dirs"`
	ReposDir      string              `toml:"repos_dir"` // legacy single-dir field
	WorkspaceDir  string              `toml:"workspace_dir"`
	Presets       map[string][]string `toml:"presets"`
	ClaudeMemSync *bool               `toml:"claude_memory_sync"`
}

// DefaultConfig returns a config with default values.
func DefaultConfig() models.Config {
	return models.Config{
		WorkspaceDir: defaultWSDir,
		Presets:      make(map[string][]string),
	}
}

// Load reads the config from ~/.grove/config.toml.
func Load() (*models.Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("grove not initialized — run 'gw init' first")
		}
		return nil, err
	}

	var tc tomlConfig
	if err := toml.Unmarshal(data, &tc); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg := &models.Config{
		RepoDirs:     tc.RepoDirs,
		WorkspaceDir: tc.WorkspaceDir,
		Presets:      tc.Presets,
	}

	// Migrate legacy repos_dir → repo_dirs
	if len(cfg.RepoDirs) == 0 && tc.ReposDir != "" {
		cfg.RepoDirs = []string{tc.ReposDir}
	}

	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = defaultWSDir
	}

	if cfg.Presets == nil {
		cfg.Presets = make(map[string][]string)
	}

	if tc.ClaudeMemSync != nil {
		cfg.ClaudeMemSync = *tc.ClaudeMemSync
	}

	// Expand ~ in paths
	cfg.WorkspaceDir = expandHome(cfg.WorkspaceDir)
	for i, d := range cfg.RepoDirs {
		cfg.RepoDirs[i] = expandHome(d)
	}

	return cfg, nil
}

// Save writes the config to ~/.grove/config.toml with hand-formatted TOML.
func Save(cfg *models.Config) error {
	if err := EnsureGroveDir(); err != nil {
		return err
	}

	var b strings.Builder
	// repo_dirs
	b.WriteString("repo_dirs = [\n")
	for _, d := range cfg.RepoDirs {
		b.WriteString(fmt.Sprintf("    %q,\n", d))
	}
	b.WriteString("]\n")

	// workspace_dir
	b.WriteString(fmt.Sprintf("workspace_dir = %q\n", cfg.WorkspaceDir))

	// claude_memory_sync
	b.WriteString(fmt.Sprintf("claude_memory_sync = %v\n", cfg.ClaudeMemSync))

	// presets
	if len(cfg.Presets) > 0 {
		b.WriteString("\n[presets]\n")
		for name, repos := range cfg.Presets {
			parts := make([]string, len(repos))
			for i, r := range repos {
				parts[i] = fmt.Sprintf("%q", r)
			}
			b.WriteString(fmt.Sprintf("%s = [%s]\n", name, strings.Join(parts, ", ")))
		}
	}

	return atomicWrite(configPath, b.String())
}

// Require returns the config or exits with an error if not initialized.
func Require() (*models.Config, error) {
	return Load()
}

// ValidatePresetName checks that a preset name is valid.
func ValidatePresetName(name string) error {
	if !presetNameRegexp.MatchString(name) {
		return fmt.Errorf("invalid preset name %q — use only letters, digits, hyphens, underscores", name)
	}
	return nil
}

// atomicWrite writes content to a temp file then renames to target.
func atomicWrite(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".grove-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}
