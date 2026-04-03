package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/x/term"
)

// Table is a simple table formatter that adapts to terminal width.
type Table struct {
	w       io.Writer
	headers []string
	rows    [][]string

	// TermWidth overrides detected terminal width (0 = auto-detect).
	TermWidth int
}

// NewTable creates a table writing to the given writer.
func NewTable(w io.Writer, headers []string) *Table {
	return &Table{w: w, headers: headers}
}

// AddRow adds a row to the table.
func (t *Table) AddRow(row []string) {
	t.rows = append(t.rows, row)
}

// Render prints the table, adapting to terminal width when possible.
func (t *Table) Render() {
	if len(t.headers) == 0 {
		return
	}

	width := t.termWidth()
	if width > 0 {
		t.renderAdaptive(width)
	} else {
		t.renderTabwriter()
	}
}

// termWidth returns the terminal width, or 0 if unknown.
func (t *Table) termWidth() int {
	if t.TermWidth > 0 {
		return t.TermWidth
	}
	if f, ok := t.w.(*os.File); ok {
		w, _, err := term.GetSize(f.Fd())
		if err == nil && w > 0 {
			return w
		}
	}
	return 0
}

// renderAdaptive renders the table with column widths fitted to the terminal.
func (t *Table) renderAdaptive(totalWidth int) {
	n := len(t.headers)
	padding := 2 // spaces between columns

	// Measure natural (max content) width per column.
	natural := make([]int, n)
	for i, h := range t.headers {
		natural[i] = len(strings.ToUpper(h))
	}
	for _, row := range t.rows {
		for i := 0; i < n && i < len(row); i++ {
			if cl := cellLen(row[i]); cl > natural[i] {
				natural[i] = cl
			}
		}
	}

	// Total natural width including padding.
	naturalTotal := 0
	for _, w := range natural {
		naturalTotal += w
	}
	naturalTotal += (n - 1) * padding

	// If it fits, use tabwriter (looks best with elastic tabs).
	if naturalTotal <= totalWidth {
		t.renderTabwriter()
		return
	}

	// Allocate column widths proportionally, with minimums.
	colWidths := allocateWidths(natural, totalWidth, padding)

	// Print header.
	t.printRow(upperStrings(t.headers), colWidths, padding)

	// Print rows.
	for _, row := range t.rows {
		cells := make([]string, n)
		for i := range t.headers {
			if i < len(row) {
				cells[i] = row[i]
			}
		}
		t.printRow(cells, colWidths, padding)
	}
}

// allocateWidths distributes totalWidth across columns.
func allocateWidths(natural []int, totalWidth, padding int) []int {
	n := len(natural)
	available := totalWidth - (n-1)*padding
	available = max(available, n) // at least 1 char per column

	// Start with min width of 4 or natural, whichever is smaller.
	widths := make([]int, n)
	minWidth := 4
	remaining := available

	for i, nat := range natural {
		w := min(minWidth, nat)
		widths[i] = w
		remaining -= w
	}

	if remaining < 0 {
		// Even minimums don't fit — just divide evenly.
		each := available / n
		for i := range widths {
			widths[i] = each
		}
		// Give leftover to first columns.
		leftover := available - each*n
		for i := range leftover {
			widths[i]++
		}
		return widths
	}

	// Distribute remaining space proportionally to how much each column wants.
	totalWant := 0
	for i, nat := range natural {
		want := max(nat-widths[i], 0)
		totalWant += want
	}

	if totalWant > 0 {
		distributed := 0
		for i, nat := range natural {
			want := nat - widths[i]
			if want <= 0 {
				continue
			}
			share := remaining * want / totalWant
			widths[i] += share
			distributed += share
		}
		// Give any rounding remainder to the first column that can use it.
		leftover := remaining - distributed
		for i := range widths {
			if leftover <= 0 {
				break
			}
			if widths[i] < natural[i] {
				give := min(leftover, natural[i]-widths[i])
				widths[i] += give
				leftover -= give
			}
		}
	}

	// Final clamp: total column width must not exceed available.
	total := 0
	for _, w := range widths {
		total += w
	}
	for total > available && total > n {
		// Shrink the widest column.
		maxIdx := 0
		for i := 1; i < n; i++ {
			if widths[i] > widths[maxIdx] {
				maxIdx = i
			}
		}
		widths[maxIdx]--
		total--
	}

	return widths
}

// printRow prints a single row with fixed column widths.
func (t *Table) printRow(cells []string, widths []int, padding int) {
	parts := make([]string, len(cells))
	for i, cell := range cells {
		w := widths[i]
		cl := cellLen(cell)
		if cl > w && w > 1 {
			cell = truncate(cell, w)
		}
		parts[i] = padRight(cell, w)
	}
	fmt.Fprintln(t.w, strings.TrimRight(strings.Join(parts, strings.Repeat(" ", padding)), " "))
}

// renderTabwriter uses the original elastic-tab approach (for wide terminals).
func (t *Table) renderTabwriter() {
	tw := tabwriter.NewWriter(t.w, 6, 4, 3, ' ', 0)
	fmt.Fprintln(tw, strings.ToUpper(strings.Join(t.headers, "\t")))
	for _, row := range t.rows {
		cells := make([]string, len(t.headers))
		for i := range t.headers {
			if i < len(row) {
				cells[i] = row[i]
			}
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	tw.Flush()
}

// cellLen returns the visible length of a string (ignoring ANSI escape codes).
func cellLen(s string) int {
	length := 0
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		length++
	}
	return length
}

// truncate shortens s to fit in maxWidth visible characters, adding "…".
func truncate(s string, maxWidth int) string {
	if cellLen(s) <= maxWidth {
		return s
	}
	if maxWidth <= 1 {
		return "…"
	}
	target := maxWidth - 1 // leave room for ellipsis

	var b strings.Builder
	visible := 0
	inEscape := false
	for _, r := range s {
		if r == '\033' {
			inEscape = true
			b.WriteRune(r)
			continue
		}
		if inEscape {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		if visible >= target {
			break
		}
		b.WriteRune(r)
		visible++
	}
	b.WriteString("…")
	return b.String()
}

// padRight pads s to the given visible width.
func padRight(s string, width int) string {
	cl := cellLen(s)
	if cl >= width {
		return s
	}
	return s + strings.Repeat(" ", width-cl)
}

func upperStrings(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToUpper(s)
	}
	return out
}
