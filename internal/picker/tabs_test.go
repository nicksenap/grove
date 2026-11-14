package picker

import (
	"reflect"
	"strings"
	"testing"
)

func tabUpdate(m tabModel, msgs ...Msg) (tabModel, bool) {
	quit := false
	for _, msg := range msgs {
		m, quit = m.Update(msg)
	}
	return m, quit
}

func testTabs() []Tab {
	return []Tab{
		{Title: "Presets", Choices: []string{"backend", "frontend"}},
		{Title: "Latest", Choices: []string{"web", "api"}, Multi: true},
		{Title: "All repos", Choices: []string{"api", "docs", "web"}, Multi: true},
	}
}

func TestTabsArrowKeysSwitchAndWrap(t *testing.T) {
	m, _ := tabUpdate(newTabModel("Pick:", testTabs()), key(KeyRight))
	if m.active != 1 {
		t.Fatalf("right: active = %d, want 1", m.active)
	}
	if !strings.Contains(m.View(), "[Latest]") {
		t.Errorf("view should mark the active tab, got:\n%s", m.View())
	}
	m, _ = tabUpdate(m, key(KeyLeft), key(KeyLeft))
	if m.active != 2 {
		t.Errorf("left twice from 1: active = %d, want 2 (wraps)", m.active)
	}
}

func TestTabsSingleSelectReturnsHighlightedRow(t *testing.T) {
	m, quit := tabUpdate(newTabModel("Pick:", testTabs()), key(KeyDown), key(KeyEnter))
	if !quit || m.active != 0 || !reflect.DeepEqual(m.selected, []string{"frontend"}) {
		t.Errorf("quit=%v active=%d selected=%v, want frontend on tab 0", quit, m.active, m.selected)
	}
}

func TestTabsChecksAreSharedBetweenMultiTabs(t *testing.T) {
	// Check "web" on Latest, then "docs" on All repos.
	m, _ := tabUpdate(newTabModel("Pick:", testTabs()), key(KeyRight), key(KeySpace), key(KeyRight))
	if !m.tabs[2].checked[2] {
		t.Fatalf("web checked on Latest should show as checked on All repos")
	}
	m, quit := tabUpdate(m, key(KeyDown), key(KeySpace), key(KeyEnter))
	if !quit || !reflect.DeepEqual(m.selected, []string{"docs", "web"}) {
		t.Errorf("quit=%v selected=%v, want [docs web]", quit, m.selected)
	}
}

func TestTabsMultiEnterWithoutChecksPicksHighlightedRow(t *testing.T) {
	m, quit := tabUpdate(newTabModel("Pick:", testTabs()), key(KeyRight), key(KeyDown), key(KeyEnter))
	if !quit || !reflect.DeepEqual(m.selected, []string{"api"}) {
		t.Errorf("quit=%v selected=%v, want [api]", quit, m.selected)
	}
}

func TestTabsEnterWithNoMatchKeepsPickerOpen(t *testing.T) {
	m, quit := tabUpdate(newTabModel("Pick:", testTabs()), rune_('/'), rune_('z'), key(KeyEnter), key(KeyEnter))
	if quit || m.cancelled {
		t.Errorf("enter with no matching row should not quit (quit=%v cancelled=%v)", quit, m.cancelled)
	}
}

func TestTabsEscCancels(t *testing.T) {
	m, quit := tabUpdate(newTabModel("Pick:", testTabs()), key(KeyEsc))
	if !quit || !m.cancelled {
		t.Errorf("esc should cancel (quit=%v cancelled=%v)", quit, m.cancelled)
	}
}

func TestTabsHLSwitchTabsExceptWhileSearching(t *testing.T) {
	m, _ := tabUpdate(newTabModel("Pick:", testTabs()), rune_('l'))
	if m.active != 1 {
		t.Fatalf("l: active = %d, want 1", m.active)
	}
	m, _ = tabUpdate(m, rune_('h'), rune_('/'), rune_('l'))
	if m.active != 0 || m.tabs[0].filter != "l" {
		t.Errorf("l while searching: active = %d filter = %q, want tab 0 and filter l", m.active, m.tabs[0].filter)
	}
}
