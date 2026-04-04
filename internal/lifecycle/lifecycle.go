// Package lifecycle runs global lifecycle hooks defined in ~/.grove/config.toml [hooks].
package lifecycle

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/logging"
)

// ErrNoHook is returned when the requested hook is not configured.
var ErrNoHook = errors.New("hook not configured")

// Vars is a set of placeholder values expanded in hook commands.
// Use {name}, {path}, {branch} in hook commands.
type Vars struct {
	Name   string
	Path   string
	Branch string
}

// Run fires a named hook if configured. Returns ErrNoHook if not set.
func Run(hookName string, vars Vars) error {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return ErrNoHook
	}

	cmd, ok := cfg.Hooks[hookName]
	if !ok || cmd == "" {
		return ErrNoHook
	}

	expanded := expand(cmd, vars)
	logging.Info("hook %s: %s", hookName, expanded)

	return exec.Command("sh", "-c", expanded).Run()
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
