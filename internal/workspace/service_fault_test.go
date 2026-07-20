package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
)

// faultJournal injects recovery-journal write/delete failures per instance.
type faultJournal struct {
	inner      journalStore
	failWrite  error
	failDelete error
}

func (f *faultJournal) Write(r *state.OperationRecord) error {
	if f.failWrite != nil {
		return f.failWrite
	}
	return f.inner.Write(r)
}
func (f *faultJournal) Read(id string) (*state.OperationRecord, error) { return f.inner.Read(id) }
func (f *faultJournal) List() ([]state.OperationRecord, error)         { return f.inner.List() }
func (f *faultJournal) Delete(id string) error {
	if f.failDelete != nil {
		return f.failDelete
	}
	return f.inner.Delete(id)
}

// TestServiceCommitSeamInjectable proves the commit boundary can be
// failure-injected per Service instance, covering both a pre-commit failure
// (edits made but state NOT applied) and an applied-then-error ambiguous commit.
func TestServiceCommitSeamInjectable(t *testing.T) {
	env := setupTestEnv(t)

	// Pre-commit failure: the callback runs and edits the snapshot, but the
	// commit seam returns before m.Commit(), so nothing is persisted.
	callbackRan := false
	boom := errors.New("pre-commit")
	env.svc.commitFault = func(m *state.Mutation) error { return boom }
	err := env.svc.withMutation(context.Background(), func(m *state.Mutation) error {
		callbackRan = true
		return m.Add(models.NewWorkspace("x", "/tmp/x", "main"))
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected pre-commit failure, got %v", err)
	}
	if !callbackRan {
		t.Fatal("callback must run (and edit) before the pre-commit failure")
	}
	if ws, _ := env.svc.State.Load(); len(ws) != 0 {
		t.Fatalf("pre-commit failure must not persist state, got %d", len(ws))
	}

	// Applied-then-error: the seam commits, then returns an ambiguous error.
	after := errors.New("post-commit")
	env.svc.commitFault = func(m *state.Mutation) error {
		if e := m.Commit(); e != nil {
			return e
		}
		return after
	}
	err = env.svc.withMutation(context.Background(), func(m *state.Mutation) error {
		return m.Add(models.NewWorkspace("y", "/tmp/y", "main"))
	})
	if !errors.Is(err, after) {
		t.Fatalf("expected applied-then-error, got %v", err)
	}
	if ws, _ := env.svc.State.Load(); len(ws) != 1 {
		t.Fatalf("applied-then-error must persist state, got %d", len(ws))
	}

	// Journal write failure is injectable per instance.
	fj := &faultJournal{inner: env.svc.Ops, failWrite: errors.New("journal")}
	env.svc.Ops = fj
	if err := env.svc.ops().Write(&state.OperationRecord{Kind: state.OpCreate, Workspace: "z"}); err == nil {
		t.Fatal("expected journal write failure")
	}
}

// TestDoctorPendingRecordSuppressesStaleFix proves a pending recovery record
// prevents the destructive "remove stale state entry" fix for that workspace.
func TestDoctorPendingRecordSuppressesStaleFix(t *testing.T) {
	env := setupTestEnv(t)

	ws := models.NewWorkspace("ghosted", filepath.Join(env.wsDir, "ghosted-missing"), "main")
	if err := env.svc.State.AddWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.ops().Write(&state.OperationRecord{Kind: state.OpCreate, Workspace: "ghosted"}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := env.svc.Doctor(true); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	if got, _ := env.svc.State.GetWorkspace("ghosted"); got == nil {
		t.Fatal("pending record must prevent stale-state removal")
	}
}

// TestDoctorCorruptJournalSuppressesAllFixes proves an unreadable journal
// disables destructive fixes entirely (the affected workspace is unknown).
func TestDoctorCorruptJournalSuppressesAllFixes(t *testing.T) {
	env := setupTestEnv(t)

	ws := models.NewWorkspace("ghosted", filepath.Join(env.wsDir, "ghosted-missing"), "main")
	if err := env.svc.State.AddWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	opsDir := filepath.Join(env.groveDir, "operations")
	if err := os.MkdirAll(opsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(opsDir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, _, err := env.svc.Doctor(true); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	if got, _ := env.svc.State.GetWorkspace("ghosted"); got == nil {
		t.Fatal("corrupt journal must suppress all destructive fixes")
	}
}
