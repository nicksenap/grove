// Package dash implements the Grove TUI dashboard using bubbletea and lipgloss.
package dash

import "github.com/charmbracelet/lipgloss"

// Gruvbox palette — dark variant.
var (
	// Background tones
	colorBg0 = lipgloss.Color("#282828")
	colorBg1 = lipgloss.Color("#3c3836")
	colorBg2 = lipgloss.Color("#504945")
	colorBg3 = lipgloss.Color("#665c54")
	colorBg4 = lipgloss.Color("#7c6f64")

	// Foreground tones
	colorFg0 = lipgloss.Color("#fbf1c7")
	colorFg1 = lipgloss.Color("#ebdbb2")
	colorFg2 = lipgloss.Color("#d5c4a1")
	colorFg4 = lipgloss.Color("#a89984")

	// Accent colors
	colorRed    = lipgloss.Color("#fb4934")
	colorGreen  = lipgloss.Color("#b8bb26")
	colorYellow = lipgloss.Color("#fabd2f")
	colorBlue   = lipgloss.Color("#83a598")
	colorPurple = lipgloss.Color("#d3869b")
	colorAqua   = lipgloss.Color("#8ec07c")
	colorOrange = lipgloss.Color("#fe8019")
)

// styleHeader is the full-width top bar with app name and version.
var styleHeader = lipgloss.NewStyle().
	Background(colorBg1).
	Foreground(colorYellow).
	Bold(true).
	Padding(0, 1)

// styleHeaderDim is secondary text in the header.
var styleHeaderDim = lipgloss.NewStyle().
	Background(colorBg1).
	Foreground(colorFg4)

// styleFooter is the bottom keybinding hint bar.
var styleFooter = lipgloss.NewStyle().
	Background(colorBg1).
	Foreground(colorFg4).
	Padding(0, 1)

// stylePanel is an unfocused panel border.
var stylePanel = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(colorBg3)

// stylePanelFocused is a focused panel border.
var stylePanelFocused = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(colorYellow)

// stylePanelTitle is the panel title text.
var stylePanelTitle = lipgloss.NewStyle().
	Foreground(colorAqua).
	Bold(true)

// styleSelected is the highlighted list item row.
var styleSelected = lipgloss.NewStyle().
	Background(colorBg2).
	Foreground(colorFg0).
	Bold(true)

// styleNormal is an unselected list item row.
var styleNormal = lipgloss.NewStyle().
	Foreground(colorFg1)

// styleNormalDim is a muted list item row.
var styleNormalDim = lipgloss.NewStyle().
	Foreground(colorFg4)

// styleRepoName highlights a repo name.
var styleRepoName = lipgloss.NewStyle().
	Foreground(colorBlue).
	Bold(true)

// styleRepoNameSelected highlights a repo name when its workspace is selected.
var styleRepoNameSelected = lipgloss.NewStyle().
	Foreground(colorYellow).
	Bold(true)

// styleBranchLabel renders a branch name.
var styleBranchLabel = lipgloss.NewStyle().
	Foreground(colorPurple)

// styleClean renders "clean" status.
var styleClean = lipgloss.NewStyle().
	Foreground(colorGreen)

// styleDirty renders "N changes" status.
var styleDirty = lipgloss.NewStyle().
	Foreground(colorOrange)

// styleAhead renders ahead count.
var styleAhead = lipgloss.NewStyle().
	Foreground(colorBlue)

// styleBehind renders behind count.
var styleBehind = lipgloss.NewStyle().
	Foreground(colorRed)

// styleMissing renders a missing workspace warning.
var styleMissing = lipgloss.NewStyle().
	Foreground(colorRed).
	Italic(true)

// styleLoading renders a loading indicator.
var styleLoading = lipgloss.NewStyle().
	Foreground(colorFg4).
	Italic(true)

// styleStatusKey renders a label in the detail panel.
var styleStatusKey = lipgloss.NewStyle().
	Foreground(colorFg4).
	Width(10)

// styleStatusVal renders a value in the detail panel.
var styleStatusVal = lipgloss.NewStyle().
	Foreground(colorFg1)

// styleHelpKey renders a keybinding key in the help view.
var styleHelpKey = lipgloss.NewStyle().
	Foreground(colorYellow).
	Bold(true)

// styleHelpDesc renders a keybinding description in the help view.
var styleHelpDesc = lipgloss.NewStyle().
	Foreground(colorFg4)
