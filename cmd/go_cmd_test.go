package cmd

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDeleteWorkspacePropagatesFailure(t *testing.T) {
	original := newDeleteCommand
	t.Cleanup(func() { newDeleteCommand = original })

	newDeleteCommand = func(name string) *exec.Cmd {
		if name != "broken-ws" {
			t.Fatalf("unexpected workspace name %q", name)
		}
		return exec.Command("sh", "-c", "exit 23")
	}

	err := deleteWorkspace("broken-ws")
	if err == nil {
		t.Fatal("expected deletion failure")
	}
	if !strings.Contains(err.Error(), "deleting workspace broken-ws") {
		t.Fatalf("expected actionable workspace error, got %v", err)
	}
}

func TestDeleteWorkspaceWaitsForSuccessfulDeletion(t *testing.T) {
	original := newDeleteCommand
	t.Cleanup(func() { newDeleteCommand = original })

	marker := t.TempDir() + "/deleted"
	newDeleteCommand = func(name string) *exec.Cmd {
		return exec.Command("sh", "-c", "sleep 0.05; touch \"$1\"", "sh", marker)
	}

	if err := deleteWorkspace("clean-ws"); err != nil {
		t.Fatalf("delete workspace: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("deleteWorkspace returned before deletion completed")
	}
}
