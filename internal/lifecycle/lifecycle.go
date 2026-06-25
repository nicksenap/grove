// Package lifecycle runs global lifecycle hooks defined in ~/.grove/config.toml [hooks].
package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/streamio"
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

// HookError wraps a hook execution failure. Abort is true when the hook's
// on_failure is set to "abort", signalling the caller that the failure should
// be fatal to the command rather than a warning.
type HookError struct {
	Hook string
	Err  error
	// Abort reflects the hook's on_failure = "abort" setting.
	Abort bool
}

func (e *HookError) Error() string {
	return fmt.Sprintf("%s hook failed: %s", e.Hook, e.Err)
}

func (e *HookError) Unwrap() error { return e.Err }

// ShouldAbort reports whether err is a hook failure whose on_failure is "abort".
func ShouldAbort(err error) bool {
	var he *HookError
	return errors.As(err, &he) && he.Abort
}

// Run fires a named hook if configured. Returns ErrNoHook if not set.
//
// Output handling depends on the hook's `stream` setting:
//   - stream = true:  the hook's stdout/stderr stream live to the terminal,
//     line-prefixed with the hook name (e.g. "[post_create] ..."), so a
//     long-running hook shows progress instead of looking hung.
//   - stream = false (default): output is captured quietly and only printed
//     (prefixed) if the hook fails, so a clean run stays silent while a
//     failure shows the actual command output rather than just "exit status 1".
//
// A non-zero `timeout` aborts the hook after the given duration. On failure the
// returned error is a *HookError; use ShouldAbort to honour on_failure = "abort".
func Run(hookName string, vars Vars) error {
	if Disabled {
		return ErrNoHook
	}

	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return ErrNoHook
	}

	hook, ok := cfg.Hooks[hookName]
	if !ok || hook.Command == "" {
		return ErrNoHook
	}

	expanded := expand(hook.Command, vars)
	logging.Info("hook %s: %s", hookName, expanded)

	ctx := context.Background()
	if hook.Timeout != "" {
		d, perr := time.ParseDuration(hook.Timeout)
		if perr != nil {
			logging.Warn("hook %s: invalid timeout %q, ignoring: %s", hookName, hook.Timeout, perr)
		} else if d > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", expanded)

	prefix := fmt.Sprintf("[%s] ", hookName)
	var captured *bytes.Buffer
	if hook.Stream {
		// Stream live to stderr, prefixed, so progress is visible. stderr keeps
		// gw's stdout clean for shell integration (e.g. `cd "$(gw go ...)"`).
		w := &streamio.PrefixWriter{Prefix: prefix, W: os.Stderr}
		cmd.Stdout = w
		cmd.Stderr = w
		runErr := cmd.Run()
		w.Flush()
		return wrap(hookName, hook, runErr, ctx)
	}

	// Quiet: capture combined output, echo it only on failure.
	captured = &bytes.Buffer{}
	cmd.Stdout = captured
	cmd.Stderr = captured
	runErr := cmd.Run()
	if runErr != nil && captured.Len() > 0 {
		echoCaptured(prefix, captured.Bytes())
	}
	return wrap(hookName, hook, runErr, ctx)
}

// wrap converts a raw run error into a *HookError (or nil), annotating it with
// the hook's on_failure policy and any timeout context.
func wrap(hookName string, hook models.Hook, runErr error, ctx context.Context) error {
	if runErr == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		runErr = fmt.Errorf("timed out after %s", hook.Timeout)
	}
	return &HookError{
		Hook:  hookName,
		Err:   runErr,
		Abort: strings.EqualFold(hook.OnFailure, "abort"),
	}
}

// echoCaptured prints captured hook output to stderr, prefixing each line so it
// reads the same as streamed output.
func echoCaptured(prefix string, out []byte) {
	w := &streamio.PrefixWriter{Prefix: prefix, W: os.Stderr}
	w.Write(out)
	w.Flush()
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
