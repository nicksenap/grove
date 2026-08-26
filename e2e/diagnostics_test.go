package e2e

import (
	"testing"
)

func TestStats(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")
	res := env.mustGW("stats")
	env.requireContains(res.combined(), "created", "stats")
}

func TestShellInit(t *testing.T) {
	env := newEnv(t)
	out := env.mustGW("shell-init")
	env.requireContains(out.stdout, "gw()", "shell-init")
}

func TestBugReport(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	out := env.mustGW("bug-report", "--print")
	env.requireContains(out.stdout, "## Environment", "bug-report environment")
	env.requireContains(out.stdout, "## Doctor", "bug-report doctor")
	env.requireContains(out.stdout, "## Recent Logs", "bug-report logs")
}
