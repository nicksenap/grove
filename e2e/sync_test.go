package e2e

import (
	"path/filepath"
	"testing"
)

func TestSync(t *testing.T) {
	env := newEnv(t)
	env.createRepoWithHistory("grove", 3)
	env.init()
	env.mustGW("create", "sync-ws", "--branch", "feat/sync-test", "--repos", "grove")

	repo := filepath.Join(env.reposDir, "grove")
	ws := env.worktree("sync-ws", "grove")
	base := env.git(repo, "symbolic-ref", "--short", "HEAD")
	env.git(repo, "checkout", "-q", base)
	env.writeFile(filepath.Join(repo, "README.md"), env.git(repo, "show", "HEAD:README.md")+"upstream change\n")
	env.git(repo, "add", ".")
	env.git(repo, "commit", "-q", "-m", "upstream: new feature")
	env.git(repo, "update-ref", "refs/remotes/origin/"+base, "HEAD")
	env.git(repo, "remote", "set-url", "origin", "/dev/null")

	behind := env.commitsBehind(ws, "origin/"+base)
	if behind == "0" || behind == "?" {
		t.Fatalf("worktree should be behind origin/%s, got: %s", base, behind)
	}

	env.mustGW("sync", "sync-ws")
	if got := env.commitsBehind(ws, "origin/"+base); got != "0" {
		t.Fatalf("worktree still %s behind after sync", got)
	}
}

func TestReset(t *testing.T) {
	env := newEnv(t)
	env.createRepoWithHistory("grove", 3)
	env.init()
	env.mustGW("create", "reset-ws", "--branch", "feat/reset-test", "--repos", "grove")
	ws := env.worktree("reset-ws", "grove")

	env.git(ws, "switch", "-q", "-c", "feat/wander")
	if got := env.currentBranch(ws); got != "feat/wander" {
		t.Fatalf("expected feat/wander, got: %s", got)
	}

	env.mustGW("reset", "reset-ws")
	if got := env.currentBranch(ws); got != "feat/reset-test" {
		t.Fatalf("expected feat/reset-test after reset, got: %s", got)
	}

	env.git(ws, "switch", "-q", "-c", "feat/wander2")
	env.writeFile(filepath.Join(ws, "README.md"), "dirt\n")
	env.mustGW("reset", "reset-ws")
	if got := env.currentBranch(ws); got != "feat/wander2" {
		t.Fatalf("dirty wanderer should stay on feat/wander2, got: %s", got)
	}
}
