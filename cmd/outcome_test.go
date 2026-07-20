package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
)

func TestFprintRepoOutcomesRendersAllRepos(t *testing.T) {
	res := &workspace.OperationResult{
		Kind:      state.OpCreate,
		Workspace: "ws",
		Status:    workspace.OutcomePending,
		RecordID:  "op-123",
		Repos: []workspace.RepoOutcome{
			{RepoName: "api", Status: state.RepoDone, Phase: "provision"},
			{RepoName: "web", Status: state.RepoFailed, Phase: "provision", Err: errors.New("boom")},
		},
	}
	var buf bytes.Buffer
	fprintRepoOutcomes(&buf, res)
	out := buf.String()
	for _, want := range []string{"api", "web", "boom", "op-123", "recovery record"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
}

func TestOperationResultExitMapping(t *testing.T) {
	cases := map[workspace.OutcomeStatus]bool{
		workspace.OutcomeSuccess:   false,
		workspace.OutcomeCancelled: false,
		workspace.OutcomePartial:   true,
		workspace.OutcomeFailed:    true,
		workspace.OutcomePending:   true,
	}
	for status, wantNonZero := range cases {
		res := &workspace.OperationResult{Status: status}
		if res.NonZeroExit() != wantNonZero {
			t.Fatalf("status %s: NonZeroExit=%v want %v", status, res.NonZeroExit(), wantNonZero)
		}
	}
}
