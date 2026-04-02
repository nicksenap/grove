package stats

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/models"
)

func setupTestEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	config.GroveDir = filepath.Join(dir, ".grove")
	os.MkdirAll(config.GroveDir, 0o755)
}

func TestLoadEmptyStats(t *testing.T) {
	setupTestEnv(t)

	events, err := loadEvents()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty, got %d", len(events))
	}
}

func TestRecordCreated(t *testing.T) {
	setupTestEnv(t)

	ws := models.Workspace{
		Name:   "test",
		Branch: "feat/test",
		Repos: []models.RepoWorktree{
			{RepoName: "api"},
			{RepoName: "web"},
		},
	}

	if err := RecordCreated(ws); err != nil {
		t.Fatalf("record: %v", err)
	}

	events, _ := loadEvents()
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
	setupTestEnv(t)

	ws := models.Workspace{
		Name:   "test",
		Branch: "main",
		Repos:  []models.RepoWorktree{{RepoName: "api"}},
	}

	RecordCreated(ws)
	if err := RecordDeleted(ws); err != nil {
		t.Fatalf("record: %v", err)
	}

	events, _ := loadEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[1].Event != "workspace_deleted" {
		t.Errorf("event: got %q", events[1].Event)
	}
}

func TestMultipleEventsAppend(t *testing.T) {
	setupTestEnv(t)

	for i := 0; i < 5; i++ {
		ws := models.Workspace{
			Name:   "test",
			Branch: "main",
			Repos:  []models.RepoWorktree{{RepoName: "api"}},
		}
		RecordCreated(ws)
	}

	events, _ := loadEvents()
	if len(events) != 5 {
		t.Errorf("expected 5 events, got %d", len(events))
	}
}

func TestCorruptStatsReturnsEmpty(t *testing.T) {
	setupTestEnv(t)

	os.WriteFile(statsPath(), []byte("{not valid json["), 0o644)

	events, err := loadEvents()
	if err != nil {
		t.Fatalf("should not error on corrupt: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty on corrupt, got %d", len(events))
	}
}

func TestStatsAtomicWrite(t *testing.T) {
	setupTestEnv(t)

	ws := models.Workspace{Name: "test", Branch: "main", Repos: []models.RepoWorktree{{RepoName: "api"}}}
	RecordCreated(ws)

	// No tmp file should remain
	tmp := statsPath() + ".tmp"
	if _, err := os.Stat(tmp); !os.IsNotExist(err) {
		t.Error("temp file should be cleaned up")
	}

	// File should be valid JSON
	data, _ := os.ReadFile(statsPath())
	var events []models.StatsEvent
	if err := json.Unmarshal(data, &events); err != nil {
		t.Errorf("stats file not valid JSON: %v", err)
	}
}

func TestTimestampFormat(t *testing.T) {
	setupTestEnv(t)

	ws := models.Workspace{Name: "test", Branch: "main", Repos: []models.RepoWorktree{{RepoName: "api"}}}
	RecordCreated(ws)

	events, _ := loadEvents()
	ts := events[0].Timestamp

	// Should be parseable
	_, err := time.Parse("2006-01-02T15:04:05.000000", ts)
	if err != nil {
		// Try without microseconds
		_, err = time.Parse("2006-01-02T15:04:05", ts)
		if err != nil {
			t.Errorf("unparseable timestamp %q: %v", ts, err)
		}
	}
}

func TestBuildHeatmapEmpty(t *testing.T) {
	lines := BuildHeatmap(nil, 52)
	if len(lines) == 0 {
		t.Error("expected non-empty heatmap even with no data")
	}
	// Should have day labels + month row + 7 data rows + legend
}

func TestBuildHeatmapWithData(t *testing.T) {
	// Create events over the last few days
	now := time.Now()
	events := []models.StatsEvent{
		{Event: "workspace_created", Timestamp: now.Format("2006-01-02T15:04:05")},
		{Event: "workspace_created", Timestamp: now.AddDate(0, 0, -1).Format("2006-01-02T15:04:05")},
		{Event: "workspace_created", Timestamp: now.AddDate(0, 0, -1).Format("2006-01-02T15:04:05")},
		{Event: "workspace_deleted", Timestamp: now.Format("2006-01-02T15:04:05")}, // should be ignored
	}

	lines := BuildHeatmap(events, 52)
	if len(lines) == 0 {
		t.Error("expected non-empty heatmap")
	}

	// Should contain at least one filled block (█)
	combined := ""
	for _, l := range lines {
		combined += l
	}
	if !strings.Contains(combined, "█") {
		t.Error("heatmap should contain filled blocks for active days")
	}
}

func TestActivityByDate(t *testing.T) {
	now := time.Now()
	today := now.Format("2006-01-02")
	yesterday := now.AddDate(0, 0, -1).Format("2006-01-02")

	events := []models.StatsEvent{
		{Event: "workspace_created", Timestamp: now.Format("2006-01-02T15:04:05")},
		{Event: "workspace_created", Timestamp: now.Format("2006-01-02T15:04:05")},
		{Event: "workspace_created", Timestamp: now.AddDate(0, 0, -1).Format("2006-01-02T15:04:05")},
		{Event: "workspace_deleted", Timestamp: now.Format("2006-01-02T15:04:05")}, // ignored
	}

	counts := activityByDate(events)
	if counts[today] != 2 {
		t.Errorf("today: expected 2, got %d", counts[today])
	}
	if counts[yesterday] != 1 {
		t.Errorf("yesterday: expected 1, got %d", counts[yesterday])
	}
}

func TestRepoNamesRecorded(t *testing.T) {
	setupTestEnv(t)

	ws := models.Workspace{
		Name:   "test",
		Branch: "main",
		Repos: []models.RepoWorktree{
			{RepoName: "api"},
			{RepoName: "web"},
			{RepoName: "worker"},
		},
	}
	RecordCreated(ws)

	events, _ := loadEvents()
	if len(events[0].RepoNames) != 3 {
		t.Errorf("expected 3 repo names, got %d", len(events[0].RepoNames))
	}
}
