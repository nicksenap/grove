package workspace

import (
	"time"

	"github.com/nicksenap/grove/internal/models"
)

const createdAtLayout = "2006-01-02T15:04:05.000000"

// ParseCreatedAt parses a workspace created_at timestamp in loc so it matches
// the local wall clock written by models.NewWorkspace.
func ParseCreatedAt(value string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.UTC
	}
	if ts, err := time.ParseInLocation(createdAtLayout, value, loc); err == nil {
		return ts, nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05", value, loc)
}

// OlderThan returns workspaces whose created_at is at least minAge before now.
// Workspaces with missing or unparseable created_at are skipped.
func OlderThan(workspaces []models.Workspace, minAge time.Duration, now time.Time) []models.Workspace {
	var out []models.Workspace
	for _, ws := range workspaces {
		created, err := ParseCreatedAt(ws.CreatedAt, now.Location())
		if err != nil {
			continue
		}
		if !created.After(now.Add(-minAge)) {
			out = append(out, ws)
		}
	}
	return out
}
