package console

import (
	"bytes"
	"strings"
	"testing"
)

func TestTableBasicAlignment(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Name", "Branch", "Count"})
	table.AddRow([]string{"workspace-one", "feat/long-branch-name", "5"})
	table.AddRow([]string{"ws", "main", "100"})
	table.Render()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 rows), got %d", len(lines))
	}

	// Header should be uppercase
	if !strings.HasPrefix(lines[0], "NAME") {
		t.Errorf("header should be uppercase, got: %q", lines[0])
	}

	// All lines should have the same column start positions (aligned)
	headerBranchIdx := strings.Index(lines[0], "BRANCH")
	row1BranchIdx := strings.Index(lines[1], "feat/long-branch-name")
	if headerBranchIdx != row1BranchIdx {
		t.Errorf("Branch column not aligned: header=%d, row1=%d", headerBranchIdx, row1BranchIdx)
	}
}

func TestTableEmptyHeaders(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{})
	table.Render()

	if buf.Len() != 0 {
		t.Error("empty headers should produce no output")
	}
}

func TestTableNoRows(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Name", "Value"})
	table.Render()

	output := buf.String()
	if !strings.Contains(output, "NAME") {
		t.Error("should still print header even with no rows")
	}
}

func TestTableMissingCells(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"A", "B", "C"})
	table.AddRow([]string{"1"}) // only 1 cell for 3 headers
	table.Render()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}

func TestTableWidthAdaptsToContent(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Short", "Long Column"})
	table.AddRow([]string{"a", "b"})
	table.AddRow([]string{"x", "very-long-value-here"})
	table.Render()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")

	headerLongIdx := strings.Index(lines[0], "LONG COLUMN")
	row2LongIdx := strings.Index(lines[2], "very-long-value-here")
	if headerLongIdx != row2LongIdx {
		t.Errorf("long column not aligned: header=%d, row2=%d", headerLongIdx, row2LongIdx)
	}
}

func TestTableNoPanicOnWideContent(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Name", "Description"})
	table.AddRow([]string{"test", strings.Repeat("x", 200)})
	table.Render()
	if buf.Len() == 0 {
		t.Error("expected some output")
	}
}

func TestTableTabwriterPadding(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"A", "B"})
	table.AddRow([]string{"hello", "world"})
	table.Render()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if !strings.Contains(lines[1], "hello") || !strings.Contains(lines[1], "world") {
		t.Errorf("unexpected row content: %q", lines[1])
	}
	helloEnd := strings.Index(lines[1], "hello") + 5
	worldStart := strings.Index(lines[1], "world")
	gap := worldStart - helloEnd
	if gap < 3 {
		t.Errorf("expected at least 3 spaces padding, got %d", gap)
	}
}

// --- Adaptive rendering tests ---

func TestAdaptiveNarrowTerminal(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Name", "Branch", "Path"})
	table.TermWidth = 50
	table.AddRow([]string{"my-workspace", "feat/very-long-branch-name", "/home/user/projects/workspace"})
	table.Render()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	// No line should exceed terminal width (use cellLen for visible width)
	for i, line := range lines {
		if cl := cellLen(line); cl > 50 {
			t.Errorf("line %d exceeds 50 visible chars: %d (%q)", i, cl, line)
		}
	}
}

func TestAdaptiveTruncatesLongValues(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Name", "Branch", "Path"})
	table.TermWidth = 40
	table.AddRow([]string{"workspace-name", "feat/very-long-branch", "/home/user/very/long/path/to/workspace"})
	table.Render()

	output := buf.String()
	if !strings.Contains(output, "…") {
		t.Error("expected truncation marker '…' in narrow output")
	}
}

func TestAdaptiveWideTerminalUsesTabwriter(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"A", "B"})
	table.TermWidth = 200
	table.AddRow([]string{"short", "val"})
	table.Render()

	// Wide terminal should use tabwriter (elastic tabs, ≥3 space padding)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	aEnd := strings.Index(lines[1], "short") + 5
	bStart := strings.Index(lines[1], "val")
	gap := bStart - aEnd
	if gap < 3 {
		t.Errorf("wide terminal should use tabwriter padding, got gap=%d", gap)
	}
}

func TestAdaptiveVeryNarrowTerminal(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Name", "Branch", "Repos", "Path", "Created"})
	table.TermWidth = 40
	table.AddRow([]string{"workspace", "main", "3", "~/dev/ws", "2025-01-15"})
	table.Render()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for i, line := range lines {
		if cl := cellLen(line); cl > 40 {
			t.Errorf("line %d exceeds 40 visible chars: %d (%q)", i, cl, line)
		}
	}
}

func TestCellLen(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"hello", 5},
		{"\033[1mhello\033[0m", 5},
		{"", 0},
		{"\033[31mred\033[0m text", 8},
	}
	for _, tt := range tests {
		got := cellLen(tt.input)
		if got != tt.want {
			t.Errorf("cellLen(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxWidth int
		want     string
	}{
		{"hello world", 8, "hello w…"},
		{"hi", 10, "hi"},
		{"abcdef", 1, "…"},
		{"abcdef", 4, "abc…"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.maxWidth)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxWidth, got, tt.want)
		}
	}
}

func TestAdaptiveMissingCells(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"A", "B", "C"})
	table.TermWidth = 40
	table.AddRow([]string{"only-one"})
	table.Render()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}

func TestAdaptiveANSITruncation(t *testing.T) {
	var buf bytes.Buffer
	table := NewTable(&buf, []string{"Name", "Status"})
	table.TermWidth = 20
	table.AddRow([]string{"workspace", "\033[31mlong red text here\033[0m"})
	table.Render()

	output := buf.String()
	// Should contain truncation marker
	if !strings.Contains(output, "…") {
		t.Error("expected ANSI-aware truncation")
	}
}
