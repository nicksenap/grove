package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/output"
)

func TestWriteWorkspaceListLineFormats(t *testing.T) {
	workspaces := []models.Workspace{
		{Name: "alpha", Path: "/workspaces/alpha", Branch: "feat/a", CreatedAt: "2026-09-15T10:00:00", Repos: []models.RepoWorktree{{RepoName: "api"}}},
		{Name: "beta", Path: "/workspaces/beta", Branch: "feat/b", CreatedAt: "2026-09-16T10:00:00", Repos: []models.RepoWorktree{}},
	}

	tests := []struct {
		format output.Format
		want   string
	}{
		{output.Name, "alpha\nbeta\n"},
		{output.Path, "/workspaces/alpha\n/workspaces/beta\n"},
		{output.TSV, "NAME\tBRANCH\tREPOS\tCREATED\nalpha\tfeat/a\t1\t2026-09-15T10:00:00\nbeta\tfeat/b\t0\t2026-09-16T10:00:00\n"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			var stdout bytes.Buffer
			if err := writeWorkspaceList(&stdout, workspaces, tt.format); err != nil {
				t.Fatalf("writeWorkspaceList: %v", err)
			}
			if stdout.String() != tt.want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.want)
			}
		})
	}
}

func TestWriteWorkspaceListJSONLines(t *testing.T) {
	workspaces := []models.Workspace{{Name: "alpha", Path: "/workspaces/alpha", Repos: []models.RepoWorktree{}}}
	var stdout bytes.Buffer
	if err := writeWorkspaceList(&stdout, workspaces, output.JSONLines); err != nil {
		t.Fatalf("writeWorkspaceList: %v", err)
	}
	if got := stdout.String(); !strings.HasPrefix(got, `{"name":"alpha","path":"/workspaces/alpha"`) || strings.Count(got, "\n") != 1 {
		t.Fatalf("unexpected JSONL: %q", got)
	}
}

func TestWriteRepoListLineFormats(t *testing.T) {
	entries := []repoEntry{{Name: "api", Path: "/repos/api", Remote: "git@example/api.git", DisplayName: "example/api"}}

	tests := []struct {
		format output.Format
		want   string
	}{
		{output.Name, "api\n"},
		{output.Path, "/repos/api\n"},
		{output.TSV, "NAME\tOWNER/REPO\tPATH\napi\texample/api\t/repos/api\n"},
	}

	for _, tt := range tests {
		t.Run(string(tt.format), func(t *testing.T) {
			var stdout bytes.Buffer
			if err := writeRepoList(&stdout, entries, tt.format); err != nil {
				t.Fatalf("writeRepoList: %v", err)
			}
			if stdout.String() != tt.want {
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.want)
			}
		})
	}
}

func TestWriteWorkspaceShowPath(t *testing.T) {
	ws := &models.Workspace{Name: "alpha", Path: "/workspaces/alpha", Branch: "feat/a", Repos: []models.RepoWorktree{}}
	var stdout bytes.Buffer
	if err := writeWorkspaceShow(&stdout, ws, output.Path); err != nil {
		t.Fatalf("writeWorkspaceShow: %v", err)
	}
	if stdout.String() != "/workspaces/alpha\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
