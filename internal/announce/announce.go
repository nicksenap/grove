// Package announce provides cross-workspace coordination between concurrent
// coding agents: one agent publishes a note about a repo ("I changed the auth
// token format"), and agents working on that same repo in other workspaces see
// it.
//
// # Why a directory of files
//
// The point of this feature is several agent processes running at once, so the
// store has to tolerate concurrent writers. Each announcement is therefore its
// own file created with O_EXCL in one directory:
//
//   - publishing is a single atomic file creation — no locking, no read-modify-
//     write window, and no way for two agents to clobber each other;
//   - reading is a directory scan, unaffected by concurrent publishes;
//   - pruning unlinks expired files, which is safe while others read or write.
//
// The predecessor of this package used SQLite for the same job. That pulled in a
// ~4 MB dependency tree to serialize writes that file creation already
// serializes, and required a JSON-RPC server to reach it. Grove's volume here is
// a handful of short messages per day.
package announce

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DefaultMaxAge is how long an announcement stays visible. Coordination notes
// are about work in flight; a month-old note is noise.
const DefaultMaxAge = 30 * 24 * time.Hour

// Categories an announcement may carry. This vocabulary is part of the machine
// CLI contract, so agents can filter on it.
const (
	CategoryBreakingChange = "breaking_change"
	CategoryStatus         = "status"
	CategoryWarning        = "warning"
	CategoryInfo           = "info"
)

// Categories lists every valid category, in rough order of urgency.
func Categories() []string {
	return []string{CategoryBreakingChange, CategoryWarning, CategoryStatus, CategoryInfo}
}

func validCategory(c string) bool {
	for _, valid := range Categories() {
		if c == valid {
			return true
		}
	}
	return false
}

// Announcement is one note published by one workspace about one repo.
type Announcement struct {
	ID string `json:"id"`
	// Workspace is the publisher, so readers can tell who is doing what and
	// exclude their own notes.
	Workspace string `json:"workspace"`
	// Repo is the coordination key: a normalized "owner/repo" derived from the
	// repo's remote, so two workspaces holding different worktrees of the same
	// upstream agree on it. See RepoKey.
	Repo      string    `json:"repo"`
	Category  string    `json:"category"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

// Store is a directory of announcement files.
type Store struct {
	Dir string
	// NowFn is injectable for tests.
	NowFn func() time.Time
	// MaxAge overrides DefaultMaxAge when non-zero.
	MaxAge time.Duration
}

// NewStore returns the production store rooted in groveDir.
func NewStore(groveDir string) *Store {
	return &Store{Dir: filepath.Join(groveDir, "announcements"), NowFn: time.Now}
}

func (s *Store) now() time.Time {
	if s.NowFn != nil {
		return s.NowFn()
	}
	return time.Now()
}

func (s *Store) maxAge() time.Duration {
	if s.MaxAge > 0 {
		return s.MaxAge
	}
	return DefaultMaxAge
}

// InvalidCategoryError reports an unsupported category.
type InvalidCategoryError struct{ Category string }

func (e *InvalidCategoryError) Error() string {
	return fmt.Sprintf("invalid category %q (want one of: %s)",
		e.Category, strings.Join(Categories(), ", "))
}

// Publish stores an announcement and returns the stored record. It also prunes
// expired entries opportunistically, so the store cannot grow without bound and
// no background process is needed.
func (s *Store) Publish(workspace, repo, category, message string) (*Announcement, error) {
	if !validCategory(category) {
		return nil, &InvalidCategoryError{category}
	}
	if strings.TrimSpace(message) == "" {
		return nil, fmt.Errorf("message is empty")
	}
	if strings.TrimSpace(repo) == "" {
		return nil, fmt.Errorf("repo is empty")
	}

	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", s.Dir, err)
	}

	created := s.now().UTC()
	a := Announcement{
		ID:        newID(created),
		Workspace: workspace,
		Repo:      NormalizeRepo(repo),
		Category:  category,
		Message:   strings.TrimSpace(message),
		CreatedAt: created,
	}

	data, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}

	// O_EXCL makes the publish atomic and collision-proof: if two agents somehow
	// generate the same ID, the loser retries with a fresh one instead of
	// overwriting the winner's note.
	for attempt := 0; ; attempt++ {
		path := filepath.Join(s.Dir, a.ID+".json")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			_, werr := f.Write(data)
			cerr := f.Close()
			if werr != nil {
				return nil, werr
			}
			if cerr != nil {
				return nil, cerr
			}
			break
		}
		if !os.IsExist(err) || attempt >= 5 {
			return nil, fmt.Errorf("writing announcement: %w", err)
		}
		a.ID = newID(created)
		if data, err = json.Marshal(a); err != nil {
			return nil, err
		}
	}

	s.Prune()
	return &a, nil
}

// ListOptions filters a read. A zero value returns everything unexpired.
type ListOptions struct {
	// Repos limits results to these coordination keys. Empty means every repo.
	Repos []string
	// ExcludeWorkspace drops one publisher's own notes — an agent coordinating
	// with others does not need to be told what it just did.
	ExcludeWorkspace string
	// Since drops anything older. Zero means "everything unexpired".
	Since time.Time
	// Limit caps the result count. Zero means unlimited.
	Limit int
}

// List returns matching announcements, newest first. A malformed or vanished
// file is skipped rather than failing the read: coordination data is advisory,
// and one bad file must not blind an agent to the rest.
func (s *Store) List(opts ListOptions) ([]Announcement, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Announcement{}, nil
		}
		return nil, err
	}

	wanted := make(map[string]bool, len(opts.Repos))
	for _, r := range opts.Repos {
		wanted[NormalizeRepo(r)] = true
	}

	cutoff := s.now().UTC().Add(-s.maxAge())

	results := []Announcement{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, entry.Name()))
		if err != nil {
			continue
		}
		var a Announcement
		if err := json.Unmarshal(data, &a); err != nil {
			continue
		}
		if a.CreatedAt.Before(cutoff) {
			continue
		}
		if len(wanted) > 0 && !wanted[a.Repo] {
			continue
		}
		if opts.ExcludeWorkspace != "" && a.Workspace == opts.ExcludeWorkspace {
			continue
		}
		if !opts.Since.IsZero() && a.CreatedAt.Before(opts.Since) {
			continue
		}
		results = append(results, a)
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].CreatedAt.Equal(results[j].CreatedAt) {
			return results[i].ID > results[j].ID
		}
		return results[i].CreatedAt.After(results[j].CreatedAt)
	})

	if opts.Limit > 0 && len(results) > opts.Limit {
		results = results[:opts.Limit]
	}
	return results, nil
}

// Prune deletes expired announcements and reports how many were removed.
// Unlinking is safe while other processes read or publish.
func (s *Store) Prune() int {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return 0
	}
	cutoff := s.now().UTC().Add(-s.maxAge())

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(s.Dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var a Announcement
		if err := json.Unmarshal(data, &a); err != nil {
			// Unparseable files are left alone: they might be another tool's, and
			// deleting data we cannot read is not ours to decide.
			continue
		}
		if a.CreatedAt.Before(cutoff) && os.Remove(path) == nil {
			removed++
		}
	}
	return removed
}

// newID builds a lexically sortable, collision-resistant id. The timestamp uses
// a filename-safe layout (no colons) so it works on Windows too.
func newID(t time.Time) string {
	var buf [4]byte
	rand.Read(buf[:])
	return t.Format("20060102T150405.000000000") + "-" + hex.EncodeToString(buf[:])
}

var (
	sshPattern   = regexp.MustCompile(`^(?:ssh://)?git@[^:/]+[:/](.+?)(?:\.git)?$`)
	httpsPattern = regexp.MustCompile(`^https?://(?:[^@/]+@)?[^/]+/(.+?)(?:\.git)?$`)
)

// NormalizeRepo reduces a git remote URL to "owner/repo" so that SSH and HTTPS
// remotes for the same upstream produce the same coordination key. A value that
// is not a URL (e.g. a bare repo name) is returned lowercased and trimmed, which
// keeps the store usable for repos with no remote.
func NormalizeRepo(repo string) string {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return ""
	}
	if m := sshPattern.FindStringSubmatch(repo); len(m) == 2 {
		return strings.ToLower(strings.Trim(m[1], "/"))
	}
	if m := httpsPattern.FindStringSubmatch(repo); len(m) == 2 {
		return strings.ToLower(strings.Trim(m[1], "/"))
	}
	return strings.ToLower(strings.TrimSuffix(repo, ".git"))
}

// RepoKey picks the coordination key for a repo: its normalized remote when it
// has one, otherwise its local name. Both sides of a coordination — publisher and
// reader — must call this, or they will key on different strings and never see
// each other's notes.
func RepoKey(remoteURL, localName string) string {
	if key := NormalizeRepo(remoteURL); key != "" {
		return key
	}
	return NormalizeRepo(localName)
}
