package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/models"
)

// TestLoadSaveWorkspaces tests the full round-trip.
func TestLoadSaveWorkspaces(t *testing.T) {
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "state.json")

	workspaces := []models.Workspace{
		{
			Name:   "test-ws",
			Path:   "/ws/test",
			Branch: "feat-x",
			Repos: []models.RepoWorktree{
				{RepoName: "svc-api", SourceRepo: "/repos/svc-api", WorktreePath: "/ws/test/svc-api", Branch: "feat-x"},
			},
			CreatedAt: "2025-01-01T00:00:00Z",
		},
	}

	// Save
	data, _ := json.MarshalIndent(workspaces, "", "  ")
	if err := os.WriteFile(statePath, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	// Load
	readData, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	var loaded []models.Workspace
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatal(err)
	}

	if len(loaded) != 1 {
		t.Fatalf("loaded %d workspaces, want 1", len(loaded))
	}
	if loaded[0].Name != "test-ws" {
		t.Errorf("Name = %q, want %q", loaded[0].Name, "test-ws")
	}
	if loaded[0].Branch != "feat-x" {
		t.Errorf("Branch = %q, want %q", loaded[0].Branch, "feat-x")
	}
	if len(loaded[0].Repos) != 1 {
		t.Errorf("Repos count = %d, want 1", len(loaded[0].Repos))
	}
}

func TestLoadEmpty(t *testing.T) {
	// Non-existent state file should return nil, nil
	tmp := t.TempDir()
	statePath := filepath.Join(tmp, "nonexistent.json")
	_, err := os.ReadFile(statePath)
	if !os.IsNotExist(err) {
		t.Error("expected ErrNotExist")
	}
}
