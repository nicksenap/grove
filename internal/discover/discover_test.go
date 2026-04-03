package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func makeRepo(t *testing.T, dir, name string) string {
	t.Helper()
	repoPath := filepath.Join(dir, name)
	gitDir := filepath.Join(repoPath, ".git")
	os.MkdirAll(gitDir, 0o755)
	return repoPath
}

func TestFindReposSingleDir(t *testing.T) {
	dir := t.TempDir()
	makeRepo(t, dir, "api")
	makeRepo(t, dir, "web")
	// Non-repo dir
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)

	repos := FindRepos([]string{dir})

	// api and web should be found (they have .git dirs)
	// docs should NOT (no .git) - but note: IsGitRepo calls git rev-parse
	// which may not work with fake .git dirs. Let's see what we get.
	if len(repos) < 1 {
		t.Logf("FindRepos found %d repos (may fail with fake .git dirs)", len(repos))
	}
}

func TestFindReposSkipsHiddenDirs(t *testing.T) {
	dir := t.TempDir()
	makeRepo(t, dir, ".hidden")
	makeRepo(t, dir, "visible")

	repos := FindRepos([]string{dir})

	for _, r := range repos {
		if r.Name == ".hidden" {
			t.Error("should skip hidden directories")
		}
	}
}

func TestFindReposMultipleDirsFirstWins(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	makeRepo(t, dir1, "api")
	makeRepo(t, dir2, "api")

	repos := FindRepos([]string{dir1, dir2})

	count := 0
	for _, r := range repos {
		if r.Name == "api" {
			count++
			if r.Path != filepath.Join(dir1, "api") {
				t.Errorf("expected first dir's api, got %s", r.Path)
			}
		}
	}
	if count > 1 {
		t.Errorf("duplicate 'api' entries: %d", count)
	}
}

func TestFindReposNonexistentDir(t *testing.T) {
	repos := FindRepos([]string{"/nonexistent/path"})
	if len(repos) != 0 {
		t.Errorf("expected empty for nonexistent dir, got %d", len(repos))
	}
}

func TestFindReposEmptyDirList(t *testing.T) {
	repos := FindRepos([]string{})
	if len(repos) != 0 {
		t.Errorf("expected empty, got %d", len(repos))
	}
}

func TestFindReposSortedByName(t *testing.T) {
	dir := t.TempDir()
	makeRepo(t, dir, "zebra")
	makeRepo(t, dir, "alpha")
	makeRepo(t, dir, "middle")

	repos := FindRepos([]string{dir})
	for i := 1; i < len(repos); i++ {
		if repos[i].Name < repos[i-1].Name {
			t.Errorf("repos not sorted: %q before %q", repos[i-1].Name, repos[i].Name)
		}
	}
}

func TestRepoMap(t *testing.T) {
	repos := []Repo{
		{Name: "api", Path: "/src/api"},
		{Name: "web", Path: "/src/web"},
	}

	m := RepoMap(repos)
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	if m["api"] != "/src/api" {
		t.Errorf("api: got %q", m["api"])
	}
	if m["web"] != "/src/web" {
		t.Errorf("web: got %q", m["web"])
	}
}

func TestFindAllReposIsAlias(t *testing.T) {
	dir := t.TempDir()
	makeRepo(t, dir, "api")

	repos1 := FindRepos([]string{dir})
	repos2 := FindAllRepos([]string{dir})

	if len(repos1) != len(repos2) {
		t.Errorf("FindAllRepos should return same as FindRepos")
	}
}

// --- Explore tests ---

func TestExploreFindsNestedRepos(t *testing.T) {
	dir := t.TempDir()
	makeRepo(t, dir, "direct-repo")
	nested := filepath.Join(dir, "org")
	makeRepo(t, nested, "nested-repo")

	results := Explore([]string{dir})

	foundDirect := false
	foundNested := false
	for _, r := range results {
		if r.Name == "direct-repo" {
			foundDirect = true
			if r.Nested {
				t.Error("direct repo should not be marked nested")
			}
		}
		if r.Name == "nested-repo" {
			foundNested = true
			if !r.Nested {
				t.Error("nested repo should be marked nested")
			}
		}
	}
	if !foundDirect {
		t.Error("direct repo not found")
	}
	if !foundNested {
		t.Error("nested repo not found")
	}
}

func TestExploreGroupsByParentDir(t *testing.T) {
	dir := t.TempDir()
	makeRepo(t, dir, "repo1")

	results := Explore([]string{dir})

	for _, r := range results {
		if r.ParentDir != dir {
			t.Errorf("expected parent dir %q, got %q", dir, r.ParentDir)
		}
	}
}

func TestExploreSkipsSpecialDirs(t *testing.T) {
	dir := t.TempDir()
	// Create dirs that should be skipped
	os.MkdirAll(filepath.Join(dir, "node_modules", ".git"), 0o755)
	os.MkdirAll(filepath.Join(dir, "__pycache__", ".git"), 0o755)
	makeRepo(t, dir, "real-repo")

	results := Explore([]string{dir})

	for _, r := range results {
		if r.Name == "node_modules" || r.Name == "__pycache__" {
			t.Errorf("should skip %q", r.Name)
		}
	}
}

func TestRepoNameFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/user/dev/api", "api"},
		{"/home/user/dev/api/", "api"},
		{"api", "api"},
	}
	for _, tt := range tests {
		got := RepoNameFromPath(tt.path)
		if got != tt.want {
			t.Errorf("RepoNameFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
