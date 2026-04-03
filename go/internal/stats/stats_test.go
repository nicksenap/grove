package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/models"
)

func testTracker(t *testing.T) *Tracker {
	t.Helper()
	dir := t.TempDir()
	groveDir := filepath.Join(dir, ".grove")
	os.MkdirAll(groveDir, 0o755)
	return &Tracker{
		StatsPath: filepath.Join(groveDir, "stats.json"),
		NowFn:     time.Now,
	}
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func TestLoadEmptyStats(t *testing.T) {
	tr := testTracker(t)

	events, err := tr.loadEvents()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty, got %d", len(events))
	}
}

func TestRecordCreated(t *testing.T) {
	tr := testTracker(t)

	ws := models.Workspace{
		Name:   "test",
		Branch: "feat/test",
		Repos: []models.RepoWorktree{
			{RepoName: "api"},
			{RepoName: "web"},
		},
	}

	if err := tr.RecordCreated(ws); err != nil {
		t.Fatalf("record: %v", err)
	}

	events, _ := tr.loadEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Event != "workspace_created" {
		t.Errorf("event: got %q", events[0].Event)
	}
	if events[0].WorkspaceName != "test" {
		t.Errorf("workspace: got %q", events[0].WorkspaceName)
	}
	if events[0].RepoCount != 2 {
		t.Errorf("repo_count: got %d", events[0].RepoCount)
	}
}

func TestRecordDeleted(t *testing.T) {
	tr := testTracker(t)

	ws := models.Workspace{
		Name:   "test",
		Branch: "main",
		Repos:  []models.RepoWorktree{{RepoName: "api"}},
	}

	tr.RecordCreated(ws)
	if err := tr.RecordDeleted(ws); err != nil {
		t.Fatalf("record: %v", err)
	}

	events, _ := tr.loadEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[1].Event != "workspace_deleted" {
		t.Errorf("event: got %q", events[1].Event)
	}
}

func TestMultipleEventsAppend(t *testing.T) {
	tr := testTracker(t)

	for i := 0; i < 5; i++ {
		ws := models.Workspace{
			Name:   "test",
			Branch: "main",
			Repos:  []models.RepoWorktree{{RepoName: "api"}},
		}
		tr.RecordCreated(ws)
	}

	events, _ := tr.loadEvents()
	if len(events) != 5 {
		t.Errorf("expected 5 events, got %d", len(events))
	}
}

func TestCorruptStatsReturnsEmpty(t *testing.T) {
	tr := testTracker(t)

	os.WriteFile(tr.StatsPath, []byte("{not valid json["), 0o644)

	events, err := tr.loadEvents()
	if err != nil {
		t.Fatalf("should not error on corrupt: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty on corrupt, got %d", len(events))
	}
}

func TestStatsAtomicWrite(t *testing.T) {
	tr := testTracker(t)

	ws := models.Workspace{Name: "test", Branch: "main", Repos: []models.RepoWorktree{{RepoName: "api"}}}
	tr.RecordCreated(ws)

	// No tmp file should remain
	tmp := tr.StatsPath + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up")
	}

	// File should be valid JSON
	data, _ := os.ReadFile(tr.StatsPath)
	var events []models.StatsEvent
	if err := json.Unmarshal(data, &events); err != nil {
		t.Errorf("stats file not valid JSON: %v", err)
	}
}

func TestTimestampFormat(t *testing.T) {
	tr := testTracker(t)

	ws := models.Workspace{Name: "test", Branch: "main", Repos: []models.RepoWorktree{{RepoName: "api"}}}
	tr.RecordCreated(ws)

	events, _ := tr.loadEvents()
	ts := events[0].Timestamp

	_, err := time.Parse("2006-01-02T15:04:05.000000", ts)
	if err != nil {
		_, err = time.Parse("2006-01-02T15:04:05", ts)
		if err != nil {
			t.Errorf("unparseable timestamp %q: %v", ts, err)
		}
	}
}

func TestRecordCreatedUsesInjectedClock(t *testing.T) {
	tr := testTracker(t)
	fixed := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	tr.NowFn = fixedClock(fixed)

	ws := models.Workspace{Name: "test", Branch: "main", Repos: []models.RepoWorktree{{RepoName: "api"}}}
	tr.RecordCreated(ws)

	events, _ := tr.loadEvents()
	if !strings.HasPrefix(events[0].Timestamp, "2025-06-15T12:00:00") {
		t.Errorf("expected fixed timestamp, got %q", events[0].Timestamp)
	}
}

func TestBuildHeatmapEmpty(t *testing.T) {
	tr := testTracker(t)
	lines := tr.BuildHeatmap(nil, 52)
	if len(lines) == 0 {
		t.Error("expected non-empty heatmap even with no data")
	}
}

func TestBuildHeatmapWithData(t *testing.T) {
	tr := testTracker(t)
	// Pin the clock so the test is deterministic
	fixed := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	tr.NowFn = fixedClock(fixed)

	events := []models.StatsEvent{
		{Event: "workspace_created", Timestamp: fixed.Format("2006-01-02T15:04:05")},
		{Event: "workspace_created", Timestamp: fixed.AddDate(0, 0, -1).Format("2006-01-02T15:04:05")},
		{Event: "workspace_created", Timestamp: fixed.AddDate(0, 0, -1).Format("2006-01-02T15:04:05")},
		{Event: "workspace_deleted", Timestamp: fixed.Format("2006-01-02T15:04:05")}, // ignored
	}

	lines := tr.BuildHeatmap(events, 52)
	if len(lines) == 0 {
		t.Error("expected non-empty heatmap")
	}

	combined := ""
	for _, l := range lines {
		combined += l
	}
	if !strings.Contains(combined, "█") {
		t.Error("heatmap should contain filled blocks for active days")
	}
}

func TestBuildHeatmapEventExactly7DaysAgo(t *testing.T) {
	tr := testTracker(t)
	fixed := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC) // a Sunday
	tr.NowFn = fixedClock(fixed)

	weekAgo := fixed.AddDate(0, 0, -7)
	events := []models.StatsEvent{
		{Event: "workspace_created", Timestamp: weekAgo.Format("2006-01-02T15:04:05")},
	}

	lines := tr.BuildHeatmap(events, 2)
	combined := strings.Join(lines, "\n")
	if !strings.Contains(combined, "█") {
		t.Error("event exactly 7 days ago should appear in a 2-week heatmap")
	}
}

func TestActivityByDate(t *testing.T) {
	fixed := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	today := fixed.Format("2006-01-02")
	yesterday := fixed.AddDate(0, 0, -1).Format("2006-01-02")

	events := []models.StatsEvent{
		{Event: "workspace_created", Timestamp: fixed.Format("2006-01-02T15:04:05")},
		{Event: "workspace_created", Timestamp: fixed.Format("2006-01-02T15:04:05")},
		{Event: "workspace_created", Timestamp: fixed.AddDate(0, 0, -1).Format("2006-01-02T15:04:05")},
		{Event: "workspace_deleted", Timestamp: fixed.Format("2006-01-02T15:04:05")}, // ignored
	}

	counts := activityByDate(events)
	if counts[today] != 2 {
		t.Errorf("today: expected 2, got %d", counts[today])
	}
	if counts[yesterday] != 1 {
		t.Errorf("yesterday: expected 1, got %d", counts[yesterday])
	}
}

// stripANSI removes ANSI escape sequences for width testing.
func stripANSI(s string) string {
	result := ""
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
		result += string(r)
	}
	return result
}

func TestHeatmapAlignment(t *testing.T) {
	tr := testTracker(t)
	// Pin to a Wednesday so we get a partial week at the end
	fixed := time.Date(2025, 7, 16, 12, 0, 0, 0, time.UTC)
	tr.NowFn = fixedClock(fixed)

	events := []models.StatsEvent{
		{Event: "workspace_created", Timestamp: fixed.Format("2006-01-02T15:04:05")},
	}

	lines := tr.BuildHeatmap(events, 8)

	// Strip ANSI from all lines and check widths
	stripped := make([]string, len(lines))
	for i, l := range lines {
		stripped[i] = stripANSI(l)
	}

	// Month header (line 0) and data rows (lines 1-7) should have the same
	// prefix width (5 chars: "Mon  " or "     ")
	monthPrefix := stripped[0][:5]
	if monthPrefix != "     " {
		t.Errorf("month line should start with 5-space prefix, got %q", monthPrefix)
	}

	for i := 1; i <= 7 && i < len(stripped); i++ {
		if len(stripped[i]) < 5 {
			t.Fatalf("line %d too short: %q", i, stripped[i])
		}
		// Data portion (after prefix) should be all single-char cells
		data := stripped[i][5:]
		for _, r := range data {
			if r != '·' && r != '█' {
				t.Errorf("line %d has unexpected char %q in grid portion: %q", i, string(r), data)
				break
			}
		}
	}

	// All data rows should have the same visible rune count
	if len(stripped) >= 8 {
		dataWidth := len([]rune(stripANSI(lines[1])))
		for i := 2; i <= 7; i++ {
			w := len([]rune(stripANSI(lines[i])))
			if w != dataWidth {
				t.Errorf("row %d rune width %d != row 1 rune width %d", i, w, dataWidth)
			}
		}
	}
}

func TestHeatmapMonthLabelsNoOverlap(t *testing.T) {
	tr := testTracker(t)
	// A date that spans multiple months in 8 weeks
	fixed := time.Date(2025, 8, 10, 12, 0, 0, 0, time.UTC)
	tr.NowFn = fixedClock(fixed)

	lines := tr.BuildHeatmap(nil, 8)
	monthLine := stripANSI(lines[0])

	// Should contain at least 2 different month labels
	months := 0
	for _, m := range []string{"Jun", "Jul", "Aug"} {
		if strings.Contains(monthLine, m) {
			months++
		}
	}
	if months < 2 {
		t.Errorf("expected at least 2 month labels in %q", monthLine)
	}

	// Month header should not be longer than data rows
	if len(lines) >= 2 {
		monthWidth := len(monthLine)
		dataWidth := len(stripANSI(lines[1]))
		if monthWidth > dataWidth+1 { // +1 tolerance
			t.Errorf("month header (%d) wider than data row (%d): %q", monthWidth, dataWidth, monthLine)
		}
	}
}

func TestRepoNamesRecorded(t *testing.T) {
	tr := testTracker(t)

	ws := models.Workspace{
		Name:   "test",
		Branch: "main",
		Repos: []models.RepoWorktree{
			{RepoName: "api"},
			{RepoName: "web"},
			{RepoName: "worker"},
		},
	}
	tr.RecordCreated(ws)

	events, _ := tr.loadEvents()
	if len(events[0].RepoNames) != 3 {
		t.Errorf("expected 3 repo names, got %d", len(events[0].RepoNames))
	}
}
