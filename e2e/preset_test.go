package e2e

import (
	"testing"
)

func TestPresets(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.createRepo("svc-api")
	env.createRepo("svc-gateway")
	env.init()
	env.writeConfig(`[presets.backend]
repos = ["svc-auth", "svc-api"]
`)

	env.mustGW("create", "preset-ws", "--branch", "feat/preset", "--preset", "backend")
	env.requireExists(env.worktree("preset-ws", "svc-auth"))
	env.requireExists(env.worktree("preset-ws", "svc-api"))
	env.requireMissing(env.worktree("preset-ws", "svc-gateway"))
	env.mustGW("delete", "preset-ws")

	list := env.mustGW("preset", "list", "--json")
	presets := decodeJSON[map[string]presetJSON](t, list.stdout)
	if _, ok := presets["backend"]; !ok {
		t.Fatalf("preset list --json missing presets: %s", list.stdout)
	}
	if len(presets["backend"].Repos) == 0 {
		t.Fatalf("preset list --json missing repos: %s", list.stdout)
	}

	show := env.mustGW("preset", "show", "backend", "--json")
	decoded := decodeJSON[presetJSON](t, show.stdout)
	if decoded.Name != "backend" {
		t.Fatalf("preset show --json failed: %s", show.stdout)
	}
	if len(decoded.Repos) != 2 {
		t.Fatalf("preset show --json wrong repo count: %+v", decoded)
	}
}
