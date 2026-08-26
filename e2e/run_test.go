package e2e

import "testing"

func TestRun(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.addGroveTOML("svc-auth", "setup = \"touch .grove-setup-ran\"\nrun = \"echo hello-from-run\"\n")
	env.init()
	env.mustGW("create", "run-ws", "--branch", "feat/run-test", "--repos", "svc-auth")

	out := env.mustGW("run", "run-ws")
	env.requireContains(out.combined(), "hello-from-run", "gw run")
}
