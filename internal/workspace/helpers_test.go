package workspace

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/stats"
)

// testEnv sets up isolated Grove dirs + real git repos for testing.
type testEnv struct {
	t        *testing.T
	dir      string
	reposDir string
	wsDir    string
	groveDir string
	cfg      *models.Config
	repoMap  map[string]string
	svc      *Service
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()
	dir := t.TempDir()

	groveDir := filepath.Join(dir, ".grove")
	wsDir := filepath.Join(groveDir, "workspaces")
	reposDir := filepath.Join(dir, "repos")
	os.MkdirAll(groveDir, 0o755)
	os.MkdirAll(wsDir, 0o755)
	os.MkdirAll(reposDir, 0o755)

	store := state.NewStore(groveDir)
	os.WriteFile(store.Path, []byte("[]"), 0o644)

	cfg := &models.Config{
		RepoDirs:     []string{reposDir},
		WorkspaceDir: wsDir,
		Presets:      map[string]models.Preset{},
	}

	svc := &Service{
		State:        store,
		Stats:        &stats.Tracker{StatsPath: filepath.Join(groveDir, "stats.json"), NowFn: time.Now},
		RunCmd:       prodRunCmd,
		RunCmdSilent: prodRunCmdSilent,
	}

	return &testEnv{
		t:        t,
		dir:      dir,
		groveDir: groveDir,
		reposDir: reposDir,
		wsDir:    wsDir,
		cfg:      cfg,
		repoMap:  make(map[string]string),
		svc:      svc,
	}
}

func (e *testEnv) createWorkspace(name, branch string, repoNames []string) error {
	e.t.Helper()
	return e.svc.CreateWithOpts(name, CreateOpts{
		Branch:  branch,
		Repos:   repoNames,
		RepoMap: e.repoMap,
		Cfg:     e.cfg,
	})
}

// createRepo creates a real git repo with an initial commit.
func (e *testEnv) createRepo(name string) string {
	e.t.Helper()
	repoPath := filepath.Join(e.reposDir, name)
	e.run(e.reposDir, "git", "init", "-q", repoPath)
	e.run(repoPath, "git", "config", "user.email", "test@test.com")
	e.run(repoPath, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# "+name), 0o644)
	e.run(repoPath, "git", "add", ".")
	e.run(repoPath, "git", "commit", "-q", "-m", "initial")
	e.repoMap[name] = repoPath
	return repoPath
}

// createRepoWithRemote creates a repo cloned from a bare origin (for sync tests).
func (e *testEnv) createRepoWithRemote(name string) string {
	e.t.Helper()
	bare := filepath.Join(e.reposDir, name+"-origin.git")
	clone := filepath.Join(e.reposDir, name)

	e.run(e.reposDir, "git", "init", "-q", "--bare", bare)
	e.run(e.reposDir, "git", "clone", "-q", bare, clone)
	e.run(clone, "git", "config", "user.email", "test@test.com")
	e.run(clone, "git", "config", "user.name", "Test")
	os.WriteFile(filepath.Join(clone, "README.md"), []byte("# "+name), 0o644)
	e.run(clone, "git", "add", ".")
	e.run(clone, "git", "commit", "-q", "-m", "initial")
	e.run(clone, "git", "push", "-q", "origin", "HEAD")
	e.repoMap[name] = clone
	return clone
}

func (e *testEnv) run(dir string, name string, args ...string) string {
	e.t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	// Filter out GIT_DIR/GIT_WORK_TREE/GIT_INDEX_FILE to prevent leaking
	// from parent process (e.g. when tests run inside a pre-commit hook).
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GIT_DIR=") ||
			strings.HasPrefix(kv, "GIT_WORK_TREE=") ||
			strings.HasPrefix(kv, "GIT_INDEX_FILE=") {
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("%s %s failed: %s\n%s", name, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// pushRemoteBranch creates a branch in repo, commits a marker file, pushes it to
// origin, then deletes the local branch — leaving only origin/<branch>. This
// simulates a PR head branch that exists on the remote but not locally.
func (e *testEnv) pushRemoteBranch(repo, branch, marker string) {
	e.t.Helper()
	e.run(repo, "git", "checkout", "-q", "-b", branch)
	os.WriteFile(filepath.Join(repo, marker), []byte(marker), 0o644)
	e.run(repo, "git", "add", ".")
	e.run(repo, "git", "commit", "-q", "-m", "remote work on "+branch)
	e.run(repo, "git", "push", "-q", "origin", branch)
	e.run(repo, "git", "checkout", "-q", "-")
	e.run(repo, "git", "branch", "-q", "-D", branch)
	// Forget the just-pushed ref locally so creation must rely on fetch.
	e.run(repo, "git", "update-ref", "-d", "refs/remotes/origin/"+branch)
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("capture stderr: %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		w.Close()
		r.Close()
	}()

	fn()
	if err := w.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return buf.String()
}

func setupLogging(t *testing.T) func() string {
	t.Helper()
	dir := t.TempDir()
	logging.LogDir = dir
	logging.Setup(false) // non-verbose: Info/Warn/Error still written

	return func() string {
		data, err := os.ReadFile(filepath.Join(dir, "grove.log"))
		if err != nil {
			t.Fatalf("reading log file: %v", err)
		}
		return string(data)
	}
}
