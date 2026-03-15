package console_test

import (
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/console"
)

func TestTableRender_Structure(t *testing.T) {
	tbl := console.NewTable("Name", "Branch", "Repos", "Path")
	tbl.AddRow("my-ws", "feat-x", "3", "/dev/my-ws")
	tbl.AddRow("other", "main", "1", "/dev/other")

	out := tbl.Render()

	// Must have 6 lines: top border, header, separator, 2 data rows, bottom border.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 lines, got %d:\n%s", len(lines), out)
	}

	// Top border must start with ┌
	if !strings.Contains(lines[0], "┌") {
		t.Errorf("top border missing ┌: %q", lines[0])
	}
	// Bottom border must start with └
	if !strings.Contains(lines[5], "└") {
		t.Errorf("bottom border missing └: %q", lines[5])
	}
	// Separator between header and rows must contain ┼
	if !strings.Contains(lines[2], "┼") {
		t.Errorf("separator missing ┼: %q", lines[2])
	}
}

func TestTableRender_StyledCells(t *testing.T) {
	tbl := console.NewTable("Status")
	tbl.AddRow(console.Green("clean"))
	tbl.AddRow(console.Yellow("2 changed"))
	tbl.AddRow(console.Red("error"))

	out := tbl.Render()

	// The raw text values must appear in the output.
	for _, want := range []string{"clean", "2 changed", "error"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestTableRender_EmptyRows(t *testing.T) {
	tbl := console.NewTable("Col1", "Col2")
	out := tbl.Render()
	// With no data rows: top border, header, separator, bottom border = 4 lines.
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines for header-only table, got %d:\n%s", len(lines), out)
	}
}

func TestStyleHelpers(t *testing.T) {
	// Each helper must include the raw text in its output.
	helpers := []struct {
		name string
		fn   func(string) string
	}{
		{"Green", console.Green},
		{"Yellow", console.Yellow},
		{"Red", console.Red},
		{"Dim", console.Dim},
		{"Bold", console.Bold},
		{"Cyan", console.Cyan},
	}
	for _, h := range helpers {
		t.Run(h.name, func(t *testing.T) {
			got := h.fn("hello")
			if !strings.Contains(got, "hello") {
				t.Errorf("%s helper dropped the text: %q", h.name, got)
			}
		})
	}
}
