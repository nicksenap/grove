package picker

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"golang.org/x/term"
)

// ErrCancelled is returned when the user cancels a picker with Escape or Ctrl+C.
var ErrCancelled = errors.New("selection cancelled")

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
		return "", ErrCancelled
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
		return nil, ErrCancelled
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

var keyNames = map[KeyType]string{
	KeyUp:        "up",
	KeyDown:      "down",
	KeyEnter:     "enter",
	KeyEsc:       "esc",
	KeyTab:       "tab",
	KeyBackspace: "backspace",
	KeyCtrlC:     "ctrl+c",
	KeyPgUp:      "pgup",
	KeyPgDown:    "pgdown",
	KeyHome:      "home",
	KeyEnd:       "end",
	KeySpace:     " ",
}

// String returns a human-readable name, matching the strings the model switches on.
func (k KeyMsg) String() string {
	if k.Type == KeyRunes {
		return string(k.Runes)
	}
	if name, ok := keyNames[k.Type]; ok {
		return name
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
		m.applyWindowSize(msg)
	case KeyMsg:
		return m.handleKey(msg)
	}
	return m, false
}

func (m selectModel) applyWindowSize(msg WindowSizeMsg) selectModel {
	m.termHeight = msg.Height
	overhead := 6
	if m.filter != "" {
		overhead++
	}
	available := msg.Height - overhead
	if available < 5 {
		available = 5
	}
	m.viewHeight = available
	return m
}

func (m selectModel) handleKey(msg KeyMsg) (selectModel, bool) {
	switch msg.String() {
	case "ctrl+c", "esc":
		m.cancelled = true
		return m, true
	case "up", "k":
		m.moveUp()
	case "down", "j":
		m.moveDown()
	case "pgup":
		m.movePageUp()
	case "pgdown":
		m.movePageDown()
	case "home":
		m.cursor = 0
		m.scrollToCursor()
	case "end":
		m.cursor = len(m.filtered) - 1
		m.scrollToCursor()
	case "tab", " ":
		m.toggleCheck()
	case "enter":
		m.confirm()
		return m, true
	case "backspace":
		m.deleteFilterChar()
	default:
		if len(msg.String()) == 1 {
			m.filter += msg.String()
			m.updateFilter()
		}
	}
	return m, false
}

func (m *selectModel) moveUp() {
	if m.cursor > 0 {
		m.cursor--
		m.scrollToCursor()
	}
}

func (m *selectModel) moveDown() {
	if m.cursor < len(m.filtered)-1 {
		m.cursor++
		m.scrollToCursor()
	}
}

func (m *selectModel) movePageUp() {
	m.cursor -= m.viewHeight
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.scrollToCursor()
}

func (m *selectModel) movePageDown() {
	m.cursor += m.viewHeight
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	m.scrollToCursor()
}

func (m *selectModel) toggleCheck() {
	if m.multi && len(m.filtered) > 0 {
		idx := m.filtered[m.cursor]
		if m.checked[idx] {
			delete(m.checked, idx)
		} else {
			m.checked[idx] = true
		}
	}
}

func (m *selectModel) confirm() {
	if m.multi {
		for idx := range m.checked {
			m.selected = append(m.selected, m.choices[idx])
		}
	} else if len(m.filtered) > 0 {
		m.selected = []string{m.choices[m.filtered[m.cursor]]}
	}
}

func (m *selectModel) deleteFilterChar() {
	if len(m.filter) > 0 {
		runes := []rune(m.filter)
		m.filter = string(runes[:len(runes)-1])
		m.updateFilter()
	}
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
// Uses blocking reads with no goroutines — this avoids goroutine leaks
// that steal keypresses when pickers are called sequentially (e.g. gw go → back → pick dir).
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
	if width, height, err := term.GetSize(int(w.Fd())); err == nil && height > 0 {
		m, _ = m.Update(WindowSizeMsg{Width: width, Height: height})
	}

	renderView(w, m.View())

	for {
		k := readKey(os.Stdin)
		if k.Type == KeyUnknown {
			continue
		}

		// Recheck terminal size on each keypress (catches resize between presses)
		if width, height, err := term.GetSize(int(w.Fd())); err == nil && height > 0 {
			m, _ = m.Update(WindowSizeMsg{Width: width, Height: height})
		}

		var quit bool
		m, quit = m.Update(k)
		renderView(w, m.View())
		if quit {
			return m, nil
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
func readKey(f *os.File) KeyMsg {
	buf := make([]byte, 8)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return KeyMsg{Type: KeyUnknown}
	}

	if n == 1 {
		return parseSingleKey(buf[0])
	}

	if n >= 3 && buf[0] == 0x1b {
		if buf[1] == '[' {
			if k, ok := parseCSI(buf, n); ok {
				return k
			}
		}
		if buf[1] == 'O' {
			return parseSS3(buf[2])
		}
	}

	return KeyMsg{Type: KeyUnknown}
}

func parseSingleKey(b byte) KeyMsg {
	switch b {
	case 0x03:
		return KeyMsg{Type: KeyCtrlC}
	case 0x0d, 0x0a:
		return KeyMsg{Type: KeyEnter}
	case 0x1b:
		return KeyMsg{Type: KeyEsc}
	case 0x7f, 0x08:
		return KeyMsg{Type: KeyBackspace}
	case 0x09:
		return KeyMsg{Type: KeyTab}
	case ' ':
		return KeyMsg{Type: KeySpace}
	default:
		if b >= 0x20 && b < 0x7f {
			return KeyMsg{Type: KeyRunes, Runes: []rune{rune(b)}}
		}
	}
	return KeyMsg{Type: KeyUnknown}
}

var csiSingles = map[byte]KeyType{
	'A': KeyUp,
	'B': KeyDown,
	'H': KeyHome,
	'F': KeyEnd,
}

func parseCSI(buf []byte, n int) (KeyMsg, bool) {
	if t, ok := csiSingles[buf[2]]; ok {
		return KeyMsg{Type: t}, true
	}
	if n >= 4 && buf[3] == '~' {
		return parseCSITilde(buf[2]), true
	}
	return KeyMsg{}, false
}

func parseCSITilde(b byte) KeyMsg {
	switch b {
	case '5':
		return KeyMsg{Type: KeyPgUp}
	case '6':
		return KeyMsg{Type: KeyPgDown}
	case '1':
		return KeyMsg{Type: KeyHome}
	case '4':
		return KeyMsg{Type: KeyEnd}
	}
	return KeyMsg{Type: KeyUnknown}
}

func parseSS3(b byte) KeyMsg {
	switch b {
	case 'H':
		return KeyMsg{Type: KeyHome}
	case 'F':
		return KeyMsg{Type: KeyEnd}
	}
	return KeyMsg{Type: KeyUnknown}
}
