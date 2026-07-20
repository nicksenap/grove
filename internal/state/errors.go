package state

import (
	"errors"
	"fmt"
)

// Stable internal error codes for state operations. These are consumed by
// service-layer typed outcomes (issue #59) and are not part of any public
// machine envelope.
const (
	// CodeStateLockTimeout indicates the advisory state lock could not be
	// acquired within the configured timeout. It is retryable.
	CodeStateLockTimeout = "STATE_LOCK_TIMEOUT"
	// CodeStateConflict indicates a same-record conflict revalidated under the
	// lock (e.g. a workspace with the same name already exists).
	CodeStateConflict = "STATE_CONFLICT"
	// CodeStateNested indicates a nested state mutation was attempted from the
	// same goroutine that already holds a Mutation handle.
	CodeStateNested = "STATE_NESTED_MUTATION"
	// CodeStateInactiveHandle indicates a Mutation handle was used after its
	// WithMutation callback returned.
	CodeStateInactiveHandle = "STATE_INACTIVE_HANDLE"
)

// CodedError carries a stable internal error code alongside a human message.
type CodedError struct {
	Code      string
	Message   string
	Err       error
	Retryable bool
}

func (e *CodedError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *CodedError) Unwrap() error { return e.Err }

// CodeOf returns the stable code for err if it is a *CodedError, else "".
func CodeOf(err error) string {
	if err == nil {
		return ""
	}
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// IsRetryable reports whether err is a retryable coded error.
func IsRetryable(err error) bool {
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Retryable
	}
	return false
}
