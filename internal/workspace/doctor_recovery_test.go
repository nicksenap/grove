package workspace

import (
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/state"
)

// TestDoctorReportsRecoveryRecord proves doctor surfaces a stranded recovery
// record without mutating it, and that clean state with no records is silent.
func TestDoctorReportsRecoveryRecord(t *testing.T) {
	env := setupTestEnv(t)

	// No records, no workspaces → healthy.
	issues, fixed, err := env.svc.Doctor(false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if len(issues) != 0 || fixed != 0 {
		t.Fatalf("expected clean doctor, got issues=%v fixed=%d", issues, fixed)
	}

	// Write a stranded recovery record as if a create crashed mid-flight.
	rec := &state.OperationRecord{
		Kind:      state.OpCreate,
		Workspace: "feat-x",
		Phase:     "provisioning",
		LastError: "worktree add failed",
		Repos: []state.RepoOperation{
			{RepoName: "api", Status: state.RepoFailed},
		},
	}
	if err := env.svc.ops().Write(rec); err != nil {
		t.Fatalf("write record: %v", err)
	}

	// Doctor reports it, even without --fix, and does not remove it.
	issues, _, err = env.svc.Doctor(false)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	found := false
	for _, iss := range issues {
		if iss.Workspace == "feat-x" && strings.Contains(iss.Issue, "interrupted create") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected interrupted-create issue, got %+v", issues)
	}

	// The record must NOT be mutated by diagnosis, even with --fix (repair is a
	// later task).
	issues, _, err = env.svc.Doctor(true)
	if err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	if got, _ := env.svc.ops().Read(rec.ID); got == nil {
		t.Fatal("recovery record must survive diagnosis")
	}
	found = false
	for _, iss := range issues {
		if iss.Workspace == "feat-x" {
			found = true
		}
	}
	if !found {
		t.Fatal("record should still be reported after diagnosis-only --fix")
	}
}
