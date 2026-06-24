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

// Disabled, when true, skips all hooks. Set by the --no-hooks flag.
var Disabled bool

// Vars is a set of placeholder values expanded in hook commands.
// Use {name}, {path}, {branch}, {source_url}, {source_ref}, {source_title} in
// hook commands. The source_* values are empty unless the workspace was seeded
// from a source URL (e.g. a GitHub PR, Notion page, or Slack thread).
type Vars struct {
	Name        string
	Path        string
	Branch      string
	SourceURL   string
	SourceRef   string
	SourceTitle string
}

// Run fires a named hook if configured. Returns ErrNoHook if not set.
func Run(hookName string, vars Vars) error {
	if Disabled {
		return ErrNoHook
	}

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
		"{source_url}", shellQuote(vars.SourceURL),
		"{source_ref}", shellQuote(vars.SourceRef),
		"{source_title}", shellQuote(vars.SourceTitle),
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
