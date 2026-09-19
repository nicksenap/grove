package workspace

import "time"

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
