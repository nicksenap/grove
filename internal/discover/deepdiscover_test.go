package discover

import (
	"os"
	"path/filepath"
	"testing"
)

// Helper to create a fake repo with .git/config (needed for cache mtime)
func makeFakeRepo(t *testing.T, dir, name string) string {
	t.Helper()
	repoPath := filepath.Join(dir, name)
	gitDir := filepath.Join(repoPath, ".git")
	os.MkdirAll(gitDir, 0o755)
	os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]"), 0o644)
	return repoPath
}

func TestDiscoverReposBasic(t *testing.T) {
	dir := t.TempDir()
	makeFakeRepo(t, dir, "api")
	makeFakeRepo(t, dir, "web")

	// Mock fetcher: returns a unique URL per repo
	fetcher := func(path string) string {
		name := filepath.Base(path)
		return "git@github.com:owner/" + name + ".git"
	}

	results := DiscoverRepos([]string{dir}, fetcher)

	if len(results) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(results))
	}

	// Should be sorted by display name
	if results[0].DisplayName > results[1].DisplayName {
		t.Error("results should be sorted by display name")
	}
}

func TestDiscoverReposDedupeByRemoteURL(t *testing.T) {
	dir := t.TempDir()
	// Two paths, same remote URL
	makeFakeRepo(t, dir, "repo-fork")
	nested := filepath.Join(dir, "org")
	os.MkdirAll(nested, 0o755)
	makeFakeRepo(t, nested, "repo-original")

	// Both return the same remote URL
	fetcher := func(path string) string {
		return "git@github.com:owner/repo.git"
	}

	results := DiscoverRepos([]string{dir}, fetcher)

	if len(results) != 1 {
		t.Fatalf("expected 1 repo after dedup, got %d", len(results))
	}
}

func TestDiscoverReposDirectChildWinsOverNested(t *testing.T) {
	dir := t.TempDir()
	// Direct child of configured dir
	directPath := makeFakeRepo(t, dir, "myrepo")
	// Nested repo with same remote
	nested := filepath.Join(dir, "org", "subdir")
	os.MkdirAll(nested, 0o755)
	makeFakeRepo(t, nested, "myrepo-nested")

	fetcher := func(path string) string {
		return "git@github.com:owner/myrepo.git" // same URL for both
	}

	results := DiscoverRepos([]string{dir}, fetcher)

	if len(results) != 1 {
		t.Fatalf("expected 1 after dedup, got %d", len(results))
	}
	if results[0].Path != directPath {
		t.Errorf("direct child should win, got %s", results[0].Path)
	}
}

func TestDiscoverReposNoRemoteUsesPath(t *testing.T) {
	dir := t.TempDir()
	makeFakeRepo(t, dir, "local-only")

	fetcher := func(path string) string {
		return "" // no remote
	}

	results := DiscoverRepos([]string{dir}, fetcher)

	if len(results) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(results))
	}
	if results[0].Remote != "" {
		t.Errorf("expected empty remote, got %q", results[0].Remote)
	}
	if results[0].DisplayName != "local-only" {
		t.Errorf("expected folder name as display, got %q", results[0].DisplayName)
	}
}

func TestDiscoverReposNoRemoteDedupByPath(t *testing.T) {
	dir := t.TempDir()
	// Two repos with no remote — should NOT dedup (different paths)
	makeFakeRepo(t, dir, "local-a")
	makeFakeRepo(t, dir, "local-b")

	fetcher := func(path string) string {
		return "" // no remote for either
	}

	results := DiscoverRepos([]string{dir}, fetcher)

	if len(results) != 2 {
		t.Fatalf("repos with no remote and different paths should not dedup, got %d", len(results))
	}
}

func TestDiscoverReposDisplayNameFromURL(t *testing.T) {
	dir := t.TempDir()
	makeFakeRepo(t, dir, "myrepo")

	fetcher := func(path string) string {
		return "git@github.com:acme/cool-project.git"
	}

	results := DiscoverRepos([]string{dir}, fetcher)

	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].DisplayName != "acme/cool-project" {
		t.Errorf("expected display name from URL, got %q", results[0].DisplayName)
	}
}

func TestDiscoverReposSortedByDisplayName(t *testing.T) {
	dir := t.TempDir()
	makeFakeRepo(t, dir, "z-repo")
	makeFakeRepo(t, dir, "a-repo")
	makeFakeRepo(t, dir, "m-repo")

	fetcher := func(path string) string {
		return "" // use folder names as display
	}

	results := DiscoverRepos([]string{dir}, fetcher)

	for i := 1; i < len(results); i++ {
		if results[i].DisplayName < results[i-1].DisplayName {
			t.Errorf("not sorted: %q before %q", results[i-1].DisplayName, results[i].DisplayName)
		}
	}
}

func TestDiscoverReposSortsNameTiesByPath(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	first := makeFakeRepo(t, dir1, "api")
	second := makeFakeRepo(t, dir2, "api")

	results := DiscoverRepos([]string{dir2, dir1}, func(string) string { return "" })

	wantFirst := first
	if second < first {
		wantFirst = second
	}
	if len(results) != 2 || results[0].Path != wantFirst {
		t.Fatalf("name tie was not sorted by path: %#v", results)
	}
}

func TestDiscoverReposEmptyDirs(t *testing.T) {
	results := DiscoverRepos([]string{}, func(string) string { return "" })
	if len(results) != 0 {
		t.Errorf("expected empty, got %d", len(results))
	}
}

func TestDiscoverReposMaxDepthRespected(t *testing.T) {
	dir := t.TempDir()
	// Create repo at depth 4 (beyond default maxDepth=3)
	deep := filepath.Join(dir, "a", "b", "c", "d")
	makeFakeRepo(t, deep, "too-deep")
	// Also one at depth 1
	makeFakeRepo(t, dir, "shallow")

	fetcher := func(path string) string { return "" }

	results := DiscoverRepos([]string{dir}, fetcher)

	for _, r := range results {
		if r.Name == "too-deep" {
			t.Error("should not find repos beyond max depth")
		}
	}
	found := false
	for _, r := range results {
		if r.Name == "shallow" {
			found = true
		}
	}
	if !found {
		t.Error("should find shallow repo")
	}
}

func TestDiscoverReposMultipleDirs(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	makeFakeRepo(t, dir1, "api")
	makeFakeRepo(t, dir2, "web")

	fetcher := func(path string) string {
		return "git@github.com:owner/" + filepath.Base(path) + ".git"
	}

	results := DiscoverRepos([]string{dir1, dir2}, fetcher)

	if len(results) != 2 {
		t.Fatalf("expected 2 repos from 2 dirs, got %d", len(results))
	}
}

func TestDiscoverReposFindsNestedRepos(t *testing.T) {
	dir := t.TempDir()
	makeFakeRepo(t, filepath.Join(dir, "org"), "nested-repo")

	results := DiscoverRepos([]string{dir}, func(string) string { return "" })

	if len(results) != 1 || results[0].Name != "nested-repo" {
		t.Fatalf("nested repo not discovered: %#v", results)
	}
}

func TestDiscoverReposSkipsIgnoredDirectories(t *testing.T) {
	dir := t.TempDir()
	makeFakeRepo(t, filepath.Join(dir, "node_modules"), "dependency")
	makeFakeRepo(t, filepath.Join(dir, "__pycache__"), "cached")
	makeFakeRepo(t, filepath.Join(dir, ".hidden"), "hidden")
	makeFakeRepo(t, dir, "visible")

	results := DiscoverRepos([]string{dir}, func(string) string { return "" })

	if len(results) != 1 || results[0].Name != "visible" {
		t.Fatalf("ignored directories were scanned: %#v", results)
	}
}

func TestRepoMapKeepsFirstDuplicateName(t *testing.T) {
	repos := []RepoInfo{{Name: "api", Path: "/src/first"}, {Name: "api", Path: "/src/second"}}

	repoMap := RepoMap(repos)

	if repoMap["api"] != "/src/first" {
		t.Fatalf("duplicate name resolved to %q, want first repo", repoMap["api"])
	}
}

func TestUniqueByNameKeepsFirstRepo(t *testing.T) {
	repos := []RepoInfo{{Name: "api", Path: "/src/first"}, {Name: "web", Path: "/src/web"}, {Name: "api", Path: "/src/second"}}

	unique := UniqueByName(repos)

	if len(unique) != 2 || unique[0].Path != "/src/first" || unique[1].Name != "web" {
		t.Fatalf("unexpected unique repos: %#v", unique)
	}
}

func TestRepoMap(t *testing.T) {
	repos := []RepoInfo{{Name: "api", Path: "/src/api"}, {Name: "web", Path: "/src/web"}}

	repoMap := RepoMap(repos)

	if repoMap["api"] != "/src/api" || repoMap["web"] != "/src/web" {
		t.Fatalf("unexpected repo map: %#v", repoMap)
	}
}
