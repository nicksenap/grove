package discover

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T, dir string) {
	t.Helper()
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "initial"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}
}

func TestFindRepos(t *testing.T) {
	parent := t.TempDir()

	// Create 3 dirs: 2 git repos, 1 non-repo
	for _, name := range []string{"repo-a", "repo-b", "not-a-repo"} {
		os.MkdirAll(filepath.Join(parent, name), 0o755)
	}
	initRepo(t, filepath.Join(parent, "repo-a"))
	initRepo(t, filepath.Join(parent, "repo-b"))

	repos := FindRepos(parent)
	if len(repos) != 2 {
		t.Errorf("found %d repos, want 2", len(repos))
	}
	if _, ok := repos["repo-a"]; !ok {
		t.Error("repo-a not found")
	}
	if _, ok := repos["repo-b"]; !ok {
		t.Error("repo-b not found")
	}
}

func TestFindAllRepos(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	os.MkdirAll(filepath.Join(dir1, "svc-api"), 0o755)
	initRepo(t, filepath.Join(dir1, "svc-api"))

	os.MkdirAll(filepath.Join(dir2, "web-app"), 0o755)
	initRepo(t, filepath.Join(dir2, "web-app"))

	repos := FindAllRepos([]string{dir1, dir2})
	if len(repos) != 2 {
		t.Errorf("found %d repos, want 2", len(repos))
	}
}

func TestFindAllReposDedup(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Same name in both dirs — first wins
	os.MkdirAll(filepath.Join(dir1, "shared"), 0o755)
	initRepo(t, filepath.Join(dir1, "shared"))
	os.MkdirAll(filepath.Join(dir2, "shared"), 0o755)
	initRepo(t, filepath.Join(dir2, "shared"))

	repos := FindAllRepos([]string{dir1, dir2})
	if len(repos) != 1 {
		t.Errorf("found %d repos, want 1 (dedup)", len(repos))
	}
	if repos["shared"] != filepath.Join(dir1, "shared") {
		t.Errorf("expected first dir to win, got %q", repos["shared"])
	}
}

func TestFindReposSkipsHidden(t *testing.T) {
	parent := t.TempDir()
	os.MkdirAll(filepath.Join(parent, ".hidden-repo"), 0o755)
	initRepo(t, filepath.Join(parent, ".hidden-repo"))

	repos := FindRepos(parent)
	if len(repos) != 0 {
		t.Errorf("found %d repos, want 0 (hidden dirs skipped)", len(repos))
	}
}

func TestExploreRepos(t *testing.T) {
	parent := t.TempDir()

	// Create nested structure: parent/group/repo
	os.MkdirAll(filepath.Join(parent, "services", "svc-api"), 0o755)
	initRepo(t, filepath.Join(parent, "services", "svc-api"))
	os.MkdirAll(filepath.Join(parent, "services", "svc-auth"), 0o755)
	initRepo(t, filepath.Join(parent, "services", "svc-auth"))

	grouped := ExploreRepos([]string{parent}, 3)
	if len(grouped) != 1 {
		t.Errorf("expected 1 group, got %d", len(grouped))
	}
	if repos, ok := grouped[parent]; ok {
		if len(repos) != 2 {
			t.Errorf("expected 2 repos in group, got %d", len(repos))
		}
	}
}
