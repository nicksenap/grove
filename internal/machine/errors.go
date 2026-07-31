package machine

import (
	"errors"
	"fmt"
)

// Code is a stable, machine-readable error identifier. Agents branch on these,
// so a code's name and meaning are part of Grove's public API: adding a code is
// a compatible change, renaming or repurposing one is not.
type Code string

const (
	// CodeInternal is an unclassified failure. Its presence means Grove does not
	// model this failure yet — treat the message as opaque.
	CodeInternal Code = "INTERNAL"

	// CodeUsage is a malformed invocation: bad flag, missing argument, or a
	// command that needs input Grove cannot obtain non-interactively.
	CodeUsage Code = "USAGE"

	// Not found.
	CodeWorkspaceNotFound Code = "WORKSPACE_NOT_FOUND"
	CodeRepoNotFound      Code = "REPO_NOT_FOUND"
	CodeNoWorkspaces      Code = "NO_WORKSPACES"

	// Conflicts: the request collides with existing state.
	CodeWorkspaceExists Code = "WORKSPACE_EXISTS"
	CodeWorktreeExists  Code = "WORKTREE_EXISTS"
	CodeBranchConflict  Code = "BRANCH_CONFLICT"
	CodeStateChanged    Code = "STATE_CHANGED"

	// Preconditions: the environment is not ready for the request.
	CodeNotInitialized Code = "NOT_INITIALIZED"
	CodeGitFailed      Code = "GIT_FAILED"
	CodeHookFailed     Code = "HOOK_FAILED"

	// CodePermission is a filesystem or credential permission failure.
	CodePermission Code = "PERMISSION_DENIED"

	// CodeTransient is a failure that may succeed on retry (network, lock
	// contention). Agents may retry these; they must not retry other codes.
	CodeTransient Code = "TRANSIENT"

	// CodeCancelled means the user aborted an interactive flow.
	CodeCancelled Code = "CANCELLED"
)

// Exit codes. Grouping failures by class lets a caller react without parsing
// JSON at all: retry on 7, fix its own invocation on 2, stop on 4.
const (
	ExitOK           = 0
	ExitFailure      = 1 // unclassified / internal
	ExitUsage        = 2 // bad invocation
	ExitNotFound     = 3 // named thing does not exist
	ExitConflict     = 4 // state collides with the request
	ExitPrecondition = 5 // environment not ready
	ExitPermission   = 6
	ExitTransient    = 7 // retry may help
	ExitCancelled    = 8
)

// exitCodes maps each error code to its exit class. Every Code must appear here;
// TestEveryCodeHasExitCode enforces it.
var exitCodes = map[Code]int{
	CodeInternal:          ExitFailure,
	CodeUsage:             ExitUsage,
	CodeWorkspaceNotFound: ExitNotFound,
	CodeRepoNotFound:      ExitNotFound,
	CodeNoWorkspaces:      ExitNotFound,
	CodeWorkspaceExists:   ExitConflict,
	CodeWorktreeExists:    ExitConflict,
	CodeBranchConflict:    ExitConflict,
	CodeStateChanged:      ExitConflict,
	CodeNotInitialized:    ExitPrecondition,
	CodeGitFailed:         ExitPrecondition,
	CodeHookFailed:        ExitPrecondition,
	CodePermission:        ExitPermission,
	CodeTransient:         ExitTransient,
	CodeCancelled:         ExitCancelled,
}

// AllCodes returns every declared error code, so docs and tests can enumerate
// the contract instead of hand-maintaining a second list.
func AllCodes() []Code {
	return []Code{
		CodeInternal,
		CodeUsage,
		CodeWorkspaceNotFound,
		CodeRepoNotFound,
		CodeNoWorkspaces,
		CodeWorkspaceExists,
		CodeWorktreeExists,
		CodeBranchConflict,
		CodeStateChanged,
		CodeNotInitialized,
		CodeGitFailed,
		CodeHookFailed,
		CodePermission,
		CodeTransient,
		CodeCancelled,
	}
}

// Error is a classified failure. Grove's service layer returns these so the CLI
// can emit a stable code, a suggested fix, and safe follow-up commands without
// pattern-matching on message text.
type Error struct {
	Code        Code
	Message     string
	Fix         string
	Details     any
	NextActions []Action
	Wrapped     error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Wrapped }

// WithFix returns a copy carrying a suggested remedy.
func (e *Error) WithFix(fix string) *Error {
	c := *e
	c.Fix = fix
	return &c
}

// WithActions returns a copy carrying safe next commands.
func (e *Error) WithActions(actions ...Action) *Error {
	c := *e
	c.NextActions = actions
	return &c
}

// WithDetails returns a copy carrying structured context.
func (e *Error) WithDetails(details any) *Error {
	c := *e
	c.Details = details
	return &c
}

// Errorf builds a classified error.
func Errorf(code Code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Wrap classifies an existing error, preserving it for errors.Is/As.
func Wrap(code Code, err error, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Wrapped: err}
}

// AsError returns the classified error in err's chain, or an CodeInternal error
// wrapping it. It never returns nil for a non-nil err.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Code: CodeInternal, Message: err.Error(), Wrapped: err}
}

// CodeFor returns the stable code for any error.
func CodeFor(err error) Code {
	if err == nil {
		return ""
	}
	return AsError(err).Code
}

// ExitCodeFor returns the process exit code for any error.
func ExitCodeFor(err error) int {
	if err == nil {
		return ExitOK
	}
	if code, ok := exitCodes[AsError(err).Code]; ok {
		return code
	}
	return ExitFailure
}
