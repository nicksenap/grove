package state

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nicksenap/grove/internal/models"
)

func TestOperationRecordRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ops := NewOperationStore(dir)

	rec := &OperationRecord{
		Kind:      OpCreate,
		Workspace: "feat-login",
		Phase:     "provisioning",
		Repos: []RepoOperation{
			{
				RepoName:          "api",
				SourceRepo:        "/src/api",
				WorktreePath:      "/ws/feat-login/api",
				Branch:            "feat/login",
				Phase:             "worktree_added",
				Status:            RepoDone,
				BranchOwnership:   OwnCreated,
				WorktreeOwnership: OwnCreated,
			},
			{
				RepoName: "web",
				Status:   RepoPending,
			},
		},
		LastError: "worktree add failed",
		Retryable: true,
		Details:   map[string]string{"base_branch": "main"},
	}

	if err := ops.Write(rec); err != nil {
		t.Fatalf("write: %v", err)
	}
	if rec.ID == "" {
		t.Fatal("write should assign an ID")
	}
	if rec.Version != operationRecordVersion {
		t.Fatalf("version: got %d want %d", rec.Version, operationRecordVersion)
	}
	if rec.CreatedAt == "" || rec.UpdatedAt == "" || rec.PID == 0 {
		t.Fatalf("write should stamp timestamps and pid: %+v", rec)
	}

	got, err := ops.Read(rec.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !reflect.DeepEqual(got, rec) {
		t.Fatalf("round-trip mismatch:\ngot  %+v\nwant %+v", got, rec)
	}

	// Ordered listing.
	rec2 := &OperationRecord{Kind: OpDelete, Workspace: "old"}
	if err := ops.Write(rec2); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	list, err := ops.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 records, got %d", len(list))
	}
	if !(list[0].ID < list[1].ID) {
		t.Fatalf("records not sorted by id: %v", []string{list[0].ID, list[1].ID})
	}

	// Delete is idempotent.
	if err := ops.Delete(rec.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := ops.Delete(rec.ID); err != nil {
		t.Fatalf("second delete should be a no-op: %v", err)
	}
	if _, err := ops.Read(rec.ID); err == nil {
		t.Fatal("expected read of deleted record to fail")
	}
}

func TestOperationRecordSurvivesProcessExit(t *testing.T) {
	dir := t.TempDir()
	groveDir := filepath.Join(dir, ".grove")
	if err := os.MkdirAll(groveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(dir, "recid")

	cmd := runHelper(t, "writeop",
		envHelperDir+"="+groveDir,
		envHelperName+"=crashed-ws",
		envHelperFile+"="+ready,
	)
	if err := cmd.Run(); err != nil {
		t.Fatalf("writeop helper failed: %v", err)
	}

	idBytes, err := os.ReadFile(ready)
	if err != nil {
		t.Fatalf("read helper id: %v", err)
	}
	id := string(idBytes)

	ops := NewOperationStore(groveDir)
	got, err := ops.Read(id)
	if err != nil {
		t.Fatalf("record should survive helper exit: %v", err)
	}
	if got.Workspace != "crashed-ws" || got.Kind != OpCreate {
		t.Fatalf("unexpected record: %+v", got)
	}
	if len(got.Repos) != 1 || got.Repos[0].WorktreeOwnership != OwnCreated {
		t.Fatalf("resource ownership not preserved: %+v", got.Repos)
	}
}

func TestOperationRecordUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	ops := NewOperationStore(dir)
	rec := &OperationRecord{Kind: OpCreate, Workspace: "future"}
	if err := ops.Write(rec); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _ := ops.Read(rec.ID)
	// Only the exact current version with a known kind is supported.
	for _, v := range []int{operationRecordVersion + 1, 0, -1} {
		got.Version = v
		if got.Supported() {
			t.Fatalf("version %d must not be supported", v)
		}
	}
	got.Version = operationRecordVersion
	got.Kind = "bogus"
	if got.Supported() {
		t.Fatal("unknown-kind record must not be supported")
	}
}

func TestOperationStoreRejectsUnknownKindAndBadID(t *testing.T) {
	dir := t.TempDir()
	ops := NewOperationStore(dir)
	if err := ops.Write(&OperationRecord{Kind: "nonsense", Workspace: "x"}); err == nil {
		t.Fatal("expected error writing unknown kind")
	}
	if err := ops.Write(&OperationRecord{Kind: "", Workspace: "x"}); err == nil {
		t.Fatal("expected error writing empty kind")
	}
	if _, err := ops.Read("../escape"); err == nil {
		t.Fatal("expected error reading id with path escape")
	}
	if err := ops.Delete("a/b"); err == nil {
		t.Fatal("expected error deleting id with separator")
	}
}

func TestOperationRecordProvisioningAndRenameRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ops := NewOperationStore(dir)
	rec := &OperationRecord{
		Kind:           OpRename,
		Workspace:      "new-name",
		RenameFrom:     "old-name",
		RenameTo:       "new-name",
		RenameFromPath: "/ws/old-name",
		RenameToPath:   "/ws/new-name",
		Repos: []RepoOperation{
			{RepoName: "api", BaseBranch: "stage", Mode: ProvisionFromBase, Status: RepoDone},
			{RepoName: "web", BaseBranch: "main", Mode: ProvisionTrack, Status: RepoDone},
		},
	}
	if err := ops.Write(rec); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ops.Read(rec.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.RenameFrom != "old-name" || got.RenameToPath != "/ws/new-name" {
		t.Fatalf("rename identity not preserved: %+v", got)
	}
	if got.Repos[0].BaseBranch != "stage" || got.Repos[1].Mode != ProvisionTrack {
		t.Fatalf("per-repo base/mode not preserved: %+v", got.Repos)
	}
}

func TestOperationRecordRootOwnershipAndSourceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ops := NewOperationStore(dir)
	rec := &OperationRecord{
		Kind:          OpCreate,
		Workspace:     "feat",
		Path:          "/ws/feat",
		RootOwnership: OwnCreated,
		Source: &models.WorkspaceSource{
			Provider: "github",
			URL:      "https://github.com/o/r/pull/7",
			Ref:      "7",
			Title:    "Add login",
		},
	}
	if err := ops.Write(rec); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ops.Read(rec.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.RootOwnership != OwnCreated {
		t.Fatalf("root ownership not preserved: %+v", got)
	}
	if got.Source == nil || got.Source.Provider != "github" || got.Source.Ref != "7" {
		t.Fatalf("source provenance not preserved: %+v", got.Source)
	}
}

func TestOperationRetryAfterDirSyncFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".grove")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ops := NewOperationStore(dir) // operations/ absent

	// First write fails at the directory-entry sync (dir may be created).
	boom := errors.New("dirsync")
	ops.failDirEntrySync = func() error { return boom }
	if err := ops.Write(&OperationRecord{Kind: OpCreate, Workspace: "x"}); err != boom {
		t.Fatalf("expected dir-sync error, got %v", err)
	}

	// Retry with the fault cleared must still sync the parent (mkdirAllSync must
	// not skip syncing just because the directory now exists) and succeed.
	ops.failDirEntrySync = nil
	rec := &OperationRecord{Kind: OpCreate, Workspace: "x"}
	if err := ops.Write(rec); err != nil {
		t.Fatalf("retry write: %v", err)
	}
	if _, err := ops.Read(rec.ID); err != nil {
		t.Fatalf("record should exist after retry: %v", err)
	}
}

func TestOperationDeleteAbsentDirIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".grove", "operations")
	ops := &OperationStore{Dir: dir} // dir never created
	if err := ops.Delete("20260101T000000.000000000-000000000001-create-x-abcdef012345"); err != nil {
		t.Fatalf("delete on absent journal dir must be a no-op: %v", err)
	}
}

func TestOperationWriteRejectsUnsupportedVersion(t *testing.T) {
	dir := t.TempDir()
	ops := NewOperationStore(dir)
	// A nonzero, non-current version must be refused, not silently rewritten.
	if err := ops.Write(&OperationRecord{Version: operationRecordVersion + 1, Kind: OpCreate, Workspace: "x"}); err == nil {
		t.Fatal("expected refusal to write unsupported version")
	}
	// Version 0 is defaulted to the current version.
	rec := &OperationRecord{Kind: OpCreate, Workspace: "x"}
	if err := ops.Write(rec); err != nil {
		t.Fatalf("write v0: %v", err)
	}
	if rec.Version != operationRecordVersion {
		t.Fatalf("version should default to %d, got %d", operationRecordVersion, rec.Version)
	}
}

func TestOperationDeletePropagatesStatError(t *testing.T) {
	// A journal "directory" that is actually a file makes Stat succeed but is not
	// a real absence; more importantly a permission error on the parent must
	// surface. Simulate by pointing Dir at a path whose parent denies traversal.
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	base := t.TempDir()
	locked := filepath.Join(base, "locked")
	if err := os.MkdirAll(filepath.Join(locked, "operations"), 0o755); err != nil {
		t.Fatal(err)
	}
	ops := NewOperationStore(locked)
	// Remove traversal permission on the parent so Stat on operations/ fails with
	// EACCES rather than ENOENT.
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	err := ops.Delete("20260101T000000.000000000-000000000001-create-x-abcdef012345")
	if err == nil {
		t.Fatal("expected non-absence stat error to propagate from Delete")
	}
	if os.IsNotExist(err) {
		t.Fatalf("error should not be treated as absence: %v", err)
	}
}

func TestOperationDirSyncFailurePropagates(t *testing.T) {
	// Creation-time directory sync failure surfaces from Write.
	t.Run("create", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), ".grove")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		ops := NewOperationStore(dir) // operations/ does not exist yet
		boom := errors.New("dirsync")
		ops.failDirEntrySync = func() error { return boom }
		if err := ops.Write(&OperationRecord{Kind: OpCreate, Workspace: "x"}); err != boom {
			t.Fatalf("expected dir-sync error from Write, got %v", err)
		}
	})
	// Delete-time directory sync failure surfaces from Delete.
	t.Run("delete", func(t *testing.T) {
		dir := t.TempDir()
		ops := NewOperationStore(dir)
		rec := &OperationRecord{Kind: OpSync, Workspace: "x"}
		if err := ops.Write(rec); err != nil {
			t.Fatalf("write: %v", err)
		}
		boom := errors.New("dirsync")
		ops.failDirEntrySync = func() error { return boom }
		if err := ops.Delete(rec.ID); err != boom {
			t.Fatalf("expected dir-sync error from Delete, got %v", err)
		}
	})
}

func TestOperationRecordCommitStatusAndOwnershipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ops := NewOperationStore(dir)
	rec := &OperationRecord{
		Kind:         OpDelete,
		Workspace:    "doomed",
		Path:         "/ws/doomed",
		Force:        true,
		CommitStatus: CommitAttempted,
		Repos: []RepoOperation{{
			RepoName:          "api",
			Status:            RepoFailed,
			BranchOwnership:   OwnPreexisting,
			WorktreeOwnership: OwnCreated,
			ErrorCode:         "WORKTREE_DIRTY",
			Error:             "uncommitted changes",
			Retryable:         true,
		}},
	}
	if err := ops.Write(rec); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ops.Read(rec.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.CommitStatus != CommitAttempted || !got.Force || got.Path != "/ws/doomed" {
		t.Fatalf("top-level repair fields not preserved: %+v", got)
	}
	r := got.Repos[0]
	if r.BranchOwnership != OwnPreexisting || r.WorktreeOwnership != OwnCreated || r.ErrorCode != "WORKTREE_DIRTY" {
		t.Fatalf("per-repo ownership/error not preserved: %+v", r)
	}
}

func TestNewOperationIDIsTimeOrdered(t *testing.T) {
	prev := ""
	for i := 0; i < 50; i++ {
		id := NewOperationID(OpCreate, "ws")
		if id <= prev {
			t.Fatalf("ids not strictly increasing: %q then %q", prev, id)
		}
		prev = id
	}
}

func TestOperationStoreNoLeakedTempAfterWrite(t *testing.T) {
	dir := t.TempDir()
	ops := NewOperationStore(dir)
	if err := ops.Write(&OperationRecord{Kind: OpSync, Workspace: "x"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(ops.Dir, ".*.tmp"))
	if len(matches) != 0 {
		t.Fatalf("leaked temp files: %v", matches)
	}
	// A record in flight (dotfile) must be ignored by List.
	if err := os.WriteFile(filepath.Join(ops.Dir, ".partial.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := ops.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected in-flight dotfile ignored, got %d records", len(list))
	}
}
