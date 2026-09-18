package workspace

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/output"
)

func TestFormatSourceLine(t *testing.T) {
	tests := []struct {
		name string
		src  *models.WorkspaceSource
		want string
	}{
		{"nil", nil, ""},
		{
			"full",
			&models.WorkspaceSource{Provider: "github", Ref: "1172", Title: "Surface data source status", URL: "https://github.com/o/r/pull/1172"},
			"Source: github 1172 — Surface data source status  (https://github.com/o/r/pull/1172)",
		},
		{
			"url only",
			&models.WorkspaceSource{Provider: "slack", URL: "https://x.slack.com/archives/C/p1"},
			"Source: slack  (https://x.slack.com/archives/C/p1)",
		},
		{
			"no provider",
			&models.WorkspaceSource{Title: "untitled"},
			"Source: source — untitled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatSourceLine(tt.src); got != tt.want {
				t.Errorf("formatSourceLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatusSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("status-ws", "feat/status", []string{"api"})

	err := env.svc.Status("status-ws", StatusOptions{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
}

func TestCollectRepoStatusReportsCurrentBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("status-ws", "feat/status", []string{"api"})

	ws, err := env.svc.State.GetWorkspace("status-ws")
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	repo := ws.Repos[0]
	env.run(repo.WorktreePath, "git", "switch", "-q", "-c", "feat/actual")

	result := collectRepoStatus(repo)
	if result.Branch != "feat/actual" {
		t.Errorf("branch: got %q, want %q", result.Branch, "feat/actual")
	}
}

func TestCollectRepoStatusReportsDetachedHead(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("status-ws", "feat/status", []string{"api"})

	ws, err := env.svc.State.GetWorkspace("status-ws")
	if err != nil {
		t.Fatalf("get workspace: %v", err)
	}
	repo := ws.Repos[0]
	env.run(repo.WorktreePath, "git", "switch", "-q", "--detach")

	result := collectRepoStatus(repo)
	if result.Branch != "(detached)" {
		t.Errorf("branch: got %q, want %q", result.Branch, "(detached)")
	}
}

func TestStatusJSON(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("json-ws", "feat/json", []string{"api"})

	err := env.svc.Status("json-ws", StatusOptions{Format: output.JSON})
	if err != nil {
		t.Fatalf("status json: %v", err)
	}
}

func TestStatusVerbose(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createWorkspace("verbose-ws", "feat/verbose", []string{"api"})

	// Should not error — verbose shows raw git status for dirty repos
	err := env.svc.Status("verbose-ws", StatusOptions{Verbose: true})
	if err != nil {
		t.Fatalf("status verbose: %v", err)
	}
}

func TestStatusNotFound(t *testing.T) {
	env := setupTestEnv(t)
	_ = env

	err := env.svc.Status("nonexistent", StatusOptions{})
	if err == nil {
		t.Error("expected error")
	}
}

func TestStatusMultiRepoAllReported(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.createWorkspace("multi-status", "feat/ms", []string{"api", "web"})

	// Should not error even with multiple repos
	err := env.svc.Status("multi-status", StatusOptions{})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
}

func TestWriteStatusPathOutputsOneWorktreePerLine(t *testing.T) {
	ws := &models.Workspace{
		Name: "alpha",
		Path: "/workspaces/alpha",
		Repos: []models.RepoWorktree{
			{RepoName: "api", WorktreePath: "/workspaces/alpha/api"},
			{RepoName: "web", WorktreePath: "/workspaces/alpha/web"},
		},
	}
	results := []repoStatusResult{{Repo: "api"}, {Repo: "web"}}
	var stdout bytes.Buffer

	if err := writeStatus(&stdout, ws, results, output.Path, StatusOptions{}); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}
	if got := stdout.String(); got != "/workspaces/alpha/api\n/workspaces/alpha/web\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestWriteStatusJSONLinesIncludesWorkspaceAndRepoPaths(t *testing.T) {
	ws := &models.Workspace{
		Name:  "alpha",
		Path:  "/workspaces/alpha",
		Repos: []models.RepoWorktree{{RepoName: "api", WorktreePath: "/workspaces/alpha/api"}},
	}
	results := []repoStatusResult{{Repo: "api", Branch: "feat/a", Status: "clean", Ahead: "0", Behind: "0"}}
	var stdout bytes.Buffer

	if err := writeStatus(&stdout, ws, results, output.JSONLines, StatusOptions{}); err != nil {
		t.Fatalf("writeStatus: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, `"workspace":"alpha"`) || !strings.Contains(got, `"path":"/workspaces/alpha/api"`) || strings.Count(got, "\n") != 1 {
		t.Fatalf("unexpected JSONL: %q", got)
	}
}
