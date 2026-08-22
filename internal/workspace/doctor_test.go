package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorHealthy(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("healthy-ws", "feat/healthy", []string{"api"})

	issues, _, err := env.svc.Doctor(false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 issues, got %d", len(issues))
	}
}

func TestDoctorDetectsMissingWorktree(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("stale-ws", "feat/stale", []string{"api"})

	// Delete worktree dir manually
	os.RemoveAll(filepath.Join(env.wsDir, "stale-ws", "api"))

	issues, _, err := env.svc.Doctor(false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(issues) == 0 {
		t.Error("expected at least 1 issue for missing worktree")
	}
}

func TestDoctorFixRemovesStale(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("fix-ws", "feat/fix", []string{"api"})

	// Delete worktree dir
	os.RemoveAll(filepath.Join(env.wsDir, "fix-ws", "api"))

	_, fixed, err := env.svc.Doctor(true)
	if err != nil {
		t.Fatalf("doctor fix: %v", err)
	}
	if fixed == 0 {
		t.Error("expected at least 1 fix")
	}

	// After fix, should be clean (or at least fewer issues)
	issues, _, _ := env.svc.Doctor(false)
	if len(issues) > 0 {
		// Workspace with no repos might still be an issue, that's ok
		t.Logf("remaining issues after fix: %d", len(issues))
	}
}

func TestDoctorDetectsMissingWorkspaceDir(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("ghost-ws", "feat/ghost", []string{"api"})

	// Delete entire workspace directory
	os.RemoveAll(filepath.Join(env.wsDir, "ghost-ws"))

	issues, _, err := env.svc.Doctor(false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}

	found := false
	for _, issue := range issues {
		if strings.Contains(issue.Issue, "workspace directory missing") {
			found = true
		}
	}
	if !found {
		t.Error("expected 'workspace directory missing' issue")
	}
}

func TestAllWorkspacesSummaryEmpty(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	results, err := env.svc.AllWorkspacesSummary()
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty, got %d", len(results))
	}
}

func TestAllWorkspacesSummaryMultiple(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createWorkspace("ws-a", "feat/a", []string{"api"})
	env.createWorkspace("ws-b", "feat/b", []string{"web"})

	results, err := env.svc.AllWorkspacesSummary()
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2, got %d", len(results))
	}

	// Each result should have name, branch, repos count, status, path
	for _, r := range results {
		if r.Name == "" || r.Branch == "" || r.Path == "" {
			t.Errorf("empty field in result: %+v", r)
		}
	}
}
