package cmd

import "testing"

func TestUnlinkTrashCommandIsHidden(t *testing.T) {
	if !unlinkTrashCmd.Hidden {
		t.Fatal("unlink-trash should be a hidden internal command")
	}
	if unlinkTrashCmd.Name() != "unlink-trash" {
		t.Fatalf("command name = %q", unlinkTrashCmd.Name())
	}
}
