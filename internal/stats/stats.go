package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/models"
)

// Tracker records and reports workspace usage statistics.
// Use NewTracker for production; inject a custom NowFn for tests.
type Tracker struct {
	StatsPath string
	NowFn     func() time.Time
}

// NewTracker creates a Tracker using the given grove dir and real clock.
func NewTracker(groveDir string) *Tracker {
	return &Tracker{
		StatsPath: filepath.Join(groveDir, "stats.json"),
		NowFn:     time.Now,
	}
}

func (t *Tracker) now() time.Time {
	return t.NowFn()
}

func (t *Tracker) loadEvents() ([]models.StatsEvent, error) {
	data, err := os.ReadFile(t.StatsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []models.StatsEvent{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return []models.StatsEvent{}, nil
	}
	var events []models.StatsEvent
	if err := json.Unmarshal(data, &events); err != nil {
		return []models.StatsEvent{}, nil // silently reset on corrupt
	}
	return events, nil
}

func (t *Tracker) saveEvents(events []models.StatsEvent) error {
	if err := os.MkdirAll(filepath.Dir(t.StatsPath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return err
	}
	tmp := t.StatsPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, t.StatsPath)
}

// RecordCreated logs a workspace creation event.
func (t *Tracker) RecordCreated(ws models.Workspace) error {
	events, err := t.loadEvents()
	if err != nil {
		return err
	}
	events = append(events, models.StatsEvent{
		Event:         "workspace_created",
		Timestamp:     t.now().Format("2006-01-02T15:04:05.000000"),
		WorkspaceName: ws.Name,
		Branch:        ws.Branch,
		RepoNames:     ws.RepoNames(),
		RepoCount:     len(ws.Repos),
	})
	return t.saveEvents(events)
}

// RecordDeleted logs a workspace deletion event.
func (t *Tracker) RecordDeleted(ws models.Workspace) error {
	events, err := t.loadEvents()
	if err != nil {
		return err
	}
	events = append(events, models.StatsEvent{
		Event:         "workspace_deleted",
		Timestamp:     t.now().Format("2006-01-02T15:04:05.000000"),
		WorkspaceName: ws.Name,
		Branch:        ws.Branch,
		RepoNames:     ws.RepoNames(),
		RepoCount:     len(ws.Repos),
	})
	return t.saveEvents(events)
}

// activityByDate counts workspace_created events per date (YYYY-MM-DD).
func activityByDate(events []models.StatsEvent) map[string]int {
	counts := make(map[string]int)
	for _, ev := range events {
		if ev.Event != "workspace_created" {
			continue
		}
		ts, _ := time.Parse("2006-01-02T15:04:05.000000", ev.Timestamp)
		if ts.IsZero() {
			ts, _ = time.Parse("2006-01-02T15:04:05", ev.Timestamp)
		}
		if !ts.IsZero() {
			counts[ts.Format("2006-01-02")]++
		}
	}
	return counts
}

type heatmapCell struct {
	date  string
	count int
}

type heatmapData struct {
	grid     [][]heatmapCell
	maxCols  int
	maxCount int
	levels   []string
	labels   []string
}

var heatmapLevels = []string{
	"\033[2m·\033[0m",
	"\033[38;5;130m█\033[0m",
	"\033[38;5;166m█\033[0m",
	"\033[38;5;208m█\033[0m",
	"\033[38;5;214m█\033[0m",
}

var heatmapDayLabels = []string{"Mon", "   ", "Wed", "   ", "Fri", "   ", "   "}

// BuildHeatmap generates a GitHub-style contribution grid.
func (t *Tracker) BuildHeatmap(events []models.StatsEvent, weeks int) []string {
	counts := activityByDate(events)
	grid, maxCols := t.buildHeatmapGrid(counts, weeks)
	maxCount := maxCountValue(counts)
	data := heatmapData{
		grid:     grid,
		maxCols:  maxCols,
		maxCount: maxCount,
		levels:   heatmapLevels,
		labels:   heatmapDayLabels,
	}

	lines := []string{"     " + data.renderMonthRow()}
	lines = append(lines, data.renderDataRows()...)
	lines = append(lines, data.renderLegend())
	return lines
}

func (t *Tracker) buildHeatmapGrid(counts map[string]int, weeks int) ([][]heatmapCell, int) {
	now := t.now()
	start := now.AddDate(0, 0, -7*weeks)
	for start.Weekday() != time.Monday {
		start = start.AddDate(0, 0, -1)
	}

	grid := make([][]heatmapCell, 7)
	for i := range grid {
		grid[i] = make([]heatmapCell, 0)
	}

	d := start
	for d.Before(now) || d.Equal(now) {
		wd := int(d.Weekday())
		if wd == 0 {
			wd = 7
		}
		row := wd - 1
		dateStr := d.Format("2006-01-02")
		grid[row] = append(grid[row], heatmapCell{date: dateStr, count: counts[dateStr]})
		d = d.AddDate(0, 0, 1)
	}

	maxCols := 0
	for _, row := range grid {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}
	return grid, maxCols
}

func maxCountValue(counts map[string]int) int {
	maxCount := 1
	for _, v := range counts {
		if v > maxCount {
			maxCount = v
		}
	}
	return maxCount
}

func (d *heatmapData) renderMonthRow() string {
	row := make([]byte, d.maxCols)
	for i := range row {
		row[i] = ' '
	}
	prevMonth := ""
	nextFree := 0
	for col := 0; col < d.maxCols && col < len(d.grid[0]); col++ {
		mt, _ := time.Parse("2006-01-02", d.grid[0][col].date)
		month := mt.Format("Jan")
		if month != prevMonth {
			prevMonth = month
			if col >= nextFree && col+len(month) <= d.maxCols {
				copy(row[col:], month)
				nextFree = col + len(month) + 1
			}
		}
	}
	return string(row)
}

func (d *heatmapData) renderDataRows() []string {
	var lines []string
	for row := 0; row < 7; row++ {
		line := d.labels[row] + "  "
		for col := 0; col < d.maxCols; col++ {
			line += d.cellGlyph(row, col)
		}
		lines = append(lines, line)
	}
	return lines
}

func (d *heatmapData) cellGlyph(row, col int) string {
	if col >= len(d.grid[row]) {
		return d.levels[0]
	}
	c := d.grid[row][col]
	if c.count == 0 {
		return d.levels[0]
	}
	level := c.count * 4 / d.maxCount
	level = max(level, 1)
	level = min(level, 4)
	return d.levels[level]
}

func (d *heatmapData) renderLegend() string {
	legend := "     Less "
	for _, l := range d.levels {
		legend += l
	}
	return legend + " More"
}

type statsSummary struct {
	created     int
	deleted     int
	thisWeek    int
	thisMonth   int
	repoUsage   map[string]int
	avgLifetime string
}

func (t *Tracker) aggregateStats(events []models.StatsEvent) statsSummary {
	now := t.now()
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, -1, 0)

	var s statsSummary
	s.repoUsage = make(map[string]int)
	createTimes := make(map[string]time.Time)
	deleteTimes := make(map[string]time.Time)

	for _, ev := range events {
		ts := parseTimestamp(ev.Timestamp)

		switch ev.Event {
		case "workspace_created":
			s.created++
			if ts.After(weekAgo) {
				s.thisWeek++
			}
			if ts.After(monthAgo) {
				s.thisMonth++
			}
			for _, r := range ev.RepoNames {
				s.repoUsage[r]++
			}
			createTimes[ev.WorkspaceName] = ts
		case "workspace_deleted":
			s.deleted++
			deleteTimes[ev.WorkspaceName] = ts
		}
	}

	var totalDuration time.Duration
	var lifetimeCount int
	for name, ct := range createTimes {
		if dt, ok := deleteTimes[name]; ok {
			totalDuration += dt.Sub(ct)
			lifetimeCount++
		}
	}

	if lifetimeCount > 0 {
		avg := totalDuration / time.Duration(lifetimeCount)
		days := int(avg.Hours()) / 24
		hours := int(avg.Hours()) % 24
		s.avgLifetime = fmt.Sprintf("%dd %dh", days, hours)
	} else {
		s.avgLifetime = "n/a"
	}

	return s
}

func parseTimestamp(s string) time.Time {
	ts, _ := time.Parse("2006-01-02T15:04:05.000000", s)
	if ts.IsZero() {
		ts, _ = time.Parse("2006-01-02T15:04:05", s)
	}
	return ts
}

// PrintStats outputs workspace usage statistics.
func (t *Tracker) PrintStats() error {
	events, err := t.loadEvents()
	if err != nil {
		return err
	}

	if len(events) == 0 {
		fmt.Fprintf(os.Stderr, "No stats yet — create a workspace to start tracking.\n")
		return nil
	}

	heatmapLines := t.BuildHeatmap(events, 52)
	fmt.Fprintln(os.Stderr)
	for _, line := range heatmapLines {
		fmt.Fprintln(os.Stderr, line)
	}

	s := t.aggregateStats(events)
	active := s.created - s.deleted
	if active < 0 {
		active = 0
	}

	fmt.Fprintf(os.Stderr, "\n%d created  ·  %d active  ·  %d this week  ·  %d this month  ·  avg lifetime %s\n",
		s.created, active, s.thisWeek, s.thisMonth, s.avgLifetime)

	t.printTopRepos(s.repoUsage)
	fmt.Fprintln(os.Stderr)
	return nil
}

func (t *Tracker) printTopRepos(repoUsage map[string]int) {
	if len(repoUsage) == 0 {
		return
	}
	type repoCount struct {
		name  string
		count int
	}
	var sorted []repoCount
	for name, count := range repoUsage {
		sorted = append(sorted, repoCount{name, count})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].count > sorted[j].count
	})

	limit := min(5, len(sorted))
	fmt.Fprintf(os.Stderr, "\nTop Repos:\n")
	for _, rc := range sorted[:limit] {
		fmt.Fprintf(os.Stderr, "  %-30s %d\n", rc.name, rc.count)
	}
}
