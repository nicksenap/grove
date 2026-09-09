package workspace

import "testing"

func TestNewUnlinkCommandInvokesUnlinkTrash(t *testing.T) {
	cmd := newUnlinkCommand("/workspaces/.trash/foo-1")
	found := false
	for i, arg := range cmd.Args {
		if arg != "unlink-trash" {
			continue
		}
		if i+1 >= len(cmd.Args) || cmd.Args[i+1] != "/workspaces/.trash/foo-1" {
			t.Fatalf("args = %v", cmd.Args)
		}
		found = true
	}
	if !found {
		t.Fatalf("unlink-trash missing from %v", cmd.Args)
	}
}
