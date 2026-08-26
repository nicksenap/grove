package e2e

import (
	"os"
	"testing"
)

func TestDoctorHealthy(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")
	if issues := env.doctorIssues(); len(issues) != 0 {
		t.Fatalf("doctor: found %d unexpected issue(s): %+v", len(issues), issues)
	}
}

func TestDoctorFix(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.createRepo("svc-api")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth,svc-api")

	if err := os.RemoveAll(env.worktree("test-ws", "svc-api")); err != nil {
		t.Fatal(err)
	}
	before := env.doctorIssues()
	if len(before) == 0 {
		t.Fatal("doctor should detect missing worktree")
	}
	env.mustGW("doctor", "--fix")
	after := env.doctorIssues()
	if len(after) > len(before) {
		t.Fatalf("doctor --fix increased issues (%d -> %d)", len(before), len(after))
	}
}
