package mcp

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	_ "modernc.org/sqlite"
)

func dbPath() string {
	return filepath.Join(config.GroveDir, "messages.db")
}

// OpenDB opens the SQLite database, creates tables, and prunes old entries.
func OpenDB() (*sql.DB, error) {
	os.MkdirAll(config.GroveDir, 0o755)
	db, err := sql.Open("sqlite", dbPath())
	if err != nil {
		return nil, err
	}

	// WAL mode for concurrent access
	db.Exec("PRAGMA journal_mode=WAL")

	// Create table
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS announcements (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			workspace_id TEXT NOT NULL,
			repo_url TEXT NOT NULL,
			category TEXT NOT NULL,
			message TEXT NOT NULL,
			created_at TEXT DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_repo_created ON announcements(repo_url, created_at);
	`)
	if err != nil {
		db.Close()
		return nil, err
	}

	// Prune entries older than 30 days
	db.Exec("DELETE FROM announcements WHERE created_at < datetime('now', '-30 days')")

	return db, nil
}

var validCategories = map[string]bool{
	"breaking_change": true,
	"status":          true,
	"warning":         true,
	"info":            true,
}

// InsertAnnouncement inserts a new announcement.
func InsertAnnouncement(db *sql.DB, workspaceID, repoURL, category, message string) (int64, error) {
	if !validCategories[category] {
		return 0, &InvalidCategoryError{category}
	}
	repoURL = NormalizeRepoURL(repoURL)

	result, err := db.Exec(
		"INSERT INTO announcements (workspace_id, repo_url, category, message) VALUES (?, ?, ?, ?)",
		workspaceID, repoURL, category, message,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// Announcement is a stored announcement record.
type Announcement struct {
	ID          int64  `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	RepoURL     string `json:"repo_url"`
	Category    string `json:"category"`
	Message     string `json:"message"`
	CreatedAt   string `json:"created_at"`
}

// QueryAnnouncements returns announcements for a repo, excluding the given workspace.
func QueryAnnouncements(db *sql.DB, repoURL, excludeWorkspace, since string) ([]Announcement, error) {
	repoURL = NormalizeRepoURL(repoURL)

	query := "SELECT id, workspace_id, repo_url, category, message, created_at FROM announcements WHERE repo_url = ? AND workspace_id != ?"
	args := []any{repoURL, excludeWorkspace}

	if since != "" {
		query += " AND created_at >= ?"
		args = append(args, since)
	}

	query += " ORDER BY created_at DESC LIMIT 50"

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Announcement
	for rows.Next() {
		var a Announcement
		if err := rows.Scan(&a.ID, &a.WorkspaceID, &a.RepoURL, &a.Category, &a.Message, &a.CreatedAt); err != nil {
			continue
		}
		results = append(results, a)
	}
	if results == nil {
		results = []Announcement{}
	}
	return results, nil
}

// NormalizeRepoURL converts SSH/HTTPS git URLs to "owner/repo" form.
var sshPattern = regexp.MustCompile(`^git@[^:]+:(.+?)(?:\.git)?$`)
var httpsPattern = regexp.MustCompile(`^https?://[^/]+/(.+?)(?:\.git)?$`)

func NormalizeRepoURL(url string) string {
	if m := sshPattern.FindStringSubmatch(url); len(m) == 2 {
		return m[1]
	}
	if m := httpsPattern.FindStringSubmatch(url); len(m) == 2 {
		return m[1]
	}
	return strings.TrimSuffix(url, ".git")
}

// InvalidCategoryError is returned when an invalid category is used.
type InvalidCategoryError struct {
	Category string
}

func (e *InvalidCategoryError) Error() string {
	return "invalid category: " + e.Category + ". Must be one of: breaking_change, status, warning, info"
}
