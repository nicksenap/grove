// Package machine defines Grove's machine-readable CLI contract: the response
// envelope agents parse, the stable error codes they branch on, and the exit
// codes they check.
//
// The contract exists so a coding agent (or a CI script) never has to parse
// human-formatted tables. It has three rules:
//
//  1. In machine mode, stdout carries exactly one JSON envelope — nothing else.
//     Progress, warnings, hook output, and debug logs go to stderr.
//  2. Every response, success or failure, uses the same envelope shape and
//     carries schemaVersion so a client can detect an incompatible Grove.
//  3. Error codes and exit codes are part of the public API. Renaming a code is
//     a breaking change; adding one is not.
//
// See docs/agent-cli.md for the full policy.
package machine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// SchemaVersion is the envelope version. It is bumped only when an existing
// field changes meaning or disappears — adding fields does not bump it.
const SchemaVersion = 1

// Envelope is the single response shape for machine mode. Exactly one of Result
// or Error is set. NextActions is always present (possibly empty) so clients can
// index it without a nil check.
type Envelope struct {
	OK            bool     `json:"ok"`
	SchemaVersion int      `json:"schemaVersion"`
	Result        any      `json:"result,omitempty"`
	Error         *Failure `json:"error,omitempty"`
	Fix           string   `json:"fix,omitempty"`
	Warnings      []string `json:"warnings,omitempty"`
	NextActions   []Action `json:"next_actions"`
}

// Failure is the structured error body. Code is stable; Message is human text
// and may change freely. Details carries command-specific context (e.g. which
// repos failed) and is never required by the contract.
type Failure struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// Action is a safe, relevant command an agent can run next. Command is a
// literal shell command so an agent can execute it without reconstruction.
type Action struct {
	Description string `json:"description"`
	Command     string `json:"command"`
}

// NextAction builds an Action.
func NextAction(description, command string) Action {
	return Action{Description: description, Command: command}
}

// ---------------------------------------------------------------------------
// Output mode
// ---------------------------------------------------------------------------

// Format selects human or machine output.
type Format string

const (
	// FormatText is the default human-oriented output (tables, colors, prompts).
	FormatText Format = "text"
	// FormatJSON is machine mode: one envelope on stdout, everything else on stderr.
	FormatJSON Format = "json"
)

var (
	mu       sync.RWMutex
	format   = FormatText
	warnings []string
	emitted  bool
)

// SetFormat sets the output mode from a user-supplied string.
func SetFormat(s string) error {
	switch Format(strings.ToLower(strings.TrimSpace(s))) {
	case FormatText:
		setFormat(FormatText)
	case FormatJSON:
		setFormat(FormatJSON)
	default:
		return fmt.Errorf("unknown --format %q (want \"text\" or \"json\")", s)
	}
	return nil
}

func setFormat(f Format) {
	mu.Lock()
	defer mu.Unlock()
	format = f
}

// Current returns the active output format.
func Current() Format {
	mu.RLock()
	defer mu.RUnlock()
	return format
}

// Enabled reports whether machine mode is active. Call it before writing
// anything to stdout.
func Enabled() bool { return Current() == FormatJSON }

// DetectEarly scans raw args for the format flag before Cobra parses them, so
// pre-command output (like the update notice) can honor machine mode. Cobra
// remains the source of truth and still validates the value.
//
// An unrecognized value enables machine mode anyway. Passing --format at all is an
// explicit request for a parseable answer, so the rejection has to be parseable
// too — otherwise the one case where a client most needs a machine-readable error
// (it asked for a format Grove does not have) is the one case it would get bare
// text on stderr.
func DetectEarly(args []string) {
	for i, a := range args {
		switch {
		case a == "--format" || a == "-o":
			if i+1 < len(args) {
				applyEarlyFormat(args[i+1])
			}
		case strings.HasPrefix(a, "--format="):
			applyEarlyFormat(strings.TrimPrefix(a, "--format="))
		case strings.HasPrefix(a, "-o="):
			applyEarlyFormat(strings.TrimPrefix(a, "-o="))
		}
	}
}

func applyEarlyFormat(value string) {
	if err := SetFormat(value); err != nil {
		setFormat(FormatJSON)
	}
}

// Warn records a warning for inclusion in the envelope. Callers still print it
// to stderr; this only makes it machine-visible.
func Warn(msg string) {
	if !Enabled() {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	warnings = append(warnings, msg)
}

// Reset clears accumulated state. Tests only.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	format = FormatText
	warnings = nil
	emitted = false
}

// ---------------------------------------------------------------------------
// Emitting
// ---------------------------------------------------------------------------

// Emit writes a success envelope to stdout. It is a no-op outside machine mode,
// so call sites can be unconditional and let the human path print its own view.
func Emit(result any, actions ...Action) {
	if !Enabled() {
		return
	}
	write(os.Stdout, successEnvelope(result, actions))
}

// EmitTo is Emit against an explicit writer, ignoring the active format. Used by
// tests and by commands that build an envelope for a file (e.g. gw plan).
func EmitTo(w io.Writer, result any, actions ...Action) {
	write(w, successEnvelope(result, actions))
}

func successEnvelope(result any, actions []Action) Envelope {
	if result == nil {
		result = struct{}{}
	}
	return Envelope{
		OK:            true,
		SchemaVersion: SchemaVersion,
		Result:        result,
		Warnings:      takeWarnings(),
		NextActions:   normalizeActions(actions),
	}
}

// EmitError writes a failure envelope to stdout and returns the exit code the
// process should use. Outside machine mode it writes nothing and just returns
// the code, leaving the human error message to the caller.
func EmitError(err error) int {
	if err == nil {
		return ExitOK
	}
	if Enabled() {
		write(os.Stdout, ErrorEnvelope(err))
	}
	return ExitCodeFor(err)
}

// ErrorEnvelope converts any error into the failure envelope. Unclassified
// errors become CodeInternal, which is the contract's explicit "Grove does not
// model this failure yet" signal rather than a silent mismatch.
func ErrorEnvelope(err error) Envelope {
	e := AsError(err)
	return Envelope{
		OK:            false,
		SchemaVersion: SchemaVersion,
		Error:         &Failure{Code: e.Code, Message: e.Message, Details: e.Details},
		Fix:           e.Fix,
		Warnings:      takeWarnings(),
		NextActions:   normalizeActions(e.NextActions),
	}
}

// write marshals one envelope followed by a newline. A marshal failure must
// still leave valid JSON on stdout, so it degrades to a hand-built envelope.
func write(w io.Writer, env Envelope) {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		fmt.Fprintf(w, `{"ok":false,"schemaVersion":%d,"error":{"code":%q,"message":%q},"next_actions":[]}`+"\n",
			SchemaVersion, CodeInternal, "could not serialize response: "+err.Error())
		return
	}
	fmt.Fprintln(w, string(data))
	mu.Lock()
	emitted = true
	mu.Unlock()
}

// Emitted reports whether an envelope has already been written, so a command
// can avoid producing a second one on a later failure.
func Emitted() bool {
	mu.RLock()
	defer mu.RUnlock()
	return emitted
}

func takeWarnings() []string {
	mu.Lock()
	defer mu.Unlock()
	w := warnings
	warnings = nil
	return w
}

func normalizeActions(actions []Action) []Action {
	if actions == nil {
		return []Action{}
	}
	return actions
}
