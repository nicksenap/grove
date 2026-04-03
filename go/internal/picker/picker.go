package picker

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nicksenap/grove/internal/console"
)

// PickOne shows a single-select picker with type-to-search.
// Returns the selected item or error if cancelled.
func PickOne(prompt string, choices []string) (string, error) {
	if len(choices) == 0 {
		return "", fmt.Errorf("no choices available")
	}
	if len(choices) == 1 {
		return choices[0], nil
	}
	if !console.IsTerminal(os.Stdin) || !console.IsTerminal(os.Stderr) {
		return "", fmt.Errorf("interactive selection requires a terminal. Provide explicit flags instead")
	}

	m := newSelectModel(prompt, choices, false)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr), tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return "", err
	}

	model := result.(selectModel)
	if model.cancelled {
		return "", fmt.Errorf("selection cancelled")
	}
	return model.selected[0], nil
}

// PickMany shows a multi-select picker with type-to-search.
// Prepends an "(all)" option for quick select-all.
// Returns the selected items or error if cancelled.
func PickMany(prompt string, choices []string) ([]string, error) {
	if !console.IsTerminal(os.Stdin) || !console.IsTerminal(os.Stderr) {
		return nil, fmt.Errorf("interactive selection requires a terminal. Provide explicit flags instead")
	}
	if len(choices) == 0 {
		return nil, fmt.Errorf("no choices available")
	}

	// Prepend "(all)" option like the Python version
	display := append([]string{"(all)"}, choices...)

	m := newSelectModel(prompt, display, true)
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr), tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		return nil, err
	}

	model := result.(selectModel)
	if model.cancelled {
		return nil, fmt.Errorf("selection cancelled")
	}
	if len(model.selected) == 0 {
		return nil, fmt.Errorf("no items selected")
	}

	// If "(all)" was selected, return all original choices
	for _, s := range model.selected {
		if s == "(all)" {
			return choices, nil
		}
	}
	return model.selected, nil
}

// selectModel is the bubbletea model for select/multiselect.
type selectModel struct {
	prompt    string
	choices   []string
	filtered  []int // indices into choices matching the filter
	cursor    int
	checked   map[int]bool // for multi-select
	multi     bool
	filter    string
	selected  []string
	cancelled bool

	// Viewport: how many items to show at once, and the scroll offset.
	viewHeight int
	offset     int // index of the first visible item in filtered
	termHeight int // actual terminal height
}

const defaultViewHeight = 20

func newSelectModel(prompt string, choices []string, multi bool) selectModel {
	indices := make([]int, len(choices))
	for i := range choices {
		indices[i] = i
	}
	return selectModel{
		prompt:     prompt,
		choices:    choices,
		filtered:   indices,
		checked:    make(map[int]bool),
		multi:      multi,
		viewHeight: defaultViewHeight,
	}
}

func (m selectModel) Init() tea.Cmd {
	return nil
}

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.termHeight = msg.Height
		// Reserve lines for header (prompt + hint + filter + padding + footer)
		overhead := 6
		if m.filter != "" {
			overhead += 1
		}
		available := msg.Height - overhead
		if available < 5 {
			available = 5
		}
		m.viewHeight = available

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.scrollToCursor()
			}

		case "down", "j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
				m.scrollToCursor()
			}

		case "pgup":
			m.cursor -= m.viewHeight
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.scrollToCursor()

		case "pgdown":
			m.cursor += m.viewHeight
			if m.cursor >= len(m.filtered) {
				m.cursor = len(m.filtered) - 1
			}
			m.scrollToCursor()

		case "home":
			m.cursor = 0
			m.scrollToCursor()

		case "end":
			m.cursor = len(m.filtered) - 1
			m.scrollToCursor()

		case "tab", " ":
			if m.multi && len(m.filtered) > 0 {
				idx := m.filtered[m.cursor]
				if m.checked[idx] {
					delete(m.checked, idx)
				} else {
					m.checked[idx] = true
				}
			}

		case "enter":
			if m.multi {
				for idx := range m.checked {
					m.selected = append(m.selected, m.choices[idx])
				}
			} else if len(m.filtered) > 0 {
				m.selected = []string{m.choices[m.filtered[m.cursor]]}
			}
			return m, tea.Quit

		case "backspace":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.updateFilter()
			}

		default:
			if len(msg.String()) == 1 {
				m.filter += msg.String()
				m.updateFilter()
			}
		}
	}
	return m, nil
}

func (m *selectModel) scrollToCursor() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.viewHeight {
		m.offset = m.cursor - m.viewHeight + 1
	}
}

func (m *selectModel) updateFilter() {
	if m.filter == "" {
		m.filtered = make([]int, len(m.choices))
		for i := range m.choices {
			m.filtered[i] = i
		}
	} else {
		m.filtered = nil
		lower := strings.ToLower(m.filter)
		for i, c := range m.choices {
			if strings.Contains(strings.ToLower(c), lower) {
				m.filtered = append(m.filtered, i)
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	m.offset = 0
	m.scrollToCursor()
}

func (m selectModel) View() string {
	var b strings.Builder

	b.WriteString("\n" + m.prompt + "\n")
	if m.multi {
		b.WriteString("  ↑/↓ navigate · tab select · type to search · enter confirm\n\n")
	} else {
		b.WriteString("  ↑/↓ navigate · type to search · enter confirm\n\n")
	}

	if m.filter != "" {
		b.WriteString(fmt.Sprintf("  filter: %s\n\n", m.filter))
	}

	// Count selected for multi-select
	if m.multi && len(m.checked) > 0 {
		b.WriteString(fmt.Sprintf("  %d selected\n\n", len(m.checked)))
	}

	total := len(m.filtered)
	if total == 0 {
		b.WriteString("  (no matches)\n")
		return b.String()
	}

	// Determine visible window
	end := m.offset + m.viewHeight
	if end > total {
		end = total
	}

	// Show scroll indicator at top if not at the start
	if m.offset > 0 {
		b.WriteString(fmt.Sprintf("  ↑ %d more\n", m.offset))
	}

	for i := m.offset; i < end; i++ {
		idx := m.filtered[i]

		cursor := "  "
		if i == m.cursor {
			cursor = "❯ "
		}

		if m.multi {
			check := "[ ]"
			if m.checked[idx] {
				check = "[✓]"
			}
			b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, check, m.choices[idx]))
		} else {
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, m.choices[idx]))
		}
	}

	// Show scroll indicator at bottom if more items below
	if end < total {
		b.WriteString(fmt.Sprintf("  ↓ %d more\n", total-end))
	}

	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
