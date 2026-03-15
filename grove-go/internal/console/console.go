// Package console provides styled terminal output using lipgloss.
package console

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Gruvbox-inspired colors
	colorRed     = lipgloss.Color("#fb4934")
	colorGreen   = lipgloss.Color("#b8bb26")
	colorYellow  = lipgloss.Color("#fabd2f")
	colorDim     = lipgloss.Color("#928374")
	colorCyan    = lipgloss.Color("#8ec07c")
	colorMagenta = lipgloss.Color("#d3869b")

	errorStyle   = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	warningStyle = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	infoStyle    = lipgloss.NewStyle().Foreground(colorDim)
)

// Error prints an error message to stderr.
func Error(msg string) {
	fmt.Fprintln(os.Stderr, errorStyle.Render("error:")+" "+msg)
}

// Errorf prints a formatted error message to stderr.
func Errorf(format string, args ...interface{}) {
	Error(fmt.Sprintf(format, args...))
}

// Success prints a success message.
func Success(msg string) {
	fmt.Println(successStyle.Render("ok:") + " " + msg)
}

// Successf prints a formatted success message.
func Successf(format string, args ...interface{}) {
	Success(fmt.Sprintf(format, args...))
}

// Info prints a dim info message.
func Info(msg string) {
	fmt.Println(infoStyle.Render(msg))
}

// Infof prints a formatted info message.
func Infof(format string, args ...interface{}) {
	Info(fmt.Sprintf(format, args...))
}

// Warning prints a warning message.
func Warning(msg string) {
	fmt.Println(warningStyle.Render("warn:") + " " + msg)
}

// Warningf prints a formatted warning message.
func Warningf(format string, args ...interface{}) {
	Warning(fmt.Sprintf(format, args...))
}

// --- Cell style helpers ---

// Green returns s styled in gruvbox green.
func Green(s string) string {
	return lipgloss.NewStyle().Foreground(colorGreen).Render(s)
}

// Yellow returns s styled in gruvbox yellow.
func Yellow(s string) string {
	return lipgloss.NewStyle().Foreground(colorYellow).Render(s)
}

// Red returns s styled in gruvbox red.
func Red(s string) string {
	return lipgloss.NewStyle().Foreground(colorRed).Render(s)
}

// Dim returns s styled in dim grey.
func Dim(s string) string {
	return lipgloss.NewStyle().Foreground(colorDim).Render(s)
}

// Bold returns s rendered bold.
func Bold(s string) string {
	return lipgloss.NewStyle().Bold(true).Render(s)
}

// Cyan returns s styled in gruvbox cyan/aqua.
func Cyan(s string) string {
	return lipgloss.NewStyle().Foreground(colorCyan).Render(s)
}

// Magenta returns s styled in gruvbox magenta/purple.
func Magenta(s string) string {
	return lipgloss.NewStyle().Foreground(colorMagenta).Render(s)
}

// --- Table ---

// Box-drawing characters for the table borders.
const (
	boxTopLeft     = "┌"
	boxTopRight    = "┐"
	boxBottomLeft  = "└"
	boxBottomRight = "┘"
	boxHoriz       = "─"
	boxVert        = "│"
	boxTopMid      = "┬"
	boxBottomMid   = "┴"
	boxMidLeft     = "├"
	boxMidRight    = "┤"
	boxCross       = "┼"
)

// Table renders a bordered table with bold-cyan headers and dim borders,
// matching the style of Python Rich's Table with border_style="dim" and
// header_style="bold cyan".
type Table struct {
	headers []string
	rows    [][]string
}

// NewTable creates a new Table with the given column headers.
func NewTable(headers ...string) *Table {
	return &Table{headers: headers}
}

// AddRow appends a row of cells. Each cell may contain ANSI-styled strings
// produced by the helper functions (Green, Yellow, etc.). The table uses
// lipgloss.Width to measure visual width, ignoring escape sequences.
func (t *Table) AddRow(cells ...string) {
	// Pad or truncate to the number of columns.
	row := make([]string, len(t.headers))
	for i := range row {
		if i < len(cells) {
			row[i] = cells[i]
		}
	}
	t.rows = append(t.rows, row)
}

// Render builds and returns the complete table string. Borders are dim, the
// header row is bold cyan, and cell content is rendered as-is (preserving any
// inline ANSI styling applied via the helper functions).
func (t *Table) Render() string {
	cols := len(t.headers)
	if cols == 0 {
		return ""
	}

	dimStyle := lipgloss.NewStyle().Foreground(colorDim)
	headerCellStyle := lipgloss.NewStyle().Foreground(colorCyan).Bold(true)

	// Measure the visual width of each column: max of header and all cell values.
	colWidths := make([]int, cols)
	for i, h := range t.headers {
		if w := lipgloss.Width(h); w > colWidths[i] {
			colWidths[i] = w
		}
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i >= cols {
				break
			}
			if w := lipgloss.Width(cell); w > colWidths[i] {
				colWidths[i] = w
			}
		}
	}

	// borderSeg returns a horizontal segment of width w using the given fill rune.
	borderSeg := func(w int) string {
		return strings.Repeat(boxHoriz, w+2) // +2 for padding spaces
	}

	// buildHorizLine constructs a full horizontal divider line.
	// left, mid, right are the junction characters.
	buildHorizLine := func(left, mid, right string) string {
		var sb strings.Builder
		sb.WriteString(dimStyle.Render(left))
		for i, w := range colWidths {
			sb.WriteString(dimStyle.Render(borderSeg(w)))
			if i < cols-1 {
				sb.WriteString(dimStyle.Render(mid))
			}
		}
		sb.WriteString(dimStyle.Render(right))
		return sb.String()
	}

	// buildRow constructs a data row. cells are the raw (possibly ANSI-styled) values.
	// styleCell is called on the plain text to produce the displayed cell content; pass
	// nil to render cells as-is.
	buildRow := func(cells []string, cellStyleFn func(string) string) string {
		var sb strings.Builder
		sb.WriteString(dimStyle.Render(boxVert))
		for i, w := range colWidths {
			var content string
			if i < len(cells) {
				content = cells[i]
			}
			if cellStyleFn != nil {
				content = cellStyleFn(content)
			}
			// Pad to column width using visual width.
			pad := w - lipgloss.Width(content)
			if pad < 0 {
				pad = 0
			}
			sb.WriteString(" ")
			sb.WriteString(content)
			sb.WriteString(strings.Repeat(" ", pad))
			sb.WriteString(" ")
			sb.WriteString(dimStyle.Render(boxVert))
		}
		return sb.String()
	}

	var lines []string

	// Top border.
	lines = append(lines, buildHorizLine(boxTopLeft, boxTopMid, boxTopRight))

	// Header row.
	styledHeaders := make([]string, len(t.headers))
	for i, h := range t.headers {
		styledHeaders[i] = headerCellStyle.Render(h)
	}
	lines = append(lines, buildRow(styledHeaders, nil))

	// Separator after header.
	lines = append(lines, buildHorizLine(boxMidLeft, boxCross, boxMidRight))

	// Data rows.
	for _, row := range t.rows {
		lines = append(lines, buildRow(row, nil))
	}

	// Bottom border.
	lines = append(lines, buildHorizLine(boxBottomLeft, boxBottomMid, boxBottomRight))

	return strings.Join(lines, "\n")
}

// Print renders the table and writes it to stdout followed by a newline.
func (t *Table) Print() {
	fmt.Println(t.Render())
}
