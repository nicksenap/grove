package workspace

import "github.com/nicksenap/grove/internal/state"

// OutcomeStatus is the overall status of a lifecycle operation.
type OutcomeStatus string

const (
	// OutcomeSuccess: every requested target completed and state is consistent.
	OutcomeSuccess OutcomeStatus = "success"
	// OutcomePartial: the core operation committed but a non-critical step
	// (e.g. setup/post-create hooks) failed. The workspace is valid.
	OutcomePartial OutcomeStatus = "partial"
	// OutcomeFailed: the operation did not commit; it was fully compensated.
	OutcomeFailed OutcomeStatus = "failed"
	// OutcomePending: the operation could not complete or fully compensate and
	// left a durable recovery record for retry/repair.
	OutcomePending OutcomeStatus = "pending"
	// OutcomeCancelled: the user explicitly cancelled; nothing was mutated.
	OutcomeCancelled OutcomeStatus = "cancelled"
)

// RepoOutcome is the ordered per-repository result of an operation.
type RepoOutcome struct {
	RepoName string
	Status   state.RepoStatus
	Phase    string
	Message  string
	Err      error
}

// OperationResult is the typed, ordered outcome of a lifecycle operation. It is
// rendered to stderr by the human CLI; the public machine envelope (issue #63)
// is a separate concern.
type OperationResult struct {
	Kind      state.OperationKind
	Workspace string
	Status    OutcomeStatus
	Repos     []RepoOutcome
	// RecordID is the recovery-record id when Status is Pending.
	RecordID string
	// Message is a human summary; Err is the terminal error, if any.
	Message string
	Err     error
}

// NonZeroExit reports whether the CLI should exit non-zero for this result.
// Exit 0 only for complete success or explicit cancellation.
func (r *OperationResult) NonZeroExit() bool {
	switch r.Status {
	case OutcomeSuccess, OutcomeCancelled:
		return false
	default:
		return true
	}
}

// toError synthesizes an error for the legacy error-returning method shims.
func (r *OperationResult) toError() error {
	if !r.NonZeroExit() {
		return nil
	}
	if r.Err != nil {
		return r.Err
	}
	if r.Message != "" {
		return &resultError{msg: r.Message}
	}
	return &resultError{msg: string(r.Status) + " " + string(r.Kind)}
}

type resultError struct{ msg string }

func (e *resultError) Error() string { return e.msg }

// addRepo appends an ordered repository outcome.
func (r *OperationResult) addRepo(o RepoOutcome) { r.Repos = append(r.Repos, o) }
