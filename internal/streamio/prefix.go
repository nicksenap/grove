// Package streamio provides shared io helpers for streaming subprocess output
// live to the terminal with a per-line prefix, so concurrent or long-running
// commands (hooks, dev servers) remain attributable and never look hung.
package streamio

import (
	"bytes"
	"fmt"
	"io"
	"sync"
)

// maxLineBuffer caps the partial-line buffer so a process emitting a very long
// run of bytes without a newline cannot grow memory without bound.
const maxLineBuffer = 65536

// PrefixWriter wraps an io.Writer and prepends Prefix to every complete line it
// forwards. A partial line (no trailing newline) is buffered until the next
// Write completes it, the buffer exceeds maxLineBuffer, or Flush is called.
//
// Use one PrefixWriter per logical output stream. Write and Flush are safe for
// concurrent use on a single instance, which lets a command funnel both its
// stdout and stderr into the same PrefixWriter and keep chronological,
// single-prefix output. Call Flush once after the subprocess exits so a final
// line that lacks a trailing newline is not lost.
type PrefixWriter struct {
	Prefix string
	W      io.Writer

	mu  sync.Mutex
	buf []byte
	// midLine is true when the last bytes forwarded to W were an unterminated
	// partial line (a forced flush past maxLineBuffer). The continuation must
	// not be re-prefixed mid-line.
	midLine bool
}

// Write implements io.Writer. It forwards each complete line prefixed with
// Prefix and buffers any trailing partial line.
func (pw *PrefixWriter) Write(p []byte) (int, error) {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	pw.buf = append(pw.buf, p...)
	for {
		idx := bytes.IndexByte(pw.buf, '\n')
		if idx < 0 {
			break
		}
		pw.emit(pw.buf[:idx], true)
		pw.buf = pw.buf[idx+1:]
	}
	// Prevent unbounded growth: flush the oversized partial line without a
	// newline and remember we're mid-line so the continuation isn't prefixed.
	if len(pw.buf) > maxLineBuffer {
		pw.emit(pw.buf, false)
		pw.buf = pw.buf[:0]
	}
	return len(p), nil
}

// emit writes one (possibly partial) line to W. It adds Prefix only at the
// start of a fresh line and a trailing newline only when newline is true.
func (pw *PrefixWriter) emit(line []byte, newline bool) {
	if !pw.midLine {
		fmt.Fprint(pw.W, pw.Prefix)
	}
	pw.W.Write(line)
	if newline {
		fmt.Fprint(pw.W, "\n")
		pw.midLine = false
	} else {
		pw.midLine = true
	}
}

// Flush emits any buffered partial line, terminating it with a newline so the
// final line of a process that did not end in a newline is still shown. It is a
// no-op when nothing is buffered. Call once after the subprocess exits.
func (pw *PrefixWriter) Flush() {
	pw.mu.Lock()
	defer pw.mu.Unlock()

	if len(pw.buf) > 0 {
		pw.emit(pw.buf, true)
		pw.buf = pw.buf[:0]
	}
}
