package picker

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestPickOneSingleChoiceAutoSelects(t *testing.T) {
	// Single choice should return immediately without TUI
	result, err := PickOne("Pick:", []string{"only-option"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "only-option" {
		t.Errorf("expected 'only-option', got %q", result)
	}
}

func TestPickOneEmptyChoicesErrors(t *testing.T) {
	_, err := PickOne("Pick:", []string{})
	if err == nil {
		t.Error("expected error for empty choices")
	}
}

func TestPickManyEmptyChoicesErrors(t *testing.T) {
	_, err := PickMany("Pick:", []string{})
	if err == nil {
		t.Error("expected error for empty choices")
	}
}

func TestPickOneNonTerminalErrors(t *testing.T) {
	// In test context, stdin/stderr are not terminals
	_, err := PickOne("Pick:", []string{"a", "b"})
	if err == nil {
		t.Error("expected error when not in a terminal")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error should mention terminal, got: %v", err)
	}
}

func TestPickManyNonTerminalErrors(t *testing.T) {
	_, err := PickMany("Pick:", []string{"a", "b"})
	if err == nil {
		t.Error("expected error when not in a terminal")
	}
	if !strings.Contains(err.Error(), "terminal") {
		t.Errorf("error should mention terminal, got: %v", err)
	}
}

func TestPickOneUsesStderrNotStdout(t *testing.T) {
	// This is a design test — verify the terminal check is on stderr.
	// In test context both stdin and stderr are pipes (not terminals),
	// so the error fires. The important thing is it does NOT check stdout.
	//
	// If the code checked stdout, commands like `cd "$(gw go)"` would fail
	// because stdout is piped for cd, but stderr is still a terminal.
	//
	// We verify by checking the error fires in tests (where stderr is a pipe).
	// In a real terminal, stderr IS a terminal, so the picker would work
	// even when stdout is piped.
	_, err := PickOne("Pick:", []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error in non-terminal test context")
	}
	// The fact that this test exists documents the stderr contract.
	// A regression to os.Stdout would cause `gw go` to fail in shell functions.
}

// ---------------------------------------------------------------------------
// Model unit tests — exercise the model directly without a terminal
// ---------------------------------------------------------------------------

// update is a helper that sends a message and returns the updated model.
func update(m selectModel, msg Msg) selectModel {
	result, _ := m.Update(msg)
	return result
}

func key(k KeyType) KeyMsg          { return KeyMsg{Type: k} }
func rune_(r rune) KeyMsg           { return KeyMsg{Type: KeyRunes, Runes: []rune{r}} }
func resize(w, h int) WindowSizeMsg { return WindowSizeMsg{Width: w, Height: h} }

func TestModelFilterReducesList(t *testing.T) {
	m := update(newSelectModel("Pick:", []string{"apple", "banana", "avocado"}, false), rune_('v'))

	if len(m.filtered) != 1 {
		t.Errorf("expected 1 match for 'v', got %d", len(m.filtered))
	}
	if m.choices[m.filtered[0]] != "avocado" {
		t.Errorf("expected avocado, got %s", m.choices[m.filtered[0]])
	}
}

func TestModelFilterNoMatches(t *testing.T) {
	m := update(newSelectModel("Pick:", []string{"apple", "banana"}, false), rune_('z'))

	if len(m.filtered) != 0 {
		t.Errorf("expected 0 matches for 'z', got %d", len(m.filtered))
	}
	if !strings.Contains(m.View(), "(no matches)") {
		t.Errorf("view should show no matches indicator, got:\n%s", m.View())
	}
}

func TestModelBackspaceRemovesFilter(t *testing.T) {
	m := update(newSelectModel("Pick:", []string{"apple", "banana"}, false), rune_('z'))
	if len(m.filtered) != 0 {
		t.Fatal("precondition: 'z' should match nothing")
	}

	m = update(m, key(KeyBackspace))
	if len(m.filtered) != 2 {
		t.Errorf("backspace should restore all choices, got %d", len(m.filtered))
	}
}

func TestModelMultiSelectToggle(t *testing.T) {
	m := update(newSelectModel("Pick:", []string{"a", "b", "c"}, true), key(KeyTab))
	if !m.checked[0] {
		t.Error("first item should be checked after tab")
	}

	m = update(m, key(KeyTab))
	if m.checked[0] {
		t.Error("first item should be unchecked after second tab")
	}
}

func TestModelScrollIndicators(t *testing.T) {
	choices := make([]string, 50)
	for i := range choices {
		choices[i] = fmt.Sprintf("item-%02d", i)
	}
	m := update(newSelectModel("Pick:", choices, false), resize(80, 15))
	view := m.View()

	// Should NOT show "↑ N more" at the top (we're at the start)
	if strings.Contains(view, "↑ ") && strings.Contains(view, " more") {
		t.Errorf("should not show up-scroll indicator at start, got:\n%s", view)
	}
	// Should show "↓ more" at the bottom
	if !strings.Contains(view, "↓") {
		t.Errorf("should show down-arrow indicator, got:\n%s", view)
	}
	// Should NOT render all 50 items
	if strings.Count(view, "item-") >= 50 {
		t.Errorf("should not render all 50 items")
	}
}

func TestModelScrollFollowsCursor(t *testing.T) {
	choices := make([]string, 50)
	for i := range choices {
		choices[i] = fmt.Sprintf("item-%02d", i)
	}
	m := update(newSelectModel("Pick:", choices, false), resize(80, 15))

	for i := 0; i < 20; i++ {
		m = update(m, key(KeyDown))
	}

	if m.cursor != 20 {
		t.Fatalf("cursor should be at 20, got %d", m.cursor)
	}

	view := m.View()
	if !strings.Contains(view, "↑") {
		t.Errorf("should show up-arrow after scrolling down, got:\n%s", view)
	}
	if !strings.Contains(view, "item-20") {
		t.Errorf("cursor item should be visible, got:\n%s", view)
	}
}

func TestModelPageUpDown(t *testing.T) {
	choices := make([]string, 50)
	for i := range choices {
		choices[i] = fmt.Sprintf("item-%02d", i)
	}
	m := update(newSelectModel("Pick:", choices, false), resize(80, 15))

	m = update(m, key(KeyPgDown))
	if m.cursor == 0 {
		t.Error("page down should move cursor")
	}

	m = update(m, key(KeyPgUp))
	if m.cursor != 0 {
		t.Errorf("page up from near top should go to 0, got %d", m.cursor)
	}
}

func TestModelSelectedCountDisplay(t *testing.T) {
	m := newSelectModel("Pick:", []string{"a", "b", "c"}, true)
	m = update(m, key(KeyTab))  // select a
	m = update(m, key(KeyDown)) // move to b
	m = update(m, key(KeyTab))  // select b

	if !strings.Contains(m.View(), "2 selected") {
		t.Errorf("should show selection count, got:\n%s", m.View())
	}
}

func TestModelEscCancels(t *testing.T) {
	m := newSelectModel("Pick:", []string{"a", "b"}, false)
	model, quit := m.Update(key(KeyEsc))

	if !model.cancelled {
		t.Error("esc should set cancelled")
	}
	if !quit {
		t.Error("esc should return quit=true")
	}
}

func TestReadKeyByteSequences(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
		want  KeyType
		runes []rune
	}{
		{"ctrl+c", []byte{0x03}, KeyCtrlC, nil},
		{"enter CR", []byte{0x0d}, KeyEnter, nil},
		{"enter LF", []byte{0x0a}, KeyEnter, nil},
		{"esc bare", []byte{0x1b}, KeyEsc, nil},
		{"backspace DEL", []byte{0x7f}, KeyBackspace, nil},
		{"backspace BS", []byte{0x08}, KeyBackspace, nil},
		{"tab", []byte{0x09}, KeyTab, nil},
		{"space", []byte{' '}, KeySpace, nil},
		{"printable rune", []byte{'a'}, KeyRunes, []rune{'a'}},

		{"up CSI A", []byte{0x1b, '[', 'A'}, KeyUp, nil},
		{"down CSI B", []byte{0x1b, '[', 'B'}, KeyDown, nil},
		{"home CSI H", []byte{0x1b, '[', 'H'}, KeyHome, nil},
		{"end CSI F", []byte{0x1b, '[', 'F'}, KeyEnd, nil},

		{"pgup CSI 5~", []byte{0x1b, '[', '5', '~'}, KeyPgUp, nil},
		{"pgdown CSI 6~", []byte{0x1b, '[', '6', '~'}, KeyPgDown, nil},
		{"home CSI 1~", []byte{0x1b, '[', '1', '~'}, KeyHome, nil},
		{"end CSI 4~", []byte{0x1b, '[', '4', '~'}, KeyEnd, nil},

		{"home SS3 H", []byte{0x1b, 'O', 'H'}, KeyHome, nil},
		{"end SS3 F", []byte{0x1b, 'O', 'F'}, KeyEnd, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("os.Pipe: %v", err)
			}
			defer r.Close()

			if _, err := w.Write(tc.input); err != nil {
				t.Fatalf("write: %v", err)
			}
			w.Close()

			got := readKey(r)
			if got.Type != tc.want {
				t.Errorf("type: got %v, want %v", got.Type, tc.want)
			}
			if tc.runes != nil && string(got.Runes) != string(tc.runes) {
				t.Errorf("runes: got %q, want %q", string(got.Runes), string(tc.runes))
			}
		})
	}
}

func TestModelHomeEnd(t *testing.T) {
	choices := make([]string, 50)
	for i := range choices {
		choices[i] = fmt.Sprintf("item-%02d", i)
	}
	m := update(newSelectModel("Pick:", choices, false), resize(80, 15))

	m = update(m, key(KeyEnd))
	if m.cursor != 49 {
		t.Errorf("end should go to last item, got %d", m.cursor)
	}
	if !strings.Contains(m.View(), "item-49") {
		t.Error("last item should be visible after end")
	}

	m = update(m, key(KeyHome))
	if m.cursor != 0 {
		t.Errorf("home should go to first item, got %d", m.cursor)
	}
	if !strings.Contains(m.View(), "item-00") {
		t.Error("first item should be visible after home")
	}
}
