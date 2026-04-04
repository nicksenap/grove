package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/BurntSushi/toml"
	"github.com/nicksenap/grove/internal/models"
)

var validPresetName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var (
	// GroveDir is the root Grove directory. Override in tests.
	GroveDir string
	// ConfigPath is the path to config.toml. Override in tests.
	ConfigPath string
	// DefaultWorkspaceDir is the default workspace directory.
	DefaultWorkspaceDir string
)

func init() {
	home, _ := os.UserHomeDir()
	GroveDir = filepath.Join(home, ".grove")
	ConfigPath = filepath.Join(GroveDir, "config.toml")
	DefaultWorkspaceDir = filepath.Join(GroveDir, "workspaces")
}

// Load reads the config file. Returns nil config (not error) if file doesn't exist.
func Load() (*models.Config, error) {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cfg models.Config
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	// Migrate legacy repos_dir → repo_dirs
	if cfg.ReposDir != "" && len(cfg.RepoDirs) == 0 {
		cfg.RepoDirs = []string{cfg.ReposDir}
		cfg.ReposDir = ""
		// Save migrated config
		if err := Save(&cfg); err != nil {
			return nil, fmt.Errorf("migrating config: %w", err)
		}
	}

	// Default workspace dir
	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = DefaultWorkspaceDir
	}

	return &cfg, nil
}

// RequireConfig loads config and exits if not initialized.
func RequireConfig() *models.Config {
	cfg, err := Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[1;31merror:\033[0m %s\n", err)
		os.Exit(1)
	}
	if cfg == nil {
		fmt.Fprintf(os.Stderr, "\033[1;31merror:\033[0m Grove not initialized. Run: gw init <repo-dir>\n")
		os.Exit(1)
	}
	return cfg
}

// Save writes config to disk atomically.
func Save(cfg *models.Config) error {
	// Validate preset names
	for name := range cfg.Presets {
		if !validPresetName.MatchString(name) {
			return fmt.Errorf("invalid preset name %q: must match [a-zA-Z0-9_-]+", name)
		}
	}

	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0o755); err != nil {
		return err
	}

	tmp := ConfigPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	enc := toml.NewEncoder(f)
	if err := enc.Encode(cfg); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()

	return os.Rename(tmp, ConfigPath)
}

// Init initializes Grove with the given repo directories.
func Init(dirs []string) (*models.Config, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}

	if cfg == nil {
		cfg = &models.Config{
			RepoDirs:     []string{},
			WorkspaceDir: DefaultWorkspaceDir,
			Presets:      make(map[string]models.Preset),
		}
	}

	// Merge new dirs (deduplicate)
	existing := make(map[string]bool)
	for _, d := range cfg.RepoDirs {
		existing[d] = true
	}
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			return nil, fmt.Errorf("resolving path %s: %w", d, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("directory does not exist: %s", abs)
		}
		if !existing[abs] {
			cfg.RepoDirs = append(cfg.RepoDirs, abs)
			existing[abs] = true
		}
	}

	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = DefaultWorkspaceDir
	}

	// Ensure directories exist
	if err := os.MkdirAll(GroveDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.WorkspaceDir, 0o755); err != nil {
		return nil, err
	}

	if err := Save(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
