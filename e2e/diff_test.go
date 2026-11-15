package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiff(t *testing.T) {
	env := newEnv(t)
	for _, name := range []string{"svc-api", "svc-auth", "svc-clean"} {
		repo := env.createRepo(name)
		env.writeFile(filepath.Join(repo, "main.txt"), "one\n")
		env.git(repo, "add", ".")
		env.git(repo, "commit", "-q", "-m", "add main.txt")
	}
	env.init()
	env.mustGW("create", "diff-ws", "--branch", "feat/diff", "--repos", "svc-auth,svc-api,svc-clean")
	wsPath := env.workspacePath("diff-ws")

	// Clean workspace: no output, and --exit-code succeeds.
	if out := env.mustGWIn(wsPath, "diff").stdout; out != "" {
		t.Fatalf("clean diff should print nothing, got:\n%s", out)
	}
	env.mustGWIn(wsPath, "diff", "--exit-code")

	env.writeFile(filepath.Join(env.worktree("diff-ws", "svc-api"), "main.txt"), "one\ntwo\n")
	env.writeFile(filepath.Join(env.worktree("diff-ws", "svc-auth"), "main.txt"), "uno\n")

	// Dirty: prefixed headers, repos in name order, no output for clean repos.
	out := env.mustGWIn(wsPath, "diff").stdout
	for _, want := range []string{
		"diff --git a/svc-api/main.txt b/svc-api/main.txt",
		"--- a/svc-auth/main.txt",
		"+++ b/svc-auth/main.txt",
	} {
		env.requireContains(out, want, "diff headers")
	}
	if strings.Index(out, "svc-api/") > strings.Index(out, "svc-auth/") {
		t.Fatalf("repos out of order:\n%s", out)
	}
	if strings.Contains(out, "svc-clean") {
		t.Fatalf("clean repo should print nothing:\n%s", out)
	}
	if res := env.gwIn(wsPath, "diff", "--quiet"); res.code != 1 {
		t.Fatalf("--quiet with changes: want exit 1, got %d\n%s", res.code, res.combined())
	}
	// Explicit workspace name and pass-through flags (-U0 is a git flag).
	env.requireContains(env.mustGW("diff", "--workspace", "diff-ws", "-U0").stdout,
		"@@ -1 +1 @@", "diff -U0 by name")

	// The combined diff applies as one patch to a monorepo-shaped tree.
	mono := filepath.Join(env.home, "mono")
	for _, name := range []string{"svc-api", "svc-auth"} {
		env.writeFile(filepath.Join(mono, name, "main.txt"), "one\n")
	}
	env.git(env.home, "init", "-q", mono)
	patch := filepath.Join(env.home, "combined.patch")
	env.writeFile(patch, out)
	env.git(mono, "apply", "--check", patch)

	// Failing: a bad revision fails every repo, gw exits non-zero.
	env.gwIn(wsPath, "diff", "no-such-ref").mustFail(t)

	// Pager: forced on, the combined stream goes through GIT_PAGER exactly once.
	capture := filepath.Join(env.home, "pager-capture")
	script := filepath.Join(env.home, "pager.sh")
	env.writeFile(script, "#!/bin/sh\necho run >> "+capture+".runs\ncat > "+capture+"\n")
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	pagerEnv := []string{"GIT_PAGER=" + script, "GROVE_FORCE_PAGER=1"}
	res := env.runGWWithEnv(wsPath, pagerEnv, "diff")
	res.must(t)
	if res.stdout != "" {
		t.Fatalf("paged output leaked to stdout:\n%s", res.stdout)
	}
	runs, _ := os.ReadFile(capture + ".runs")
	if got := strings.Count(string(runs), "run"); got != 1 {
		t.Fatalf("pager ran %d times, want 1", got)
	}
	paged, _ := os.ReadFile(capture)
	env.requireContains(string(paged), "a/svc-api/main.txt", "paged output")
	env.requireContains(string(paged), "a/svc-auth/main.txt", "paged output")
	env.requireContains(string(paged), "\x1b[", "paged output is colored")

	// --no-pager wins over a forced pager.
	res = env.runGWWithEnv(wsPath, pagerEnv, "diff", "--no-pager")
	res.must(t)
	env.requireContains(res.stdout, "a/svc-api/main.txt", "--no-pager output")
	runs, _ = os.ReadFile(capture + ".runs")
	if got := strings.Count(string(runs), "run"); got != 1 {
		t.Fatalf("--no-pager still ran the pager (%d runs)", got)
	}
}

// Quitting the pager early (here: head -1) is silent and not a failure, like
// git; a real git error in an earlier repo still sets the exit code.
func TestDiffPagerQuitEarly(t *testing.T) {
	env := newEnv(t)
	for _, name := range []string{"svc-a-gone", "svc-big", "svc-small"} {
		repo := env.createRepo(name)
		env.writeFile(filepath.Join(repo, "main.txt"), "one\n")
		env.git(repo, "add", ".")
		env.git(repo, "commit", "-q", "-m", "add main.txt")
	}
	env.init()
	env.mustGW("create", "quit-ws", "--branch", "feat/quit", "--repos", "svc-a-gone,svc-big,svc-small")
	wsPath := env.workspacePath("quit-ws")
	// Bigger than a pipe buffer, so git is still writing when the pager exits.
	env.writeFile(filepath.Join(env.worktree("quit-ws", "svc-big"), "main.txt"),
		strings.Repeat("a long line of changed content\n", 20000))
	env.writeFile(filepath.Join(env.worktree("quit-ws", "svc-small"), "main.txt"), "two\n")
	pagerEnv := []string{"GIT_PAGER=head -1", "GROVE_FORCE_PAGER=1"}

	res := env.runGWWithEnv(wsPath, pagerEnv, "diff")
	res.must(t)
	if res.stderr != "" {
		t.Fatalf("quitting the pager should be silent, got stderr:\n%s", res.stderr)
	}
	if strings.Count(res.stdout, "\n") != 1 {
		t.Fatalf("want exactly the pager's one line, got:\n%s", res.stdout)
	}

	// A repo that fails before the pager quits still sets the exit code.
	if err := os.RemoveAll(env.worktree("quit-ws", "svc-a-gone")); err != nil {
		t.Fatal(err)
	}
	res = env.runGWWithEnv(wsPath, pagerEnv, "diff")
	if res.code != 128 {
		t.Fatalf("want exit 128 from the missing repo, got %d\n%s", res.code, res.combined())
	}
	if strings.Count(res.stdout, "\n") != 1 {
		t.Fatalf("want the pager's one line, got:\n%s", res.stdout)
	}
}

// writeCapturePager writes a GIT_PAGER script that records each run in
// <capture>.runs and the paged input in <capture>, then runs extra.
func (e *env) writeCapturePager(extra string) (script, capture string) {
	e.t.Helper()
	capture = filepath.Join(e.home, "pager-capture")
	script = filepath.Join(e.home, "pager.sh")
	e.writeFile(script, "#!/bin/sh\necho run >> "+capture+".runs\ncat > "+capture+"\n"+extra)
	if err := os.Chmod(script, 0o755); err != nil {
		e.t.Fatal(err)
	}
	return script, capture
}

func pagerRuns(t *testing.T, capture string) int {
	t.Helper()
	runs, _ := os.ReadFile(capture + ".runs")
	return strings.Count(string(runs), "run")
}

// newDiffWorkspace creates workspace diff-ws over repos, each with a changed main.txt.
func (e *env) newDiffWorkspace(repos ...string) string {
	e.t.Helper()
	for _, name := range repos {
		repo := e.createRepo(name)
		e.writeFile(filepath.Join(repo, "main.txt"), "one\n")
		e.git(repo, "add", ".")
		e.git(repo, "commit", "-q", "-m", "add main.txt")
	}
	e.init()
	e.mustGW("create", "diff-ws", "--branch", "feat/diff", "--repos", strings.Join(repos, ","))
	for _, name := range repos {
		e.writeFile(filepath.Join(e.worktree("diff-ws", name), "main.txt"), "two\n")
	}
	return e.workspacePath("diff-ws")
}

// The pager is resolved from the first repo that exists, so a missing first
// repo still pages, and its failure still sets the exit code.
func TestDiffPagerWithMissingFirstRepo(t *testing.T) {
	env := newEnv(t)
	wsPath := env.newDiffWorkspace("svc-a-gone", "svc-b")
	if err := os.RemoveAll(env.worktree("diff-ws", "svc-a-gone")); err != nil {
		t.Fatal(err)
	}
	script, capture := env.writeCapturePager("")
	res := env.runGWWithEnv(wsPath, []string{"GIT_PAGER=" + script, "GROVE_FORCE_PAGER=1"}, "diff")
	if res.code != 128 {
		t.Fatalf("want exit 128, got %d\n%s", res.code, res.combined())
	}
	if got := pagerRuns(t, capture); got != 1 {
		t.Fatalf("pager ran %d times, want 1", got)
	}
	paged, _ := os.ReadFile(capture)
	env.requireContains(string(paged), "a/svc-b/main.txt", "paged output")
}

// Like git, --exit-code and --quiet never start a pager.
func TestDiffExitCodeSkipsPager(t *testing.T) {
	env := newEnv(t)
	wsPath := env.newDiffWorkspace("svc-a")
	script, capture := env.writeCapturePager("")
	pagerEnv := []string{"GIT_PAGER=" + script, "GROVE_FORCE_PAGER=1"}
	for _, flag := range []string{"--exit-code", "--quiet"} {
		if res := env.runGWWithEnv(wsPath, pagerEnv, "diff", flag); res.code != 1 {
			t.Fatalf("%s: want exit 1, got %d\n%s", flag, res.code, res.combined())
		}
	}
	if got := pagerRuns(t, capture); got != 0 {
		t.Fatalf("pager ran %d times, want 0", got)
	}
}

// Global flags before `diff` are gw's, not git's.
func TestDiffGlobalFlags(t *testing.T) {
	env := newEnv(t)
	wsPath := env.newDiffWorkspace("svc-a")
	env.requireContains(env.mustGWIn(wsPath, "--verbose", "diff").stdout, "a/svc-a/main.txt", "--verbose diff")
	env.requireContains(env.mustGWIn(wsPath, "-n", "diff", "--stat").stdout, "1 file changed", "-n diff --stat")
}

// Ctrl-C while paging must not kill gw: it keeps waiting for the pager.
func TestDiffSIGINTWaitsForPager(t *testing.T) {
	env := newEnv(t)
	wsPath := env.newDiffWorkspace("svc-a")
	done := filepath.Join(env.home, "pager-done")
	script, capture := env.writeCapturePager("sleep 1\ntouch " + done + "\n")

	cmd := exec.Command(gwBin, "diff")
	cmd.Dir = wsPath
	cmd.Env = append(env.cmdEnv(), "GIT_PAGER="+script, "GROVE_FORCE_PAGER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Wait until the pager has consumed the diff, so gw is waiting on it.
	deadline := time.Now().Add(10 * time.Second)
	for pagerRuns(t, capture) == 0 || !fileHas(capture, "svc-a") {
		if time.Now().After(deadline) {
			t.Fatal("pager never received the diff")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("gw should exit cleanly after the pager, got %v", err)
	}
	env.requireExists(done)
}

func fileHas(path, s string) bool {
	b, _ := os.ReadFile(path)
	return strings.Contains(string(b), s)
}
