package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	if err := os.RemoveAll(env.worktree("quit-ws", "svc-a-gone")); err != nil {
		t.Fatal(err)
	}
	env.runGWWithEnv(wsPath, pagerEnv, "diff").mustFail(t)
}
