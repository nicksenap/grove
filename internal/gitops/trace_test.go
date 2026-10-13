package gitops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/logging"
)

// setupLog points logging at a temp dir and returns a reader for grove.log.
// Cleanup resets verbose while still pointed at the temp dir, so nothing is
// ever opened under the real ~/.grove.
func setupLog(t *testing.T, verbose bool) func() string {
	t.Helper()
	orig := logging.LogDir
	dir := t.TempDir()
	logging.LogDir = dir
	logging.Setup(verbose)
	t.Cleanup(func() {
		logging.Setup(false)
		logging.LogDir = orig
	})
	return func() string {
		data, _ := os.ReadFile(filepath.Join(dir, "grove.log"))
		return string(data)
	}
}

func TestRunGitTracesWhenVerbose(t *testing.T) {
	repo := initTestRepo(t)
	readLog := setupLog(t, true)

	if _, err := runGit(repo, "rev-parse", "HEAD"); err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	_, _ = runGit(repo, "rev-parse", "no-such-ref")

	log := readLog()
	for _, want := range []string{
		"DEBUG",
		"git rev-parse HEAD",
		"dir=" + repo,
		"ok",
		"git rev-parse no-such-ref",
		"error: ",
		"fatal:", // git's own stderr, not just the exec exit status
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q, got:\n%s", want, log)
		}
	}
	if strings.Contains(log, "exit status") {
		t.Errorf("log should carry git stderr, not the raw exec error, got:\n%s", log)
	}
}

func TestRunGitNoTraceWhenNotVerbose(t *testing.T) {
	repo := initTestRepo(t)
	readLog := setupLog(t, false)

	if _, err := runGit(repo, "rev-parse", "HEAD"); err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if log := readLog(); strings.Contains(log, "DEBUG") {
		t.Errorf("expected no debug trace without verbose, got:\n%s", log)
	}
}

func TestLogGitCallRedactsStderr(t *testing.T) {
	readLog := setupLog(t, true)

	// Git echoes the remote URL in its error output; credentials in it must
	// not reach grove.log. Feed the output directly so the test needs no
	// network and no particular git wording.
	stderr := "fatal: unable to access 'https://user:s3cret@example.com/o/r.git/': Could not resolve host"
	logGitCall(t.TempDir(), []string{"clone", "https://user:s3cret@example.com/o/r.git", "dest"},
		time.Millisecond, stderr, errors.New("exit status 128"))

	log := readLog()
	if strings.Contains(log, "s3cret") {
		t.Errorf("credential leaked into log:\n%s", log)
	}
	if strings.Count(log, "https://***@example.com/o/r.git") != 2 {
		t.Errorf("expected redacted URL in both args and stderr, got:\n%s", log)
	}
}

func TestLogGitCallTruncatesLongOutput(t *testing.T) {
	readLog := setupLog(t, true)

	long := strings.Repeat("x", maxTraceOutput*4)
	logGitCall(t.TempDir(), []string{"fetch"}, time.Millisecond, long, errors.New("exit status 1"))

	log := readLog()
	if !strings.Contains(log, "...[truncated]") {
		t.Errorf("expected truncation marker, got %d bytes", len(log))
	}
	if len(log) > maxTraceOutput+512 {
		t.Errorf("trace line not truncated: %d bytes", len(log))
	}
}

func TestRedactCredentials(t *testing.T) {
	cases := map[string]string{
		"clone https://user:tok3n@github.com/o/r.git /tmp/x": "clone https://***@github.com/o/r.git /tmp/x",
		"clone https://ghp_abc@github.com/o/r":               "clone https://***@github.com/o/r",
		"clone git@github.com:o/r.git":                       "clone git@github.com:o/r.git",
		"fetch origin":                                       "fetch origin",
	}
	for in, want := range cases {
		if got := redactCredentials(in); got != want {
			t.Errorf("redactCredentials(%q) = %q, want %q", in, got, want)
		}
	}
}
