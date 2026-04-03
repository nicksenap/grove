// Package plugin implements external binary plugin discovery and execution.
// Plugins are executables named gw-<name> found in ~/.grove/plugins/ or $PATH.
package plugin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/state"
)

// Dir returns the plugin directory path.
func Dir() string {
	return filepath.Join(config.GroveDir, "plugins")
}

// Find locates a plugin binary by name. Checks ~/.grove/plugins/ first, then $PATH.
// Returns the absolute path to the binary, or error if not found.
func Find(name string) (string, error) {
	bin := "gw-" + name

	// Check plugins dir first
	pluginPath := filepath.Join(Dir(), bin)
	if info, err := os.Stat(pluginPath); err == nil && !info.IsDir() {
		return pluginPath, nil
	}

	// Fall back to $PATH
	if p, err := exec.LookPath(bin); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("plugin %q not found (looked for %s in %s and $PATH)", name, bin, Dir())
}

// Exec replaces the current process with the plugin binary.
// On Unix this uses syscall.Exec; on Windows it spawns a child process.
func Exec(pluginPath string, args []string) error {
	env := pluginEnv()

	argv := append([]string{pluginPath}, args...)

	if runtime.GOOS == "windows" {
		cmd := exec.Command(pluginPath, args...)
		cmd.Env = env
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	return syscall.Exec(pluginPath, argv, env)
}

// pluginEnv returns the current environment with Grove-specific variables added.
func pluginEnv() []string {
	env := os.Environ()

	set := func(key, val string) {
		prefix := key + "="
		for i, e := range env {
			if strings.HasPrefix(e, prefix) {
				env[i] = prefix + val
				return
			}
		}
		env = append(env, prefix+val)
	}

	set("GROVE_DIR", config.GroveDir)
	set("GROVE_CONFIG", config.ConfigPath)
	set("GROVE_STATE", filepath.Join(config.GroveDir, "state.json"))

	// Try to detect current workspace from cwd
	if ws := detectWorkspace(); ws != "" {
		set("GROVE_WORKSPACE", ws)
	}

	return env
}

// detectWorkspace returns the current workspace name if cwd is inside a workspace.
func detectWorkspace() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}

	ws, err := state.FindWorkspaceByPath(cwd)
	if err != nil || ws == nil {
		return ""
	}
	return ws.Name
}

// List returns all installed plugins in the plugins directory.
func List() ([]InstalledPlugin, error) {
	dir := Dir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var plugins []InstalledPlugin
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "gw-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Must be executable
		if info.Mode()&0o111 == 0 {
			continue
		}
		plugins = append(plugins, InstalledPlugin{
			Name: strings.TrimPrefix(name, "gw-"),
			Path: filepath.Join(dir, name),
		})
	}
	return plugins, nil
}

// Remove deletes a plugin and its metadata from the plugins directory.
func Remove(name string) error {
	bin := "gw-" + name
	pluginPath := filepath.Join(Dir(), bin)

	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		return fmt.Errorf("plugin %q is not installed", name)
	}

	// Clean up metadata file if present
	os.Remove(metaPath(name))

	return os.Remove(pluginPath)
}

// InstalledPlugin describes a plugin found in the plugins directory.
type InstalledPlugin struct {
	Name string
	Path string
}
