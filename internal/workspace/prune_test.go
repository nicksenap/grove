package workspace

import (
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/models"
)

func TestOlderThanIncludesWorkspacesAtLeastMinAge(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	old := models.Workspace{Name: "old", CreatedAt: now.Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05.000000")}
	fresh := models.Workspace{Name: "fresh", CreatedAt: now.Add(-6 * 24 * time.Hour).Format("2006-01-02T15:04:05.000000")}

	got := OlderThan([]models.Workspace{old, fresh}, 7*24*time.Hour, now)
	if len(got) != 1 || got[0].Name != "old" {
		t.Fatalf("got %+v, want only old", got)
	}
}

func TestOlderThanIncludesExactBoundary(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	ws := models.Workspace{Name: "boundary", CreatedAt: now.Add(-7 * 24 * time.Hour).Format("2006-01-02T15:04:05.000000")}

	got := OlderThan([]models.Workspace{ws}, 7*24*time.Hour, now)
	if len(got) != 1 {
		t.Fatalf("workspace exactly min-age should be included, got %+v", got)
	}
}

func TestOlderThanSkipsUnparseableCreatedAt(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	got := OlderThan([]models.Workspace{
		{Name: "empty", CreatedAt: ""},
		{Name: "bad", CreatedAt: "not-a-date"},
	}, time.Hour, now)
	if len(got) != 0 {
		t.Fatalf("unparseable created_at should be skipped, got %+v", got)
	}
}

func TestParseCreatedAtUsesLocation(t *testing.T) {
	loc := time.FixedZone("test", -7*3600)
	ts, err := ParseCreatedAt("2026-04-10T12:00:00.000000", loc)
	if err != nil {
		t.Fatal(err)
	}
	if ts.Location() != loc {
		t.Fatalf("location = %v, want %v", ts.Location(), loc)
	}
	if ts.Hour() != 12 {
		t.Fatalf("hour = %d, want 12 (wall clock in given location)", ts.Hour())
	}
}

func TestOlderThanPreservesInputOrder(t *testing.T) {
	now := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	first := models.Workspace{Name: "first", CreatedAt: now.Add(-10 * 24 * time.Hour).Format("2006-01-02T15:04:05.000000")}
	second := models.Workspace{Name: "second", CreatedAt: now.Add(-8 * 24 * time.Hour).Format("2006-01-02T15:04:05.000000")}

	got := OlderThan([]models.Workspace{first, second}, 7*24*time.Hour, now)
	if len(got) != 2 || got[0].Name != "first" || got[1].Name != "second" {
		t.Fatalf("got %+v", got)
	}
}
