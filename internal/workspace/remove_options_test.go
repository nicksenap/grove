package workspace

import (
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/models"
)

func TestVerifyExpectedWorkspaceRejectsRecreatedWorkspace(t *testing.T) {
	ws := &models.Workspace{Name: "feature", Path: "/workspaces/feature", CreatedAt: "new"}
	err := verifyExpectedWorkspace(ws, RemoveOptions{
		ExpectedCreatedAt: "old",
		ExpectedPath:      ws.Path,
	})
	if err == nil || !strings.Contains(err.Error(), "changed after pre-delete checks") {
		t.Fatalf("error = %v", err)
	}
}

func TestVerifyExpectedWorkspaceAllowsLowLevelDeleteWithoutPrecondition(t *testing.T) {
	ws := &models.Workspace{Name: "feature", Path: "/workspaces/feature", CreatedAt: "new"}
	if err := verifyExpectedWorkspace(ws, RemoveOptions{}); err != nil {
		t.Fatal(err)
	}
}
