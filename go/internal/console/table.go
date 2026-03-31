package console

import (
	"fmt"
	"io"
	"strings"
)

// Table is a simple table formatter.
type Table struct {
	w       io.Writer
	headers []string
	rows    [][]string
}

// NewTable creates a table writing to the given writer.
func NewTable(w io.Writer, headers []string) *Table {
	return &Table{w: w, headers: headers}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(row []string) {
	t.rows = append(t.rows, row)
}

// Render prints the table.
func (t *Table) Render() {
	if len(t.headers) == 0 {
		return
	}

	// Calculate column widths
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Print header
	var headerParts []string
	for i, h := range t.headers {
		headerParts = append(headerParts, fmt.Sprintf("%-*s", widths[i], h))
	}
	fmt.Fprintf(t.w, "%s%s%s\n", boldCyan, strings.Join(headerParts, "  "), reset)

	// Print separator
	var sepParts []string
	for _, w := range widths {
		sepParts = append(sepParts, strings.Repeat("─", w))
	}
	fmt.Fprintf(t.w, "%s%s%s\n", dim, strings.Join(sepParts, "──"), reset)

	// Print rows
	for _, row := range t.rows {
		var parts []string
		for i := range t.headers {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			parts = append(parts, fmt.Sprintf("%-*s", widths[i], cell))
		}
		fmt.Fprintln(t.w, strings.Join(parts, "  "))
	}
}
