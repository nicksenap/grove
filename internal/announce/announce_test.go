package announce

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: filepath.Join(t.TempDir(), "announcements"), NowFn: time.Now}
}

func TestPublishAndList(t *testing.T) {
	s := testStore(t)

	a, err := s.Publish("ws-a", "git@github.com:org/api.git", CategoryBreakingChange, "auth token format changed")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if a.Repo != "org/api" {
		t.Errorf("repo key = %q, want org/api", a.Repo)
	}
	if a.ID == "" || a.CreatedAt.IsZero() {
		t.Errorf("stored record incomplete: %+v", a)
	}

	got, err := s.List(ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Message != "auth token format changed" {
		t.Fatalf("list = %+v", got)
	}
}

// An agent coordinating with others must not be told what it just did itself.
func TestListExcludesOwnWorkspace(t *testing.T) {
	s := testStore(t)
	s.Publish("ws-a", "org/api", CategoryInfo, "from a")
	s.Publish("ws-b", "org/api", CategoryInfo, "from b")

	got, err := s.List(ListOptions{ExcludeWorkspace: "ws-a"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Workspace != "ws-b" {
		t.Fatalf("list = %+v, want only ws-b", got)
	}
}

func TestListFiltersByRepo(t *testing.T) {
	s := testStore(t)
	s.Publish("ws-a", "org/api", CategoryInfo, "about api")
	s.Publish("ws-a", "org/web", CategoryInfo, "about web")

	got, err := s.List(ListOptions{Repos: []string{"https://github.com/org/web"}})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Message != "about web" {
		t.Fatalf("list = %+v", got)
	}
}

// SSH and HTTPS remotes for one upstream must produce one key, or two agents on
// the same repo never see each other.
func TestNormalizeRepoUnifiesRemoteForms(t *testing.T) {
	want := "org/api"
	for _, form := range []string{
		"git@github.com:org/api.git",
		"git@github.com:org/api",
		"ssh://git@github.com/org/api.git",
		"https://github.com/org/api.git",
		"https://github.com/org/api",
		"https://user@gitlab.com/org/api.git",
		"org/api",
		"ORG/API",
	} {
		if got := NormalizeRepo(form); got != want {
			t.Errorf("NormalizeRepo(%q) = %q, want %q", form, got, want)
		}
	}
}

func TestNormalizeRepoKeepsNestedGroups(t *testing.T) {
	if got := NormalizeRepo("git@gitlab.com:group/sub/api.git"); got != "group/sub/api" {
		t.Errorf("got %q, want group/sub/api", got)
	}
}

func TestRepoKeyFallsBackToLocalName(t *testing.T) {
	if got := RepoKey("", "my-repo"); got != "my-repo" {
		t.Errorf("RepoKey with no remote = %q, want my-repo", got)
	}
	if got := RepoKey("git@github.com:org/api.git", "api"); got != "org/api" {
		t.Errorf("RepoKey should prefer the remote, got %q", got)
	}
}

func TestPublishRejectsInvalidCategory(t *testing.T) {
	s := testStore(t)
	if _, err := s.Publish("ws", "org/api", "gossip", "hello"); err == nil {
		t.Fatal("expected an invalid-category error")
	}
	for _, c := range Categories() {
		if _, err := s.Publish("ws", "org/api", c, "msg"); err != nil {
			t.Errorf("category %q should be valid: %v", c, err)
		}
	}
}

func TestPublishRejectsEmptyMessageAndRepo(t *testing.T) {
	s := testStore(t)
	if _, err := s.Publish("ws", "org/api", CategoryInfo, "   "); err == nil {
		t.Error("expected an error for an empty message")
	}
	if _, err := s.Publish("ws", "  ", CategoryInfo, "msg"); err == nil {
		t.Error("expected an error for an empty repo")
	}
}

func TestListNewestFirst(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, msg := range []string{"first", "second", "third"} {
		s.NowFn = func() time.Time { return base.Add(time.Duration(i) * time.Minute) }
		s.Publish("ws-a", "org/api", CategoryInfo, msg)
	}
	s.NowFn = func() time.Time { return base.Add(time.Hour) }

	got, err := s.List(ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 3 || got[0].Message != "third" || got[2].Message != "first" {
		t.Fatalf("order = %+v", got)
	}
}

func TestListSinceAndLimit(t *testing.T) {
	s := testStore(t)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	for i, msg := range []string{"old", "newer", "newest"} {
		s.NowFn = func() time.Time { return base.Add(time.Duration(i) * time.Hour) }
		s.Publish("ws-a", "org/api", CategoryInfo, msg)
	}
	s.NowFn = func() time.Time { return base.Add(5 * time.Hour) }

	got, _ := s.List(ListOptions{Since: base.Add(30 * time.Minute)})
	if len(got) != 2 {
		t.Errorf("since filter returned %d, want 2", len(got))
	}

	got, _ = s.List(ListOptions{Limit: 1})
	if len(got) != 1 || got[0].Message != "newest" {
		t.Errorf("limit returned %+v", got)
	}
}

func TestExpiredAnnouncementsAreHiddenAndPruned(t *testing.T) {
	s := testStore(t)
	s.MaxAge = time.Hour
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	s.NowFn = func() time.Time { return base }
	s.Publish("ws-a", "org/api", CategoryInfo, "stale")
	s.NowFn = func() time.Time { return base.Add(2 * time.Hour) }

	got, _ := s.List(ListOptions{})
	if len(got) != 0 {
		t.Errorf("expired announcement still listed: %+v", got)
	}

	// Publishing prunes, so the store cannot grow without bound and needs no
	// background job.
	s.Publish("ws-a", "org/api", CategoryInfo, "fresh")
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 1 {
		t.Errorf("expected the stale file to be pruned, found %d files", len(entries))
	}
}

// The whole point of the feature is several agents running at once, so
// concurrent publishes must not lose or overwrite each other.
func TestConcurrentPublishesAllLand(t *testing.T) {
	s := testStore(t)
	const writers = 24

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			if _, err := s.Publish("ws-a", "org/api", CategoryStatus, "note"); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent publish failed: %v", err)
	}

	got, err := s.List(ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != writers {
		t.Errorf("stored %d announcements, want %d", len(got), writers)
	}
}

// A corrupt file must not blind an agent to the rest — coordination data is
// advisory, and deleting data we cannot parse is not ours to decide.
func TestListSkipsUnparseableFiles(t *testing.T) {
	s := testStore(t)
	s.Publish("ws-a", "org/api", CategoryInfo, "good")
	os.WriteFile(filepath.Join(s.Dir, "broken.json"), []byte("{not json"), 0o644)

	got, err := s.List(ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Message != "good" {
		t.Fatalf("list = %+v", got)
	}

	s.Prune()
	if _, err := os.Stat(filepath.Join(s.Dir, "broken.json")); err != nil {
		t.Error("prune should leave unparseable files alone")
	}
}

func TestListOnMissingStoreIsEmpty(t *testing.T) {
	s := testStore(t)
	got, err := s.List(ListOptions{})
	if err != nil {
		t.Fatalf("list on a fresh machine should not fail: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want an empty slice, got %+v", got)
	}
}
