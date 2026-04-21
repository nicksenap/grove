package mcp

import (
	"errors"
	"testing"

	"github.com/nicksenap/grove/internal/config"
)

func TestNormalizeRepoURL(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"ssh with .git", "git@github.com:nicksenap/grove.git", "nicksenap/grove"},
		{"ssh without .git", "git@github.com:nicksenap/grove", "nicksenap/grove"},
		{"https with .git", "https://github.com/nicksenap/grove.git", "nicksenap/grove"},
		{"https without .git", "https://github.com/nicksenap/grove", "nicksenap/grove"},
		{"http with .git", "http://example.com/a/b.git", "a/b"},
		{"already normalized", "nicksenap/grove", "nicksenap/grove"},
		{"trailing .git plain", "nicksenap/grove.git", "nicksenap/grove"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeRepoURL(tt.in); got != tt.want {
				t.Errorf("NormalizeRepoURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func setupTestDB(t *testing.T) func() {
	t.Helper()
	orig := config.GroveDir
	config.GroveDir = t.TempDir()
	return func() { config.GroveDir = orig }
}

func TestInsertAnnouncement_ValidCategories(t *testing.T) {
	defer setupTestDB(t)()
	db, err := OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	for _, cat := range []string{"breaking_change", "status", "warning", "info"} {
		id, err := InsertAnnouncement(db, "ws-1", "git@github.com:owner/repo.git", cat, "hi")
		if err != nil {
			t.Errorf("InsertAnnouncement(%q): %v", cat, err)
		}
		if id == 0 {
			t.Errorf("InsertAnnouncement(%q) returned id 0", cat)
		}
	}
}

func TestInsertAnnouncement_InvalidCategory(t *testing.T) {
	defer setupTestDB(t)()
	db, err := OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	_, err = InsertAnnouncement(db, "ws-1", "owner/repo", "urgent", "boom")
	var icerr *InvalidCategoryError
	if !errors.As(err, &icerr) {
		t.Fatalf("expected *InvalidCategoryError, got %v", err)
	}
	if icerr.Category != "urgent" {
		t.Errorf("Category = %q, want %q", icerr.Category, "urgent")
	}
}

func TestQueryAnnouncements_ExcludesSelf(t *testing.T) {
	defer setupTestDB(t)()
	db, err := OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	// Both workspaces publish to the same repo.
	if _, err := InsertAnnouncement(db, "ws-me", "owner/repo", "info", "mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := InsertAnnouncement(db, "ws-other", "owner/repo", "info", "theirs"); err != nil {
		t.Fatal(err)
	}

	got, err := QueryAnnouncements(db, "owner/repo", "ws-me", "")
	if err != nil {
		t.Fatalf("QueryAnnouncements: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results, want 1 (self excluded)", len(got))
	}
	if got[0].WorkspaceID != "ws-other" {
		t.Errorf("WorkspaceID = %q, want %q", got[0].WorkspaceID, "ws-other")
	}
	if got[0].Message != "theirs" {
		t.Errorf("Message = %q, want %q", got[0].Message, "theirs")
	}
}

func TestQueryAnnouncements_NormalizesRepoURL(t *testing.T) {
	// Insert with SSH form, query with HTTPS form — both normalize to owner/repo.
	defer setupTestDB(t)()
	db, err := OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	if _, err := InsertAnnouncement(db, "ws-a", "git@github.com:owner/repo.git", "info", "hi"); err != nil {
		t.Fatal(err)
	}

	got, err := QueryAnnouncements(db, "https://github.com/owner/repo.git", "ws-b", "")
	if err != nil {
		t.Fatalf("QueryAnnouncements: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d results, want 1 (URL normalization)", len(got))
	}
}

func TestQueryAnnouncements_EmptyReturnsNonNilSlice(t *testing.T) {
	// Callers JSON-marshal the result; nil would become "null", [] is better.
	defer setupTestDB(t)()
	db, err := OpenDB()
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer db.Close()

	got, err := QueryAnnouncements(db, "owner/nonexistent", "ws-a", "")
	if err != nil {
		t.Fatalf("QueryAnnouncements: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
