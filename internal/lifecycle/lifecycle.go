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
	"syscall"
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
	run, err := prepare(hookName, vars)
	if err != nil {
		return err
	}
	defer run.close()

	if run.hook.Stream {
		// Stream live to stderr, prefixed, so progress is visible. stderr keeps
		// gw's stdout clean for shell integration (e.g. `cd "$(gw go ...)"`).
		w := streamio.New(run.prefix(), os.Stderr)
		run.cmd.Stdout = w
		run.cmd.Stderr = w
		runErr := run.cmd.Run()
		w.Flush()
		return run.wrap(runErr)
	}

	// Quiet: capture combined output, echo it only on failure.
	captured := &bytes.Buffer{}
	run.cmd.Stdout = captured
	run.cmd.Stderr = captured
	runErr := run.cmd.Run()
	if runErr != nil && captured.Len() > 0 {
		echoCaptured(run.prefix(), captured.Bytes())
	}
	return run.wrap(runErr)
}

// Resolve runs a value-returning hook and returns its stdout as the result.
// Returns ErrNoHook if the hook is not configured or hooks are disabled.
//
// Contract: stdout must be exactly one non-empty line (surrounding whitespace
// is trimmed). stderr is kept out of the result: streamed live when
// stream = true, otherwise echoed only on failure. stdin is not attached.
// Any failure — non-zero exit, timeout, or malformed output — is always an
// aborting *HookError, regardless of on_failure: a wrong value is worse than
// a failed command.
func Resolve(hookName string, vars Vars) (string, error) {
	run, err := prepare(hookName, vars)
	if err != nil {
		return "", err
	}
	defer run.close()
	run.hook.OnFailure = "abort"

	var stdout, stderr bytes.Buffer
	run.cmd.Stdout = &stdout
	if run.hook.Stream {
		w := streamio.New(run.prefix(), os.Stderr)
		defer w.Flush()
		run.cmd.Stderr = w
	} else {
		run.cmd.Stderr = &stderr
	}

	fail := func(err error) (string, error) {
		if stderr.Len() > 0 {
			echoCaptured(run.prefix(), stderr.Bytes())
		}
		return "", run.wrap(err)
	}
	if err := run.cmd.Run(); err != nil {
		return fail(err)
	}
	out := strings.TrimSpace(stdout.String())
	switch {
	case out == "":
		return fail(errors.New("produced no output"))
	case strings.ContainsAny(out, "\r\n"):
		return fail(fmt.Errorf("produced more than one line: %q", out))
	}
	return out, nil
}

// hookRun is a configured hook ready to run: its command has placeholders
// expanded and any timeout applied. Call close when done.
type hookRun struct {
	name   string
	hook   models.Hook
	cmd    *exec.Cmd
	ctx    context.Context
	cancel context.CancelFunc // nil without a timeout
}

// prepare loads the named hook and builds its command. Returns ErrNoHook if
// hooks are disabled or the hook is not configured.
func prepare(hookName string, vars Vars) (*hookRun, error) {
	if Disabled {
		return nil, ErrNoHook
	}

	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return nil, ErrNoHook
	}

	hook, ok := cfg.Hooks[hookName]
	if !ok || hook.Command == "" {
		return nil, ErrNoHook
	}

	expanded := expand(hook.Command, vars)
	logging.Info("hook %s: %s", hookName, expanded)

	run := &hookRun{name: hookName, hook: hook, ctx: context.Background()}
	if hook.Timeout != "" {
		d, perr := time.ParseDuration(hook.Timeout)
		if perr != nil {
			logging.Warn("hook %s: invalid timeout %q, ignoring: %s", hookName, hook.Timeout, perr)
		} else if d > 0 {
			run.ctx, run.cancel = context.WithTimeout(run.ctx, d)
		}
	}

	cmd := exec.CommandContext(run.ctx, "sh", "-c", expanded)
	if run.cancel != nil {
		// Run the hook in its own process group so a timeout can kill the whole
		// tree. The shell typically spawns the real work (e.g. an installer) as
		// a child; killing only the shell would leave that child running and
		// holding the output pipe open, so cmd.Wait would block until the child
		// finished anyway — defeating the timeout. Killing the group (-pgid)
		// terminates the children too.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return nil
			}
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		// Backstop: if a stray descendant still holds the output pipe after the
		// group kill, don't let Wait hang — give it a moment, then force-close.
		cmd.WaitDelay = 2 * time.Second
	}
	run.cmd = cmd
	return run, nil
}

func (r *hookRun) close() {
	if r.cancel != nil {
		r.cancel()
	}
}

// prefix labels output lines with the hook name, e.g. "[post_create] ".
func (r *hookRun) prefix() string { return fmt.Sprintf("[%s] ", r.name) }

// wrap converts a raw run error into a *HookError (or nil), annotating it with
// the hook's on_failure policy and any timeout context.
func (r *hookRun) wrap(runErr error) error {
	if runErr == nil {
		return nil
	}
	if errors.Is(r.ctx.Err(), context.DeadlineExceeded) {
		runErr = fmt.Errorf("timed out after %s", r.hook.Timeout)
	}
	return &HookError{
		Hook:  r.name,
		Err:   runErr,
		Abort: strings.EqualFold(r.hook.OnFailure, "abort"),
	}
}

// echoCaptured prints captured hook output to stderr, prefixing each line so it
// reads the same as streamed output.
func echoCaptured(prefix string, out []byte) {
	w := streamio.New(prefix, os.Stderr)
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
