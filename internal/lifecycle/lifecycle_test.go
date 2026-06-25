package lifecycle

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/config"
)

// useTempConfig points config.ConfigPath at a temp file containing the given
// TOML body and restores it on cleanup.
func useTempConfig(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	origPath := config.ConfigPath
	config.ConfigPath = filepath.Join(dir, "config.toml")
	t.Cleanup(func() { config.ConfigPath = origPath })
	if err := os.WriteFile(config.ConfigPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns whatever
// was written to it.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		io.Copy(&b, r)
		done <- b.String()
	}()

	fn()

	w.Close()
	os.Stderr = orig
	return <-done
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"simple", "my-workspace", "'my-workspace'"},
		{"spaces", "my workspace", "'my workspace'"},
		{"single quotes", "it's", "'it'\\''s'"},
		{"semicolon injection", "x; rm -rf ~", "'x; rm -rf ~'"},
		{"subshell injection", "$(whoami)", "'$(whoami)'"},
		{"backtick injection", "`whoami`", "'`whoami`'"},
		{"pipe injection", "x | cat /etc/passwd", "'x | cat /etc/passwd'"},
		{"ampersand injection", "x && echo pwned", "'x && echo pwned'"},
		{"newline injection", "x\nrm -rf /", "'x\nrm -rf /'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.in)
			if got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		vars Vars
		want string
	}{
		{
			name: "no placeholders",
			cmd:  "zellij action close-pane",
			vars: Vars{},
			want: "zellij action close-pane",
		},
		{
			name: "all placeholders",
			cmd:  "echo {name} {path} {branch}",
			vars: Vars{Name: "ws-1", Path: "/tmp/ws", Branch: "feat/login"},
			want: "echo 'ws-1' '/tmp/ws' 'feat/login'",
		},
		{
			name: "injection in branch name",
			cmd:  "echo {branch}",
			vars: Vars{Branch: "feat/x; rm -rf ~"},
			want: "echo 'feat/x; rm -rf ~'",
		},
		{
			name: "injection via subshell",
			cmd:  "echo {name}",
			vars: Vars{Name: "$(rm -rf /)"},
			want: "echo '$(rm -rf /)'",
		},
		{
			name: "empty vars expand to nothing",
			cmd:  "do-thing {name}",
			vars: Vars{},
			want: "do-thing ",
		},
		{
			name: "source placeholders",
			cmd:  "seed {source_url} {source_ref} {source_title}",
			vars: Vars{SourceURL: "https://github.com/o/r/pull/7", SourceRef: "7", SourceTitle: "Fix bug"},
			want: "seed 'https://github.com/o/r/pull/7' '7' 'Fix bug'",
		},
		{
			name: "empty source placeholders expand to nothing",
			cmd:  "seed {source_url}{source_ref}{source_title}",
			vars: Vars{Name: "ws"},
			want: "seed ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expand(tt.cmd, tt.vars)
			if got != tt.want {
				t.Errorf("expand(%q, %+v) = %q, want %q", tt.cmd, tt.vars, got, tt.want)
			}
		})
	}
}

func TestRunDisabled(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "fired")

	// Point config at a temp config.toml with a post_create hook that writes a marker.
	origPath := config.ConfigPath
	config.ConfigPath = filepath.Join(dir, "config.toml")
	t.Cleanup(func() { config.ConfigPath = origPath })

	cfg := "[hooks]\npost_create = \"touch " + marker + "\"\n"
	if err := os.WriteFile(config.ConfigPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// With Disabled set, Run must skip the hook entirely and report ErrNoHook.
	Disabled = true
	t.Cleanup(func() { Disabled = false })

	if err := Run("post_create", Vars{}); !errors.Is(err, ErrNoHook) {
		t.Fatalf("Run(disabled) = %v, want ErrNoHook", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("hook fired despite Disabled (marker exists: %v)", err)
	}

	// Sanity: with Disabled cleared, the same hook fires.
	Disabled = false
	if err := Run("post_create", Vars{}); err != nil {
		t.Fatalf("Run(enabled) = %v, want nil", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("hook did not fire when enabled: %v", err)
	}
}

// TestRunStreamsWhenConfigured verifies stream = true sends hook output live to
// stderr, prefixed with the hook name.
func TestRunStreamsWhenConfigured(t *testing.T) {
	useTempConfig(t, "[hooks.post_create]\ncommand = \"echo hello-from-hook\"\nstream = true\n")

	var runErr error
	out := captureStderr(t, func() { runErr = Run("post_create", Vars{}) })

	if runErr != nil {
		t.Fatalf("Run = %v, want nil", runErr)
	}
	if !strings.Contains(out, "[post_create] hello-from-hook") {
		t.Errorf("streamed output missing prefixed line, got: %q", out)
	}
}

// TestRunSilentOnSuccess verifies a non-streaming hook produces no output when
// it succeeds.
func TestRunSilentOnSuccess(t *testing.T) {
	useTempConfig(t, "[hooks]\npost_create = \"echo should-not-appear\"\n")

	var runErr error
	out := captureStderr(t, func() { runErr = Run("post_create", Vars{}) })

	if runErr != nil {
		t.Fatalf("Run = %v, want nil", runErr)
	}
	if strings.Contains(out, "should-not-appear") {
		t.Errorf("non-streaming successful hook should be silent, got: %q", out)
	}
}

// TestRunCapturesAndEchoesOnFailure verifies a non-streaming hook that fails has
// its captured output echoed to stderr (so failures are not opaque).
func TestRunCapturesAndEchoesOnFailure(t *testing.T) {
	useTempConfig(t, "[hooks]\npost_create = \"echo boom-details; exit 1\"\n")

	var runErr error
	out := captureStderr(t, func() { runErr = Run("post_create", Vars{}) })

	if runErr == nil {
		t.Fatal("Run = nil, want failure")
	}
	var he *HookError
	if !errors.As(runErr, &he) {
		t.Fatalf("error type = %T, want *HookError", runErr)
	}
	if he.Abort {
		t.Error("Abort should be false without on_failure = abort")
	}
	if !strings.Contains(out, "[post_create] boom-details") {
		t.Errorf("failed hook output should be echoed, got: %q", out)
	}
}

// TestRunOnFailureAbort verifies on_failure = "abort" marks the error abortable.
func TestRunOnFailureAbort(t *testing.T) {
	useTempConfig(t, "[hooks.post_create]\ncommand = \"exit 7\"\non_failure = \"abort\"\n")

	err := Run("post_create", Vars{})
	if err == nil {
		t.Fatal("Run = nil, want failure")
	}
	if !ShouldAbort(err) {
		t.Errorf("ShouldAbort = false, want true for on_failure = abort")
	}
}

// TestRunTimeout verifies a hook exceeding its timeout is aborted and reported.
// The command forces the shell to spawn a child (background sleep + wait) so the
// shell cannot be exec-optimized into the sleep itself — this reproduces the
// real case where killing only the shell would leave a child holding the output
// pipe open and block Wait until the child exits.
func TestRunTimeout(t *testing.T) {
	useTempConfig(t, "[hooks.post_create]\ncommand = \"sleep 30 & wait\"\ntimeout = \"100ms\"\n")

	start := time.Now()
	err := Run("post_create", Vars{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run = nil, want timeout failure")
	}
	if elapsed > 3*time.Second {
		t.Errorf("hook ran for %s, timeout did not abort it", elapsed)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want it to mention the timeout", err.Error())
	}
}

// TestRunInvalidTimeoutFailsOpen verifies an unparseable timeout string is
// ignored (logged as a warning) and the hook still runs unbounded, rather than
// being rejected — so a typo in `timeout` never silently stops a hook firing.
func TestRunInvalidTimeoutFailsOpen(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "fired")
	useTempConfig(t, "[hooks.post_create]\ncommand = \"touch "+marker+"\"\ntimeout = \"banana\"\n")

	if err := Run("post_create", Vars{}); err != nil {
		t.Fatalf("Run = %v, want nil (invalid timeout should be ignored, not fatal)", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("hook did not fire despite only an invalid timeout: %v", err)
	}
}

// TestRunTimeoutKillsChildren verifies the timeout kills the hook's child
// processes, not just the shell — otherwise a hook like "npm install" would
// leave the real work running after the timeout fired.
func TestRunTimeoutKillsChildren(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	// Spawn a long sleep, record its PID, then wait on it. If the group kill
	// works, the recorded PID is dead shortly after Run returns.
	cmd := "sleep 30 & echo $! > " + pidFile + "; wait"
	useTempConfig(t, "[hooks.post_create]\ncommand = \""+cmd+"\"\ntimeout = \"200ms\"\n")

	if err := Run("post_create", Vars{}); err == nil {
		t.Fatal("Run = nil, want timeout failure")
	}

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse child pid %q: %v", data, err)
	}

	// Give the kill a moment to propagate, then confirm the child is gone.
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err == nil {
		syscall.Kill(pid, syscall.SIGKILL) // clean up the leak
		t.Errorf("child process %d still alive after timeout; group kill did not reap it", pid)
	}
}
