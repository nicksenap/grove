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

// BuildHeatmap generates a GitHub-style contribution grid.
// Returns lines of text with ANSI coloring.
func (t *Tracker) BuildHeatmap(events []models.StatsEvent, weeks int) []string {
	counts := activityByDate(events)

	// Find the Monday at the start of our range
	now := t.now()
	start := now.AddDate(0, 0, -7*weeks)
	// Snap to Monday
	for start.Weekday() != time.Monday {
		start = start.AddDate(0, 0, -1)
	}

	// Build grid: 7 rows (Mon-Sun) x N columns (weeks)
	type cell struct {
		date  string
		count int
	}
	grid := make([][]cell, 7)
	for i := range grid {
		grid[i] = make([]cell, 0)
	}

	// Fill grid
	numWeeks := 0
	d := start
	for d.Before(now) || d.Equal(now) {
		weekday := int(d.Weekday())
		if weekday == 0 {
			weekday = 7 // Sunday = 7
		}
		row := weekday - 1 // Monday=0, Sunday=6

		dateStr := d.Format("2006-01-02")
		grid[row] = append(grid[row], cell{date: dateStr, count: counts[dateStr]})

		if row == 0 {
			numWeeks++
		}
		d = d.AddDate(0, 0, 1)
	}

	// Pad all rows to the same number of columns
	maxCols := 0
	for _, row := range grid {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}

	// Find max for intensity scaling
	maxCount := 1
	for _, ev := range counts {
		if ev > maxCount {
			maxCount = ev
		}
	}

	// Intensity levels (brown-to-orange palette)
	levels := []string{
		"\033[2m·\033[0m",        // 0: dim dot
		"\033[38;5;130m█\033[0m", // 1: dark brown
		"\033[38;5;166m█\033[0m", // 2: brown
		"\033[38;5;208m█\033[0m", // 3: orange
		"\033[38;5;214m█\033[0m", // 4: bright orange
	}

	dayLabels := []string{"Mon", "   ", "Wed", "   ", "Fri", "   ", "   "}

	var lines []string

	// Month labels — place at exact column positions, skip if overlapping previous
	monthRow := make([]byte, maxCols)
	for i := range monthRow {
		monthRow[i] = ' '
	}
	prevMonth := ""
	nextFree := 0 // first column available for a new label
	for col := 0; col < maxCols && col < len(grid[0]); col++ {
		mt, _ := time.Parse("2006-01-02", grid[0][col].date)
		month := mt.Format("Jan")
		if month != prevMonth {
			prevMonth = month
			if col >= nextFree && col+len(month) <= maxCols {
				copy(monthRow[col:], month)
				nextFree = col + len(month) + 1 // +1 for gap between labels
			}
		}
	}
	lines = append(lines, "     "+string(monthRow))

	// Data rows — pad to maxCols so all rows are the same width
	for row := 0; row < 7; row++ {
		line := dayLabels[row] + "  "
		for col := 0; col < maxCols; col++ {
			if col >= len(grid[row]) {
				line += levels[0] // pad with dim dot to match grid width
			} else {
				c := grid[row][col]
				if c.count == 0 {
					line += levels[0]
				} else {
					level := c.count * 4 / maxCount
					if level > 4 {
						level = 4
					}
					if level < 1 {
						level = 1
					}
					line += levels[level]
				}
			}
		}
		lines = append(lines, line)
	}

	// Legend
	legend := "     Less "
	for _, l := range levels {
		legend += l
	}
	legend += " More"
	lines = append(lines, legend)

	return lines
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

	// Heatmap
	heatmapLines := t.BuildHeatmap(events, 52)
	fmt.Fprintln(os.Stderr)
	for _, line := range heatmapLines {
		fmt.Fprintln(os.Stderr, line)
	}

	var created, deleted int
	var thisWeek, thisMonth int
	now := t.now()
	weekAgo := now.AddDate(0, 0, -7)
	monthAgo := now.AddDate(0, -1, 0)

	repoUsage := make(map[string]int)
	createTimes := make(map[string]time.Time)
	deleteTimes := make(map[string]time.Time)

	for _, ev := range events {
		ts, _ := time.Parse("2006-01-02T15:04:05.000000", ev.Timestamp)
		if ts.IsZero() {
			ts, _ = time.Parse("2006-01-02T15:04:05", ev.Timestamp)
		}

		switch ev.Event {
		case "workspace_created":
			created++
			if ts.After(weekAgo) {
				thisWeek++
			}
			if ts.After(monthAgo) {
				thisMonth++
			}
			for _, r := range ev.RepoNames {
				repoUsage[r]++
			}
			createTimes[ev.WorkspaceName] = ts
		case "workspace_deleted":
			deleted++
			deleteTimes[ev.WorkspaceName] = ts
		}
	}

	active := created - deleted
	if active < 0 {
		active = 0
	}

	// Avg lifetime
	var totalDuration time.Duration
	var lifetimeCount int
	for name, ct := range createTimes {
		if dt, ok := deleteTimes[name]; ok {
			totalDuration += dt.Sub(ct)
			lifetimeCount++
		}
	}

	avgStr := "n/a"
	if lifetimeCount > 0 {
		avg := totalDuration / time.Duration(lifetimeCount)
		days := int(avg.Hours()) / 24
		hours := int(avg.Hours()) % 24
		avgStr = fmt.Sprintf("%dd %dh", days, hours)
	}

	fmt.Fprintf(os.Stderr, "\n%d created  ·  %d active  ·  %d this week  ·  %d this month  ·  avg lifetime %s\n",
		created, active, thisWeek, thisMonth, avgStr)

	// Top repos
	if len(repoUsage) > 0 {
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

		limit := 5
		if len(sorted) < limit {
			limit = len(sorted)
		}

		fmt.Fprintf(os.Stderr, "\nTop Repos:\n")
		for _, rc := range sorted[:limit] {
			fmt.Fprintf(os.Stderr, "  %-30s %d\n", rc.name, rc.count)
		}
	}

	fmt.Fprintln(os.Stderr)
	return nil
}
