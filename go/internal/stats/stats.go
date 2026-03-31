package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/models"
)

func statsPath() string {
	return filepath.Join(config.GroveDir, "stats.json")
}

func loadEvents() ([]models.StatsEvent, error) {
	data, err := os.ReadFile(statsPath())
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

func saveEvents(events []models.StatsEvent) error {
	path := statsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RecordCreated logs a workspace creation event.
func RecordCreated(ws models.Workspace) error {
	events, err := loadEvents()
	if err != nil {
		return err
	}
	events = append(events, models.StatsEvent{
		Event:         "workspace_created",
		Timestamp:     time.Now().Format("2006-01-02T15:04:05.000000"),
		WorkspaceName: ws.Name,
		Branch:        ws.Branch,
		RepoNames:     ws.RepoNames(),
		RepoCount:     len(ws.Repos),
	})
	return saveEvents(events)
}

// RecordDeleted logs a workspace deletion event.
func RecordDeleted(ws models.Workspace) error {
	events, err := loadEvents()
	if err != nil {
		return err
	}
	events = append(events, models.StatsEvent{
		Event:         "workspace_deleted",
		Timestamp:     time.Now().Format("2006-01-02T15:04:05.000000"),
		WorkspaceName: ws.Name,
		Branch:        ws.Branch,
		RepoNames:     ws.RepoNames(),
		RepoCount:     len(ws.Repos),
	})
	return saveEvents(events)
}

// PrintStats outputs workspace usage statistics.
func PrintStats() error {
	events, err := loadEvents()
	if err != nil {
		return err
	}

	if len(events) == 0 {
		fmt.Fprintf(os.Stderr, "No stats yet — create a workspace to start tracking.\n")
		return nil
	}

	var created, deleted int
	var thisWeek, thisMonth int
	now := time.Now()
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
