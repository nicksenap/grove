package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGit is a tiny test helper.
func cmdTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// TestCreateCommandExitsNonZeroAndRetainsClone builds the gw binary and runs a
// real `gw create` that clones a local file:// source and then fails (the target
// workspace name already exists). It proves two contract points at the command
// layer: (1) a failed create exits non-zero, and (2) the successfully cloned
// source repo is retained on disk despite the failure.
func TestCreateCommandExitsNonZeroAndRetainsClone(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary; skipped in -short")
	}
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	repoDir := filepath.Join(tmp, "repos")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A local source repo to clone via file:// .
	src := filepath.Join(tmp, "srcrepo")
	cmdTestGit(t, tmp, "init", "-q", src)
	cmdTestGit(t, src, "config", "user.email", "t@t.co")
	cmdTestGit(t, src, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdTestGit(t, src, "add", ".")
	cmdTestGit(t, src, "commit", "-qm", "init")

	// Build the gw binary.
	bin := filepath.Join(tmp, "gw")
	build := exec.Command("go", "build", "-o", bin, "./gw")
	build.Dir = mustModuleCmdDir(t)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build gw: %v\n%s", err, out)
	}

	env := append(os.Environ(), "HOME="+home)

	// Initialize grove with our repo dir.
	initCmd := exec.Command(bin, "init", repoDir)
	initCmd.Env = env
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("gw init: %v\n%s", err, out)
	}

	// Create the workspace name first so the later create collides.
	url := "file://" + src
	// Seed an existing workspace named "srcrepo" (derived name) via a normal
	// create from a plain local repo so the second create hits "already exists".
	plain := filepath.Join(repoDir, "plain")
	cmdTestGit(t, repoDir, "init", "-q", "plain")
	cmdTestGit(t, plain, "config", "user.email", "t@t.co")
	cmdTestGit(t, plain, "config", "user.name", "t")
	os.WriteFile(filepath.Join(plain, "f"), []byte("x"), 0o644)
	cmdTestGit(t, plain, "add", ".")
	cmdTestGit(t, plain, "commit", "-qm", "init")

	seed := exec.Command(bin, "create", "dup-ws", "-b", "feat/seed", "-r", "plain")
	seed.Env = env
	if out, err := seed.CombinedOutput(); err != nil {
		t.Fatalf("seed create: %v\n%s", err, out)
	}

	// Now create "dup-ws" again but with a clone URL — the clone should happen
	// (source acquisition), then the create fails because dup-ws exists.
	fail := exec.Command(bin, "create", "dup-ws", "-b", "feat/x", "-r", url)
	fail.Env = env
	out, err := fail.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for duplicate create, got success:\n%s", out)
	}
	if _, ok := err.(*exec.ExitError); !ok {
		t.Fatalf("expected exit error, got %T: %v", err, err)
	}
	// The cloned source must be retained despite the failure.
	if _, statErr := os.Stat(filepath.Join(repoDir, "srcrepo")); statErr != nil {
		t.Fatalf("cloned source repo must be retained after failed create: %v", statErr)
	}
}

// mustModuleCmdDir returns the cmd/ directory (this test file's directory).
func mustModuleCmdDir(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd // tests run in the package dir (cmd/)
}
