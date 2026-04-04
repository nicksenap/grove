// Package lifecycle runs global lifecycle hooks defined in ~/.grove/config.toml [hooks].
package lifecycle

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/logging"
)

// Vars is a set of placeholder values expanded in hook commands.
// Use {name}, {path}, {branch} in hook commands.
type Vars struct {
	Name   string
	Path   string
	Branch string
}

// Run fires a named hook if configured. Returns error if not set or execution fails.
func Run(hookName string, vars Vars) error {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return nil
	}

	cmd, ok := cfg.Hooks[hookName]
	if !ok || cmd == "" {
		return fmt.Errorf("no hook configured for %q", hookName)
	}

	expanded := expand(cmd, vars)
	logging.Info("hook %s: %s", hookName, expanded)

	return exec.Command("sh", "-c", expanded).Run()
}

// Has returns true if a hook is configured for the given name.
func Has(hookName string) bool {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return false
	}
	cmd, ok := cfg.Hooks[hookName]
	return ok && cmd != ""
}

func expand(cmd string, vars Vars) string {
	r := strings.NewReplacer(
		"{name}", shellQuote(vars.Name),
		"{path}", shellQuote(vars.Path),
		"{branch}", shellQuote(vars.Branch),
	)
	return r.Replace(cmd)
}

// shellQuote wraps a value in single quotes, escaping any embedded single quotes.
// Empty strings are not quoted so unused placeholders expand to nothing.
func shellQuote(s string) string {
	if s == "" {
		return ""
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
