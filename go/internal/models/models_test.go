package models

import (
	"encoding/json"
	"testing"
)

func TestNewWorkspace(t *testing.T) {
	ws := NewWorkspace("test", "/tmp/test", "feat/test")

	if ws.Name != "test" {
		t.Errorf("expected name 'test', got %q", ws.Name)
	}
	if ws.Path != "/tmp/test" {
		t.Errorf("expected path '/tmp/test', got %q", ws.Path)
	}
	if ws.Branch != "feat/test" {
		t.Errorf("expected branch 'feat/test', got %q", ws.Branch)
	}
	if ws.CreatedAt == "" {
		t.Error("expected non-empty created_at")
	}
	if ws.Repos == nil {
		t.Error("expected non-nil repos slice")
	}
	if len(ws.Repos) != 0 {
		t.Errorf("expected empty repos, got %d", len(ws.Repos))
	}
}

func TestWorkspaceFindRepo(t *testing.T) {
	ws := NewWorkspace("test", "/tmp/test", "feat/test")
	ws.Repos = []RepoWorktree{
		{RepoName: "api", SourceRepo: "/src/api", WorktreePath: "/tmp/test/api", Branch: "feat/test"},
		{RepoName: "web", SourceRepo: "/src/web", WorktreePath: "/tmp/test/web", Branch: "feat/test"},
	}

	r := ws.FindRepo("api")
	if r == nil {
		t.Fatal("expected to find repo 'api'")
	}
	if r.RepoName != "api" {
		t.Errorf("expected 'api', got %q", r.RepoName)
	}

	r = ws.FindRepo("nonexistent")
	if r != nil {
		t.Errorf("expected nil for nonexistent repo, got %v", r)
	}
}

func TestWorkspaceRemoveRepo(t *testing.T) {
	ws := NewWorkspace("test", "/tmp/test", "feat/test")
	ws.Repos = []RepoWorktree{
		{RepoName: "api", SourceRepo: "/src/api", WorktreePath: "/tmp/test/api", Branch: "feat/test"},
		{RepoName: "web", SourceRepo: "/src/web", WorktreePath: "/tmp/test/web", Branch: "feat/test"},
	}

	ok := ws.RemoveRepo("api")
	if !ok {
		t.Error("expected RemoveRepo to return true")
	}
	if len(ws.Repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(ws.Repos))
	}
	if ws.Repos[0].RepoName != "web" {
		t.Errorf("expected remaining repo 'web', got %q", ws.Repos[0].RepoName)
	}

	ok = ws.RemoveRepo("nonexistent")
	if ok {
		t.Error("expected RemoveRepo to return false for nonexistent")
	}
}

func TestWorkspaceRepoNames(t *testing.T) {
	ws := NewWorkspace("test", "/tmp/test", "feat/test")
	ws.Repos = []RepoWorktree{
		{RepoName: "api"},
		{RepoName: "web"},
		{RepoName: "worker"},
	}

	names := ws.RepoNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	expected := []string{"api", "web", "worker"}
	for i, n := range expected {
		if names[i] != n {
			t.Errorf("expected names[%d]=%q, got %q", i, n, names[i])
		}
	}
}

func TestWorkspaceJSONRoundtrip(t *testing.T) {
	ws := Workspace{
		Name:      "my-feature",
		Path:      "/home/user/.grove/workspaces/my-feature",
		Branch:    "feat/my-feature",
		CreatedAt: "2024-01-15T10:30:00.123456",
		Repos: []RepoWorktree{
			{
				RepoName:     "api",
				SourceRepo:   "/home/user/dev/api",
				WorktreePath: "/home/user/.grove/workspaces/my-feature/api",
				Branch:       "feat/my-feature",
			},
		},
	}

	data, err := json.Marshal(ws)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var ws2 Workspace
	if err := json.Unmarshal(data, &ws2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if ws2.Name != ws.Name {
		t.Errorf("name: got %q, want %q", ws2.Name, ws.Name)
	}
	if ws2.CreatedAt != ws.CreatedAt {
		t.Errorf("created_at: got %q, want %q", ws2.CreatedAt, ws.CreatedAt)
	}
	if len(ws2.Repos) != 1 {
		t.Fatalf("repos: got %d, want 1", len(ws2.Repos))
	}
	if ws2.Repos[0].RepoName != "api" {
		t.Errorf("repo_name: got %q, want 'api'", ws2.Repos[0].RepoName)
	}
}

func TestWorkspaceJSONBackwardCompat(t *testing.T) {
	// Missing repos and created_at — should default gracefully
	data := `{"name": "old", "path": "/tmp/old", "branch": "main"}`

	var ws Workspace
	if err := json.Unmarshal([]byte(data), &ws); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if ws.Name != "old" {
		t.Errorf("name: got %q, want 'old'", ws.Name)
	}
	if ws.Repos == nil {
		// Go's json.Unmarshal leaves nil slices as nil — this is fine
		// but we should handle nil repos gracefully throughout
	}
	if ws.CreatedAt != "" {
		t.Errorf("created_at should be empty, got %q", ws.CreatedAt)
	}
}

func TestRepoWorktreeJSONRoundtrip(t *testing.T) {
	rw := RepoWorktree{
		RepoName:     "api",
		SourceRepo:   "/src/api",
		WorktreePath: "/ws/api",
		Branch:       "feat/test",
	}

	data, err := json.Marshal(rw)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var rw2 RepoWorktree
	if err := json.Unmarshal(data, &rw2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if rw2 != rw {
		t.Errorf("roundtrip mismatch: got %+v, want %+v", rw2, rw)
	}
}

func TestDoctorIssueJSON(t *testing.T) {
	repoName := "api"
	issue := DoctorIssue{
		Workspace:       "test",
		Repo:            &repoName,
		Issue:           "worktree missing",
		SuggestedAction: "remove stale repo entry",
	}

	data, err := json.Marshal(issue)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var issue2 DoctorIssue
	if err := json.Unmarshal(data, &issue2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if *issue2.Repo != "api" {
		t.Errorf("repo: got %q, want 'api'", *issue2.Repo)
	}

	// Nil repo case
	issue3 := DoctorIssue{
		Workspace: "test",
		Repo:      nil,
		Issue:     "workspace dir missing",
	}
	data, _ = json.Marshal(issue3)
	if err := json.Unmarshal(data, &issue3); err != nil {
		t.Fatalf("unmarshal nil repo: %v", err)
	}
	if issue3.Repo != nil {
		t.Errorf("expected nil repo, got %v", issue3.Repo)
	}
}

func TestToJSON(t *testing.T) {
	ws := Workspace{Name: "test", Path: "/tmp/test", Branch: "main"}
	data, err := ToJSON(ws)
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty JSON output")
	}

	// Verify it's valid JSON
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Errorf("ToJSON output is not valid JSON: %v", err)
	}
}
