package streamio

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestPrefixWriterLines verifies complete lines are prefixed and a partial line
// is held until a later Write completes it.
func TestPrefixWriterLines(t *testing.T) {
	var buf bytes.Buffer
	pw := &PrefixWriter{Prefix: "[test] ", W: &buf}

	pw.Write([]byte("hello\nworld\n"))
	pw.Write([]byte("partial"))
	pw.Write([]byte(" line\ndone\n"))
	pw.Flush()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	expected := []string{
		"[test] hello",
		"[test] world",
		"[test] partial line",
		"[test] done",
	}
	if len(lines) != len(expected) {
		t.Fatalf("expected %d lines, got %d: %v", len(expected), len(lines), lines)
	}
	for i, line := range lines {
		if line != expected[i] {
			t.Errorf("line %d: got %q, want %q", i, line, expected[i])
		}
	}
}

// TestPrefixWriterFlushPartial verifies Flush emits a trailing line that lacks
// a newline (the original prefixWriter silently dropped it).
func TestPrefixWriterFlushPartial(t *testing.T) {
	var buf bytes.Buffer
	pw := &PrefixWriter{Prefix: "[p] ", W: &buf}

	pw.Write([]byte("no-newline-tail"))
	if buf.Len() != 0 {
		t.Fatalf("partial line should be buffered until Flush, got %q", buf.String())
	}
	pw.Flush()

	if got := buf.String(); got != "[p] no-newline-tail\n" {
		t.Errorf("Flush output = %q, want %q", got, "[p] no-newline-tail\n")
	}
}

// TestPrefixWriterOversizedLine verifies a line longer than maxLineBuffer is
// force-flushed and its continuation is NOT re-prefixed mid-line.
func TestPrefixWriterOversizedLine(t *testing.T) {
	var buf bytes.Buffer
	pw := &PrefixWriter{Prefix: "[x] ", W: &buf}

	big := bytes.Repeat([]byte("a"), maxLineBuffer+1000)
	pw.Write(big)
	pw.Write([]byte("more"))
	pw.Write([]byte("\n"))
	pw.Flush()

	out := buf.String()
	if n := strings.Count(out, "[x] "); n != 1 {
		t.Errorf("prefix should appear exactly once for an oversized line, got %d: %.40q...", n, out)
	}
	if !strings.HasSuffix(out, "more\n") {
		t.Errorf("continuation should be appended without a new prefix, got tail %q", out[len(out)-10:])
	}
}

// TestPrefixWriterConcurrentWrites verifies Write/Flush are safe under -race
// and every emitted line carries the prefix.
func TestPrefixWriterConcurrentWrites(t *testing.T) {
	var buf bytes.Buffer
	pw := &PrefixWriter{Prefix: "[c] ", W: &buf}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			fmt.Fprintf(pw, "line-%d\n", n)
		}(i)
	}
	wg.Wait()
	pw.Flush()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 50 {
		t.Fatalf("expected 50 lines, got %d", len(lines))
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "[c] ") {
			t.Errorf("line missing prefix: %q", line)
		}
	}
}
