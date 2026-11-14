package output

import (
	"bytes"
	"testing"

	"github.com/nicksenap/grove/internal/models"
)

// TestWorkspaceJSONContract pins the workspace JSON that plugins and scripts
// parse. Adding a field is fine (update the expected text); renaming or
// removing one breaks consumers. Changing models.Workspace must not change
// this output by itself.
func TestWorkspaceJSONContract(t *testing.T) {
	full := models.Workspace{
		Name:      "fix-auth",
		Path:      "/ws/fix-auth",
		Branch:    "fix/auth",
		CreatedAt: "2026-09-15T10:00:00.000000",
		Repos: []models.RepoWorktree{
			{RepoName: "api", SourceRepo: "/src/api", WorktreePath: "/ws/fix-auth/api", Branch: "fix/auth"},
			{RepoName: "web", SourceRepo: "/src/web", WorktreePath: "/ws/fix-auth/web", Branch: "pr-head", PreserveBranch: true},
		},
		Source: &models.WorkspaceSource{Provider: "github", URL: "https://example.com/pr/1", Ref: "1", Title: "Fix auth"},
	}
	minimal := models.Workspace{Name: "empty", Path: "/ws/empty", Branch: "b", CreatedAt: "2026-09-16T10:00:00.000000"}

	const want = `{"name":"fix-auth","path":"/ws/fix-auth","branch":"fix/auth","created_at":"2026-09-15T10:00:00.000000",` +
		`"repos":[{"repo_name":"api","source_repo":"/src/api","worktree_path":"/ws/fix-auth/api","branch":"fix/auth"},` +
		`{"repo_name":"web","source_repo":"/src/web","worktree_path":"/ws/fix-auth/web","branch":"pr-head","preserve_branch":true}],` +
		`"source":{"provider":"github","url":"https://example.com/pr/1","ref":"1","title":"Fix auth"}}` + "\n" +
		`{"name":"empty","path":"/ws/empty","branch":"b","created_at":"2026-09-16T10:00:00.000000","repos":[]}` + "\n"

	var got bytes.Buffer
	if err := WriteJSONLines(&got, FromWorkspaces([]models.Workspace{full, minimal})); err != nil {
		t.Fatalf("WriteJSONLines: %v", err)
	}
	if got.String() != want {
		t.Errorf("workspace JSON changed.\n got: %s\nwant: %s", got.String(), want)
	}
}
