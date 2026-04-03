package picker

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/nicksenap/grove/internal/console"
	"golang.org/x/term"
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
	m, err := runPicker(m)
	if err != nil {
		return "", err
	}

	if m.cancelled {
		return "", fmt.Errorf("selection cancelled")
	}
	return m.selected[0], nil
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
	m, err := runPicker(m)
	if err != nil {
		return nil, err
	}

	if m.cancelled {
		return nil, fmt.Errorf("selection cancelled")
	}
	if len(m.selected) == 0 {
		return nil, fmt.Errorf("no items selected")
	}

	// If "(all)" was selected, return all original choices
	for _, s := range m.selected {
		if s == "(all)" {
			return choices, nil
		}
	}
	return m.selected, nil
}

// --- Key and message types ---

// KeyType identifies the kind of key event.
type KeyType int

const (
	KeyRunes     KeyType = iota // printable character(s)
	KeyUp                       // ↑
	KeyDown                     // ↓
	KeyEnter                    // Enter/Return
	KeyEsc                      // Escape
	KeyTab                      // Tab
	KeyBackspace                // Backspace/Delete
	KeyCtrlC                    // Ctrl+C
	KeyPgUp                     // Page Up
	KeyPgDown                   // Page Down
	KeyHome                     // Home
	KeyEnd                      // End
	KeySpace                    // Space bar
	KeyUnknown                  // unrecognised sequence
)

// KeyMsg represents a keyboard event.
type KeyMsg struct {
	Type  KeyType
	Runes []rune
}

// String returns a human-readable name, matching the strings the model switches on.
func (k KeyMsg) String() string {
	switch k.Type {
	case KeyUp:
		return "up"
	case KeyDown:
		return "down"
	case KeyEnter:
		return "enter"
	case KeyEsc:
		return "esc"
	case KeyTab:
		return "tab"
	case KeyBackspace:
		return "backspace"
	case KeyCtrlC:
		return "ctrl+c"
	case KeyPgUp:
		return "pgup"
	case KeyPgDown:
		return "pgdown"
	case KeyHome:
		return "home"
	case KeyEnd:
		return "end"
	case KeySpace:
		return " "
	case KeyRunes:
		return string(k.Runes)
	}
	return ""
}

// WindowSizeMsg is sent when the terminal is resized.
type WindowSizeMsg struct {
	Width  int
	Height int
}

// Msg is a picker event (KeyMsg or WindowSizeMsg).
type Msg any

// --- Model ---

// selectModel is the model for select/multiselect.
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

// Update processes a message and returns the updated model and whether to quit.
func (m selectModel) Update(msg Msg) (selectModel, bool) {
	switch msg := msg.(type) {
	case WindowSizeMsg:
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

	case KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, true

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
			return m, true

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
	return m, false
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
		fmt.Fprintf(&b, "  filter: %s\n\n", m.filter)
	}

	// Count selected for multi-select
	if m.multi && len(m.checked) > 0 {
		fmt.Fprintf(&b, "  %d selected\n\n", len(m.checked))
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
		fmt.Fprintf(&b, "  ↑ %d more\n", m.offset)
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
			fmt.Fprintf(&b, "%s%s %s\n", cursor, check, m.choices[idx])
		} else {
			fmt.Fprintf(&b, "%s%s\n", cursor, m.choices[idx])
		}
	}

	// Show scroll indicator at bottom if more items below
	if end < total {
		fmt.Fprintf(&b, "  ↓ %d more\n", total-end)
	}

	return b.String()
}

// --- Terminal runner ---

// runPicker runs the interactive picker using raw terminal I/O.
func runPicker(m selectModel) (selectModel, error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return m, fmt.Errorf("failed to set raw mode: %w", err)
	}
	defer term.Restore(fd, oldState)

	w := os.Stderr

	// Enter alt screen, hide cursor
	fmt.Fprint(w, "\x1b[?1049h\x1b[?25l")
	defer fmt.Fprint(w, "\x1b[?25h\x1b[?1049l")

	// Get initial terminal size
	width, height, err := term.GetSize(int(w.Fd()))
	if err == nil && height > 0 {
		m, _ = m.Update(WindowSizeMsg{Width: width, Height: height})
	}

	// Handle SIGWINCH for terminal resize
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	// Read keys in a goroutine so we can also handle resize signals
	keyCh := make(chan KeyMsg, 1)
	go func() {
		for {
			k := readKey(os.Stdin)
			keyCh <- k
		}
	}()

	// Initial render
	renderView(w, m.View())

	for {
		select {
		case <-sigCh:
			width, height, err := term.GetSize(int(w.Fd()))
			if err == nil && height > 0 {
				m, _ = m.Update(WindowSizeMsg{Width: width, Height: height})
				renderView(w, m.View())
			}
		case k := <-keyCh:
			if k.Type == KeyUnknown {
				continue
			}
			var quit bool
			m, quit = m.Update(k)
			renderView(w, m.View())
			if quit {
				return m, nil
			}
		}
	}
}

// renderView clears the screen and draws the view.
// In raw mode \n alone moves down without carriage return, so we translate to \r\n.
func renderView(w *os.File, view string) {
	var buf bytes.Buffer
	buf.WriteString("\x1b[H\x1b[2J")
	buf.WriteString(strings.ReplaceAll(view, "\n", "\r\n"))
	w.Write(buf.Bytes())
}

// readKey reads a single key event from the terminal in raw mode.
// Escape sequences are parsed into the appropriate KeyType.
func readKey(f *os.File) KeyMsg {
	buf := make([]byte, 8)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return KeyMsg{Type: KeyUnknown}
	}

	// Single byte
	if n == 1 {
		switch buf[0] {
		case 0x03: // Ctrl+C
			return KeyMsg{Type: KeyCtrlC}
		case 0x0d, 0x0a: // Enter
			return KeyMsg{Type: KeyEnter}
		case 0x1b: // Bare escape (no more bytes followed)
			return KeyMsg{Type: KeyEsc}
		case 0x7f, 0x08: // Backspace
			return KeyMsg{Type: KeyBackspace}
		case 0x09: // Tab
			return KeyMsg{Type: KeyTab}
		case ' ':
			return KeyMsg{Type: KeySpace}
		default:
			if buf[0] >= 0x20 && buf[0] < 0x7f {
				return KeyMsg{Type: KeyRunes, Runes: []rune{rune(buf[0])}}
			}
		}
		return KeyMsg{Type: KeyUnknown}
	}

	// Escape sequences: ESC [ ...
	if buf[0] == 0x1b && n >= 3 && buf[1] == '[' {
		switch buf[2] {
		case 'A':
			return KeyMsg{Type: KeyUp}
		case 'B':
			return KeyMsg{Type: KeyDown}
		case 'H':
			return KeyMsg{Type: KeyHome}
		case 'F':
			return KeyMsg{Type: KeyEnd}
		case '5':
			if n >= 4 && buf[3] == '~' {
				return KeyMsg{Type: KeyPgUp}
			}
		case '6':
			if n >= 4 && buf[3] == '~' {
				return KeyMsg{Type: KeyPgDown}
			}
		case '1':
			// ESC [ 1 ~ = Home (some terminals)
			if n >= 4 && buf[3] == '~' {
				return KeyMsg{Type: KeyHome}
			}
			// ESC [ 1 ; 5 A = Ctrl+Up (ignore)
		case '4':
			// ESC [ 4 ~ = End (some terminals)
			if n >= 4 && buf[3] == '~' {
				return KeyMsg{Type: KeyEnd}
			}
		}
	}

	// ESC O H / ESC O F — Home/End on some terminals
	if buf[0] == 0x1b && n >= 3 && buf[1] == 'O' {
		switch buf[2] {
		case 'H':
			return KeyMsg{Type: KeyHome}
		case 'F':
			return KeyMsg{Type: KeyEnd}
		}
	}

	return KeyMsg{Type: KeyUnknown}
}
