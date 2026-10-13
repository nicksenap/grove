package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/logging"
)

// setupLog points logging at a temp dir and returns a reader for grove.log.
func setupLog(t *testing.T, verbose bool) func() string {
	t.Helper()
	orig := logging.LogDir
	logging.LogDir = t.TempDir()
	logging.Setup(verbose)
	t.Cleanup(func() {
		logging.LogDir = orig
		logging.Setup(false)
	})
	return func() string {
		data, _ := os.ReadFile(filepath.Join(logging.LogDir, "grove.log"))
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
		"DEBUG - git rev-parse HEAD (dir=" + repo + ")",
		" ok\n",
		"DEBUG - git rev-parse no-such-ref",
		" error: ",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q, got:\n%s", want, log)
		}
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
