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
	Missing   bool   `json:"missing"`
	Error     string `json:"error,omitempty"`
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

func TestPruneMinAgeAcceptsUnits(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "old-ws", "--branch", "feat/old", "--repos", "svc-auth")
	env.backdateWorkspace("old-ws", 10*24*time.Hour)

	for _, tc := range []struct {
		minAge string
		want   int
	}{
		{"2w", 0},  // 14 days: too young
		{"7d", 1},  // 7 days
		{"7", 1},   // bare number means days
		{"12h", 1}, // 12 hours
	} {
		res := env.mustGW("prune", "--json", "--min-age", tc.minAge)
		got := decodeJSON[[]pruneCandidateJSON](t, res.stdout)
		if len(got) != tc.want {
			t.Errorf("--min-age %s: got %+v, want %d candidate(s)", tc.minAge, got, tc.want)
		}
	}

	res := env.gw("prune", "--min-age", "7x").mustFail(t)
	if !strings.Contains(res.combined(), "unit must be h, d, or w") {
		t.Fatalf("unexpected error for bad unit:\n%s", res.combined())
	}
}

func TestPruneCleansStaleRecordWhoseDirectoryIsGone(t *testing.T) {
	env := newEnv(t)
	repo := env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "gone-ws", "--branch", "feat/gone", "--repos", "svc-auth")
	env.mustGW("create", "fresh-ws", "--branch", "feat/fresh", "--repos", "svc-auth")
	if err := os.RemoveAll(env.workspacePath("gone-ws")); err != nil {
		t.Fatalf("simulate out-of-band removal: %v", err)
	}

	// Listed regardless of age.
	res := env.mustGW("prune", "--json")
	got := decodeJSON[[]pruneCandidateJSON](t, res.stdout)
	if len(got) != 1 || got[0].Name != "gone-ws" || !got[0].Missing {
		t.Fatalf("prune --json = %+v, want gone-ws marked missing", got)
	}

	env.mustGW("prune", "--yes")

	if workspaceNamed(env.listWorkspaces(), "gone-ws") != nil {
		t.Fatal("gone-ws still in state after prune --yes")
	}
	if workspaceNamed(env.listWorkspaces(), "fresh-ws") == nil {
		t.Fatal("fresh-ws should be kept")
	}
	if strings.Contains(env.git(repo, "worktree", "list", "--porcelain"), "gone-ws") {
		t.Fatal("stale worktree registration for gone-ws was not pruned")
	}
	if env.branchExists(repo, "feat/gone") {
		t.Fatal("branch feat/gone still present in source repo")
	}
	env.requireMissing(filepath.Join(env.wsDir, ".trash"))
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
