package workspace

import (
	"strings"
	"testing"
)

func TestLoggingCreateAndDelete(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	// Create workspace
	err := env.createWorkspace("log-ws", "feat/log", []string{"api", "web"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	log := readLog()

	// Should log creation start with workspace name, branch, and repos
	if !strings.Contains(log, `creating workspace "log-ws"`) {
		t.Errorf("log should contain creation start, got:\n%s", log)
	}
	if !strings.Contains(log, "feat/log") {
		t.Errorf("log should contain branch name, got:\n%s", log)
	}
	if !strings.Contains(log, `workspace "log-ws" created`) {
		t.Errorf("log should contain creation success, got:\n%s", log)
	}

	// Should log branch creation for each repo
	if !strings.Contains(log, `creating branch "feat/log" in api`) {
		t.Errorf("log should contain branch creation for api, got:\n%s", log)
	}
	if !strings.Contains(log, `creating branch "feat/log" in web`) {
		t.Errorf("log should contain branch creation for web, got:\n%s", log)
	}

	// Delete workspace
	err = env.svc.Delete("log-ws")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	log = readLog()

	if !strings.Contains(log, `deleting workspace "log-ws"`) {
		t.Errorf("log should contain deletion start, got:\n%s", log)
	}
	if !strings.Contains(log, `workspace "log-ws" deleted`) {
		t.Errorf("log should contain deletion success, got:\n%s", log)
	}
	// Branch deletion should be logged
	if !strings.Contains(log, `deleted branch "feat/log"`) {
		t.Errorf("log should contain branch deletion, got:\n%s", log)
	}
}

func TestLoggingSync(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")

	err := env.createWorkspace("sync-log-ws", "feat/synclog", []string{"api"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = env.svc.Sync("sync-log-ws")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	log := readLog()
	if !strings.Contains(log, `syncing workspace "sync-log-ws"`) {
		t.Errorf("log should contain sync start, got:\n%s", log)
	}
}

func TestLoggingReset(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")

	err := env.createWorkspace("reset-log-ws", "feat/resetlog", []string{"api"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	err = env.svc.Reset("reset-log-ws", false)
	if err != nil {
		t.Fatalf("reset: %v", err)
	}

	log := readLog()
	if !strings.Contains(log, `resetting workspace "reset-log-ws"`) {
		t.Errorf("log should contain reset start, got:\n%s", log)
	}
}

func TestLoggingAddAndRemoveRepos(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	env.createWorkspace("addrem-ws", "feat/addrem", []string{"api"})

	err := env.svc.AddRepos("addrem-ws", []string{"web"}, env.repoMap)
	if err != nil {
		t.Fatalf("add-repo: %v", err)
	}

	log := readLog()
	if !strings.Contains(log, `added 1 repo(s) to workspace "addrem-ws"`) {
		t.Errorf("log should contain add-repo, got:\n%s", log)
	}

	err = env.svc.RemoveRepos("addrem-ws", []string{"web"})
	if err != nil {
		t.Fatalf("remove-repo: %v", err)
	}

	log = readLog()
	if !strings.Contains(log, `removed 1 repo(s) from workspace "addrem-ws"`) {
		t.Errorf("log should contain remove-repo, got:\n%s", log)
	}
}

func TestLoggingRename(t *testing.T) {
	readLog := setupLogging(t)
	env := setupTestEnv(t)
	env.createRepo("api")

	env.createWorkspace("old-name", "feat/rename", []string{"api"})

	err := env.svc.Rename("old-name", "new-name")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}

	log := readLog()
	if !strings.Contains(log, `workspace "old-name" renamed to "new-name"`) {
		t.Errorf("log should contain rename, got:\n%s", log)
	}
}
