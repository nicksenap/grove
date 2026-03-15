package models

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNewWorkspace(t *testing.T) {
	repos := []RepoWorktree{
		{RepoName: "svc-api", SourceRepo: "/repos/svc-api", WorktreePath: "/ws/test/svc-api", Branch: "feat-x"},
	}
	ws := NewWorkspace("test", "/ws/test", "feat-x", repos)

	if ws.Name != "test" {
		t.Errorf("Name = %q, want %q", ws.Name, "test")
	}
	if ws.Branch != "feat-x" {
		t.Errorf("Branch = %q, want %q", ws.Branch, "feat-x")
	}
	if len(ws.Repos) != 1 {
		t.Errorf("Repos count = %d, want 1", len(ws.Repos))
	}

	// CreatedAt should be a valid RFC3339 timestamp
	if _, err := time.Parse(time.RFC3339, ws.CreatedAt); err != nil {
		t.Errorf("CreatedAt = %q, not valid RFC3339: %v", ws.CreatedAt, err)
	}
}

func TestRepoWorktreeDir(t *testing.T) {
	got := RepoWorktreeDir("/ws/test", "svc-api")
	want := filepath.Join("/ws/test", "svc-api")
	if got != want {
		t.Errorf("RepoWorktreeDir = %q, want %q", got, want)
	}
}

func TestDoctorIssue(t *testing.T) {
	issue := DoctorIssue{
		Level:   "error",
		Message: "workspace missing",
		Fix:     "remove state",
	}
	if issue.Level != "error" {
		t.Errorf("Level = %q, want %q", issue.Level, "error")
	}
}
