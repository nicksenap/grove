package e2e

import (
	"path/filepath"
	"testing"
)

func TestVersion(t *testing.T) {
	env := newEnv(t)
	res := env.mustGW("--version")
	if res.stdout == "" && res.stderr == "" {
		t.Fatal("gw --version produced no output")
	}
}

func TestInitAndDoctor(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.mustGW("init", env.reposDir)
	if issues := env.doctorIssues(); len(issues) != 0 {
		t.Fatalf("doctor: found %d issue(s) after clean init: %+v", len(issues), issues)
	}
}

func TestCreateWorkspace(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.createRepo("svc-api")
	env.addGroveTOML("svc-auth", "setup = \"touch .grove-setup-ran\"\n")
	env.init()

	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth,svc-api")

	if workspaceNamed(env.listWorkspaces(), "test-ws") == nil {
		t.Fatal("workspace not in list --json")
	}
	env.requireExists(env.worktree("test-ws", "svc-auth"))
	env.requireExists(env.worktree("test-ws", "svc-api"))
	if got := env.currentBranch(env.worktree("test-ws", "svc-auth")); got != "feat/e2e" {
		t.Fatalf("expected branch feat/e2e, got %s", got)
	}
	env.requireExists(filepath.Join(env.worktree("test-ws", "svc-auth"), ".grove-setup-ran"))
}

func TestWsListAndShow(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")

	res := env.mustGW("ws", "list", "--json")
	if workspaceNamed(decodeJSON[[]workspaceJSON](t, res.stdout), "test-ws") == nil {
		t.Fatal("gw ws list --json missing workspace")
	}
	shown := env.showWorkspace("test-ws")
	if shown.Name != "test-ws" {
		t.Fatalf("gw ws show --json name = %q", shown.Name)
	}
	env.gw("ws", "show", "nonexistent-ws").mustFail(t)
}

func TestDuplicateWorkspaceRejected(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")
	env.gw("create", "test-ws", "--branch", "feat/dupe", "--repos", "svc-auth").mustFail(t)
}

func TestGo(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")

	res := env.mustGW("go", "test-ws")
	if got := res.stdout; got != env.workspacePath("test-ws") {
		t.Fatalf("go: expected %s, got %s", env.workspacePath("test-ws"), got)
	}
	env.gw("go", "nonexistent-ws").mustFail(t)
}

func TestStatus(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")
	env.mustGW("status", "test-ws")
}

func TestRename(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")
	env.mustGW("rename", "test-ws", "--to", "renamed-ws")

	if workspaceNamed(env.listWorkspaces(), "test-ws") != nil {
		t.Fatal("old workspace name still in list")
	}
	if workspaceNamed(env.listWorkspaces(), "renamed-ws") == nil {
		t.Fatal("new workspace name not in list")
	}
	env.requireExists(env.worktree("renamed-ws", "svc-auth"))
}

func TestMultipleWorkspaces(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")
	env.mustGW("create", "ws-two", "--branch", "feat/other", "--repos", "svc-auth")

	if got := len(env.listWorkspaces()); got != 2 {
		t.Fatalf("expected 2 workspaces, got %d", got)
	}
	if got := env.currentBranch(env.worktree("ws-two", "svc-auth")); got != "feat/other" {
		t.Fatalf("expected feat/other, got %s", got)
	}
}

func TestDeleteWorkspace(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")
	env.mustGW("create", "ws-two", "--branch", "feat/other", "--repos", "svc-auth")
	env.mustGW("delete", "ws-two")

	if got := len(env.listWorkspaces()); got != 1 {
		t.Fatalf("expected 1 workspace after delete, got %d", got)
	}
	env.requireMissing(env.workspacePath("ws-two"))
	if env.branchExists(filepath.Join(env.reposDir, "svc-auth"), "feat/other") {
		t.Fatal("branch feat/other still present in source repo")
	}
}

func TestWsDeleteSubcommand(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "keep-ws", "--branch", "feat/keep", "--repos", "svc-auth")
	env.mustGW("create", "ws-del-sub", "--branch", "feat/ws-del", "--repos", "svc-auth")
	env.mustGW("ws", "delete", "ws-del-sub")

	if got := len(env.listWorkspaces()); got != 1 {
		t.Fatalf("expected 1 workspace after gw ws delete, got %d", got)
	}
	env.requireMissing(env.workspacePath("ws-del-sub"))
}

func TestCreateReplace(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-api")
	env.init()
	env.mustGW("create", "replace-old", "--branch", "feat/replace-old", "--repos", "svc-api")
	oldDir := env.workspacePath("replace-old")

	env.mustGWIn(oldDir, "create", "replace-new", "--branch", "feat/replace-new", "--repos", "svc-api", "--replace", "-f")

	if workspaceNamed(env.listWorkspaces(), "replace-old") != nil {
		t.Fatal("old workspace still present after --replace")
	}
	if workspaceNamed(env.listWorkspaces(), "replace-new") == nil {
		t.Fatal("new workspace missing after --replace")
	}
	env.requireMissing(oldDir)
	env.requireExists(env.worktree("replace-new", "svc-api"))

	env.gwIn(env.home, "create", "should-not-exist", "--branch", "feat/nope", "--repos", "svc-api", "--replace", "-f").mustFail(t)
	env.gwIn(env.workspacePath("replace-new"), "create", "replace-new", "--branch", "feat/collide", "--repos", "svc-api", "--replace", "-f").mustFail(t)
}

func TestStatusAutoDetectFromCwd(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")
	env.mustGWIn(env.worktree("test-ws", "svc-auth"), "status")
}

func TestFinalCleanup(t *testing.T) {
	env := newEnv(t)
	env.createRepo("svc-auth")
	env.init()
	env.mustGW("create", "test-ws", "--branch", "feat/e2e", "--repos", "svc-auth")
	env.mustGW("delete", "test-ws")
	if got := len(env.listWorkspaces()); got != 0 {
		t.Fatalf("expected 0 workspaces, got %d", got)
	}
}
