package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pruneCandidateJSON struct {
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	CreatedAt string `json:"created_at"`
	AgeDays   int    `json:"age_days"`
}

func TestPruneListsOldWorkspacesWithoutDeleting(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "old-ws", "--branch", "feat/old", "--repos", "svc-auth")
	env.mustGW("create", "fresh-ws", "--branch", "feat/fresh", "--repos", "svc-auth")
	env.backdateWorkspace("old-ws", 10*24*time.Hour)

	res := env.mustGW("prune", "--json")
	got := decodeJSON[[]pruneCandidateJSON](t, res.stdout)
	if len(got) != 1 || got[0].Name != "old-ws" {
		t.Fatalf("prune --json = %+v, want only old-ws", got)
	}
	if workspaceNamed(env.listWorkspaces(), "old-ws") == nil {
		t.Fatal("dry-run prune deleted old-ws")
	}
	env.requireExists(env.workspacePath("old-ws"))
}

func TestPruneYesDeletesOldWorkspaces(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "old-ws", "--branch", "feat/old", "--repos", "svc-auth")
	env.mustGW("create", "fresh-ws", "--branch", "feat/fresh", "--repos", "svc-auth")
	env.backdateWorkspace("old-ws", 10*24*time.Hour)

	env.mustGW("prune", "--yes")

	if workspaceNamed(env.listWorkspaces(), "old-ws") != nil {
		t.Fatal("old-ws still listed after prune --yes")
	}
	if workspaceNamed(env.listWorkspaces(), "fresh-ws") == nil {
		t.Fatal("fresh-ws should be kept")
	}
	env.requireMissing(env.workspacePath("old-ws"))
	env.requireExists(env.workspacePath("fresh-ws"))
	if env.branchExists(filepath.Join(env.reposDir, "svc-auth"), "feat/old") {
		t.Fatal("branch feat/old still present in source repo")
	}
}

func TestPruneDefaultMinAgeKeepsRecentWorkspaces(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "fresh-ws", "--branch", "feat/fresh", "--repos", "svc-auth")

	res := env.mustGW("prune", "--json")
	got := decodeJSON[[]pruneCandidateJSON](t, res.stdout)
	if len(got) != 0 {
		t.Fatalf("default prune listed recent workspaces: %+v", got)
	}
}

func (e *env) backdateWorkspace(name string, age time.Duration) {
	e.t.Helper()
	path := filepath.Join(e.groveDir, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatalf("read state: %v", err)
	}
	var workspaces []workspaceJSON
	if err := json.Unmarshal(data, &workspaces); err != nil {
		e.t.Fatalf("decode state: %v", err)
	}
	stamp := time.Now().Add(-age).Format("2006-01-02T15:04:05.000000")
	found := false
	for i := range workspaces {
		if workspaces[i].Name == name {
			workspaces[i].CreatedAt = stamp
			found = true
		}
	}
	if !found {
		e.t.Fatalf("workspace %s not in state", name)
	}
	out, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		e.t.Fatalf("encode state: %v", err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		e.t.Fatalf("write state: %v", err)
	}
	if !strings.Contains(string(out), stamp) {
		e.t.Fatalf("backdate did not persist created_at for %s", name)
	}
}
