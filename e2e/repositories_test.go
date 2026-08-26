package e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestReposListing(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()

	res := env.mustGW("repos", "--json")
	repos := decodeJSON[[]repoEntry](t, res.stdout)
	found := false
	for _, r := range repos {
		if r.Name == "svc-auth" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("gw repos --json missing svc-auth: %s", res.stdout)
	}
	if len(repos) == 0 || repos[0].Name == "" || repos[0].Path == "" || repos[0].DisplayName == "" {
		t.Fatalf("gw repos --json missing expected fields: %s", res.stdout)
	}
	table := env.mustGW("repos")
	env.requireContains(table.combined(), "svc-auth", "gw repos table")
}

func TestCreateTrackExistingRemoteBranch(t *testing.T) {
	env := newEnv(t)
	origin := env.createBareOrigin("pr-svc-origin.git")
	repo := filepath.Join(env.reposDir, "pr-svc")
	env.git(env.home, "clone", "-q", origin, repo)
	env.writeFile(filepath.Join(repo, "README.md"), "base\n")
	env.git(repo, "add", ".")
	env.git(repo, "commit", "-q", "-m", "initial")
	env.git(repo, "push", "-q", "origin", "HEAD")
	env.pushRemoteBranch(repo, "feat/pr-head", "pr-marker.txt")
	env.init()

	env.mustGW("create", "pr-ws", "--repos", "pr-svc", "--branch", "feat/pr-head", "--track",
		"--source-url", "https://github.com/acme/pr-svc/pull/42",
		"--source-provider", "github",
		"--source-ref", "42",
		"--source-title", "Add the thing")

	env.requireExists(filepath.Join(env.worktree("pr-ws", "pr-svc"), "pr-marker.txt"))
	if got := env.upstream(env.worktree("pr-ws", "pr-svc")); got != "origin/feat/pr-head" {
		t.Fatalf("expected upstream origin/feat/pr-head, got %s", got)
	}
	shown := env.showWorkspace("pr-ws")
	if shown.Source == nil || shown.Source.Provider != "github" || shown.Source.Ref != "42" {
		t.Fatalf("source not persisted correctly: %+v", shown.Source)
	}
	status := env.mustGW("status", "pr-ws", "--json")
	decoded := decodeJSON[statusJSON](t, status.stdout)
	if decoded.Source == nil || decoded.Source.URL != "https://github.com/acme/pr-svc/pull/42" {
		t.Fatalf("status --json source url wrong: %+v", decoded.Source)
	}
	table := env.mustGW("status", "pr-ws")
	env.requireContains(table.combined(), "Source: github 42", "status table")
}

func TestCreateTrackFallback(t *testing.T) {
	env := newEnv(t)
	origin := env.createBareOrigin("pr-svc-origin.git")
	repo := filepath.Join(env.reposDir, "pr-svc")
	env.git(env.home, "clone", "-q", origin, repo)
	env.writeFile(filepath.Join(repo, "README.md"), "base\n")
	env.git(repo, "add", ".")
	env.git(repo, "commit", "-q", "-m", "initial")
	env.git(repo, "push", "-q", "origin", "HEAD")
	env.init()

	env.mustGW("create", "fallback-ws", "--repos", "pr-svc", "--branch", "feat/ghost-pr", "--track")
	if got := env.currentBranch(env.worktree("fallback-ws", "pr-svc")); got != "feat/ghost-pr" {
		t.Fatalf("expected feat/ghost-pr, got %s", got)
	}
}

func TestAddAndRemoveRepos(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.createRepo("svc-api")
	env.createRepo("svc-gateway")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth,svc-api")

	env.mustGW("add-repo", "test-ws", "--repos", "svc-gateway")
	env.requireExists(env.worktree("test-ws", "svc-gateway"))
	if got := env.currentBranch(env.worktree("test-ws", "svc-gateway")); got != "feat/e2e" {
		t.Fatalf("expected feat/e2e, got %s", got)
	}
	if got := len(env.showWorkspace("test-ws").Repos); got != 3 {
		t.Fatalf("expected 3 repos in state, got %d", got)
	}

	env.mustGW("remove-repo", "test-ws", "--repos", "svc-gateway", "--force")
	env.requireMissing(env.worktree("test-ws", "svc-gateway"))
}

func TestAddRepoFromRemoteURL(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")

	origin := env.createBareOrigin("remote-origin.git")
	env.seedBareOrigin(origin, "remote content\n")
	url := fileURL(origin)

	env.mustGW("add-repo", "test-ws", "--repos", url)
	env.requireExists(filepath.Join(env.reposDir, "remote-origin", ".git"))
	env.requireExists(env.worktree("test-ws", "remote-origin"))
	if got := env.currentBranch(env.worktree("test-ws", "remote-origin")); got != "feat/e2e" {
		t.Fatalf("expected feat/e2e, got %s", got)
	}
	if got := len(env.showWorkspace("test-ws").Repos); got != 2 {
		t.Fatalf("expected 2 repos, got %d", got)
	}

	env.mustGW("add-repo", "test-ws", "--repos", url)
	if got := len(env.showWorkspace("test-ws").Repos); got != 2 {
		t.Fatalf("idempotent add-repo changed repo count to %d", got)
	}
}

func TestAddRepoFromHTTPSRemote(t *testing.T) {
	if os.Getenv("GROVE_EXTERNAL_E2E") != "1" {
		t.Skip("set GROVE_EXTERNAL_E2E=1 to run the public HTTPS clone check")
	}
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")

	const httpsURL = "https://github.com/nicksenap/gw-zellij.git"
	env.timeout = 2 * env.timeout
	env.mustGW("add-repo", "test-ws", "--repos", httpsURL)
	env.requireExists(filepath.Join(env.reposDir, "gw-zellij", ".git"))
	env.requireExists(env.worktree("test-ws", "gw-zellij"))
	if got := env.currentBranch(env.worktree("test-ws", "gw-zellij")); got != "feat/e2e" {
		t.Fatalf("expected feat/e2e, got %s", got)
	}
	cloned := env.git(filepath.Join(env.reposDir, "gw-zellij"), "remote", "get-url", "origin")
	if cloned != httpsURL {
		t.Fatalf("expected origin %s, got %s", httpsURL, cloned)
	}
	env.mustGW("remove-repo", "test-ws", "--repos", "gw-zellij", "--force")
}

func TestAddRepoCwdAutoDetect(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.createRepo("svc-gateway")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")

	env.mustGWIn(env.workspacePath("test-ws"), "add-repo", "--repos", "svc-gateway")
	if got := len(env.showWorkspace("test-ws").Repos); got != 2 {
		t.Fatalf("expected 2 repos after cwd-detected add-repo, got %d", got)
	}

	env.mustGW("remove-repo", "test-ws", "--repos", "svc-gateway", "--force")
	env.mustGWIn(env.worktree("test-ws", "svc-auth"), "add-repo", "--repos", "svc-gateway")
	if got := len(env.showWorkspace("test-ws").Repos); got != 2 {
		t.Fatalf("expected 2 repos from subdir detect, got %d", got)
	}
}

func TestRecursiveRepositoryDiscovery(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()

	nested := filepath.Join(env.home, "nested-repos")
	if err := os.MkdirAll(filepath.Join(nested, "team"), 0o755); err != nil {
		t.Fatal(err)
	}
	env.git(env.home, "init", "-q", filepath.Join(nested, "team", "svc-nested"))
	env.git(filepath.Join(nested, "team", "svc-nested"), "commit", "--allow-empty", "-q", "-m", "initial commit")

	out := env.mustGW("add-dir", nested)
	env.requireContains(out.combined(), "1 repos found", "add-dir nested discovery")

	env.mustGW("create", "nested-discovery", "-b", "feat/nested-discovery", "-r", "svc-nested")
	env.mustGW("-n", "delete", "nested-discovery")

	help := env.mustGW("--help")
	if regexp.MustCompile(`(?m)^  explore\s`).MatchString(help.combined()) {
		t.Fatal("explore command is still advertised")
	}
}
