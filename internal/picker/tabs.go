package picker

import (
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/console"
)

// Tab is one page of a tabbed picker.
type Tab struct {
	Title   string
	Choices []string
	// Multi makes the tab multi-select. Checked items are shared between all
	// multi-select tabs by their text, so a selection survives a tab switch.
	Multi bool
}

// PickTabs shows a picker with one tab per entry in tabs; ←/→ switches tabs.
// It returns the index of the tab that was confirmed and the selection:
// the highlighted item for a single-select tab, or every checked item for a
// multi-select tab (the highlighted item if nothing is checked).
func PickTabs(prompt string, tabs []Tab) (int, []string, error) {
	if !console.IsTerminal(os.Stdin) || !console.IsTerminal(os.Stderr) {
		return 0, nil, fmt.Errorf("interactive selection requires a terminal. Provide explicit flags instead")
	}
	if len(tabs) == 0 {
		return 0, nil, fmt.Errorf("no choices available")
	}

	m, err := runPicker(newTabModel(prompt, tabs))
	if err != nil {
		return 0, nil, err
	}
	if m.cancelled {
		return 0, nil, ErrCancelled
	}
	return m.active, m.selected, nil
}

// tabModel is a row of selectModels with one active at a time.
type tabModel struct {
	prompt    string
	titles    []string
	tabs      []selectModel
	active    int
	checked   map[string]bool // shared by all multi-select tabs
	selected  []string
	cancelled bool
}

// tabBarHeight is the number of extra lines the tab bar adds to the view.
const tabBarHeight = 2

func newTabModel(prompt string, tabs []Tab) tabModel {
	m := tabModel{prompt: prompt, checked: make(map[string]bool)}
	for _, t := range tabs {
		m.titles = append(m.titles, t.Title)
		m.tabs = append(m.tabs, newSelectModel(prompt, t.Choices, t.Multi))
	}
	return m
}

// Update processes a message and returns the updated model and whether to quit.
func (m tabModel) Update(msg Msg) (tabModel, bool) {
	switch msg := msg.(type) {
	case WindowSizeMsg:
		msg.Height -= tabBarHeight
		for i := range m.tabs {
			m.tabs[i].applyWindowSize(msg)
		}
	case KeyMsg:
		return m.handleKey(msg)
	}
	return m, false
}

func (m tabModel) handleKey(msg KeyMsg) (tabModel, bool) {
	dir := msg.String()
	if m.tabs[m.active].searching && msg.Type == KeyRunes {
		dir = "" // h and l are search text while the search input is open
	}
	switch dir {
	case "left", "h":
		m.switchTab(-1)
		return m, false
	case "right", "l":
		m.switchTab(1)
		return m, false
	}

	tab, quit := m.tabs[m.active].Update(msg)
	m.tabs[m.active] = tab
	m.saveChecks()
	if !quit {
		return m, false
	}
	if tab.cancelled {
		m.cancelled = true
		return m, true
	}
	m.selected = m.selection()
	// Enter with nothing to pick (empty tab or no filter match) keeps the picker open.
	return m, len(m.selected) > 0
}

func (m *tabModel) switchTab(delta int) {
	n := len(m.tabs)
	m.active = (m.active + delta + n) % n
	m.loadChecks()
}

// saveChecks copies the active tab's checkboxes into the shared set.
func (m *tabModel) saveChecks() {
	tab := m.tabs[m.active]
	if !tab.multi {
		return
	}
	for i, c := range tab.choices {
		if tab.checked[i] {
			m.checked[c] = true
		} else {
			delete(m.checked, c)
		}
	}
}

// loadChecks sets the active tab's checkboxes from the shared set.
func (m *tabModel) loadChecks() {
	tab := &m.tabs[m.active]
	if !tab.multi {
		return
	}
	tab.checked = make(map[int]bool)
	for i, c := range tab.choices {
		if m.checked[c] {
			tab.checked[i] = true
		}
	}
}

// selection returns what Enter picks on the active tab.
func (m tabModel) selection() []string {
	tab := m.tabs[m.active]
	if !tab.multi || len(m.checked) == 0 {
		if len(tab.filtered) == 0 {
			return nil
		}
		return []string{tab.choices[tab.filtered[tab.cursor]]}
	}
	// Checked items in display order, the active tab first.
	var out []string
	seen := make(map[string]bool)
	add := func(t selectModel) {
		for _, c := range t.choices {
			if t.multi && m.checked[c] && !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	add(tab)
	for _, t := range m.tabs {
		add(t)
	}
	return out
}

func (m tabModel) View() string {
	var b strings.Builder
	tab := m.tabs[m.active]

	b.WriteString("\n" + m.prompt + "\n")
	if tab.searching {
		b.WriteString("  " + tab.keyHelp() + "\n\n")
	} else {
		b.WriteString("  ←/→ switch tab · " + tab.keyHelp() + "\n\n")
	}

	b.WriteString(" ")
	for i, title := range m.titles {
		if i == m.active {
			fmt.Fprintf(&b, " [%s]", title)
		} else {
			fmt.Fprintf(&b, "  %s ", title)
		}
	}
	b.WriteString("\n\n")

	b.WriteString(tab.body(len(m.checked)))
	return b.String()
}
