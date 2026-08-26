package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const defaultGWTimeout = 45 * time.Second

type env struct {
	t        *testing.T
	home     string
	reposDir string
	groveDir string
	wsDir    string
	timeout  time.Duration
}

type cmdResult struct {
	args   []string
	stdout string
	stderr string
	code   int
	err    error
}

type workspaceJSON struct {
	Name      string           `json:"name"`
	Path      string           `json:"path"`
	Branch    string           `json:"branch"`
	CreatedAt string           `json:"created_at"`
	Repos     []repoWorktree   `json:"repos"`
	Source    *workspaceSource `json:"source"`
}

type repoWorktree struct {
	RepoName     string `json:"repo_name"`
	SourceRepo   string `json:"source_repo"`
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
}

type workspaceSource struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
	Ref      string `json:"ref"`
	Title    string `json:"title"`
}

type repoEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Remote      string `json:"remote"`
	DisplayName string `json:"display_name"`
}

type doctorIssue struct {
	Workspace       string  `json:"workspace"`
	Repo            *string `json:"repo"`
	Issue           string  `json:"issue"`
	SuggestedAction string  `json:"suggested_action"`
}

type statusJSON struct {
	Workspace string           `json:"workspace"`
	Path      string           `json:"path"`
	Source    *workspaceSource `json:"source"`
}

type presetJSON struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}

type pluginJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type recipeCreateJSON struct {
	Created bool             `json:"created"`
	Name    string           `json:"name"`
	Jobs    []recipeJobJSON  `json:"jobs"`
	Error   *recipeErrorJSON `json:"error"`
}

type recipeJobJSON struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type recipeErrorJSON struct {
	Code string `json:"code"`
	Job  string `json:"job"`
	Step int    `json:"step"`
}

func newEnv(t *testing.T) *env {
	t.Helper()
	home := t.TempDir()
	reposDir := filepath.Join(home, "repos")
	groveDir := filepath.Join(home, ".grove")
	wsDir := filepath.Join(groveDir, "workspaces")
	if err := os.MkdirAll(reposDir, 0o755); err != nil {
		t.Fatalf("mkdir repos: %v", err)
	}
	e := &env{
		t:        t,
		home:     home,
		reposDir: reposDir,
		groveDir: groveDir,
		wsDir:    wsDir,
		timeout:  defaultGWTimeout,
	}
	e.git(home, "config", "--global", "user.email", "e2e@grove.test")
	e.git(home, "config", "--global", "user.name", "Grove E2E")
	e.git(home, "config", "--global", "init.defaultBranch", "main")
	return e
}

func (e *env) init() {
	e.t.Helper()
	e.mustGW("init", e.reposDir)
}

func (e *env) cmdEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+4)
	for _, kv := range env {
		switch {
		case strings.HasPrefix(kv, "HOME="),
			strings.HasPrefix(kv, "GIT_DIR="),
			strings.HasPrefix(kv, "GIT_WORK_TREE="),
			strings.HasPrefix(kv, "GIT_INDEX_FILE="),
			strings.HasPrefix(kv, "GIT_CEILING_DIRECTORIES="),
			strings.HasPrefix(kv, "ZELLIJ_SESSION_NAME="):
			continue
		}
		out = append(out, kv)
	}
	out = append(out,
		"HOME="+e.home,
		"GIT_CEILING_DIRECTORIES=/",
		"GIT_CONFIG_NOSYSTEM=1",
	)
	return out
}

func (e *env) runGW(dir string, args ...string) cmdResult {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, gwBin, args...)
	if dir != "" {
		cmd.Dir = dir
	} else {
		cmd.Dir = e.home
	}
	cmd.Env = e.cmdEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := cmdResult{
		args:   args,
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
	if err == nil {
		return res
	}
	if ctx.Err() == context.DeadlineExceeded {
		res.code = -1
		return res
	}
	if ee, ok := err.(*exec.ExitError); ok {
		res.code = ee.ExitCode()
		return res
	}
	res.code = -1
	return res
}

func (e *env) gw(args ...string) cmdResult {
	e.t.Helper()
	return e.runGW(e.home, args...)
}

func (e *env) gwIn(dir string, args ...string) cmdResult {
	e.t.Helper()
	return e.runGW(dir, args...)
}

func (e *env) mustGW(args ...string) cmdResult {
	e.t.Helper()
	res := e.gw(args...)
	res.must(e.t)
	return res
}

func (e *env) mustGWIn(dir string, args ...string) cmdResult {
	e.t.Helper()
	res := e.gwIn(dir, args...)
	res.must(e.t)
	return res
}

func (r cmdResult) must(t *testing.T) cmdResult {
	t.Helper()
	if r.code != 0 {
		t.Fatalf("gw %s: exit %d\nstdout:\n%s\nstderr:\n%s\nerr: %v",
			strings.Join(r.args, " "), r.code, r.stdout, r.stderr, r.err)
	}
	return r
}

func (r cmdResult) mustFail(t *testing.T) cmdResult {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("gw %s: expected non-zero exit\nstdout:\n%s\nstderr:\n%s",
			strings.Join(r.args, " "), r.stdout, r.stderr)
	}
	return r
}

func (r cmdResult) combined() string {
	return r.stdout + r.stderr
}

func decodeJSON[T any](t *testing.T, raw string) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, raw)
	}
	return v
}

func (e *env) listWorkspaces() []workspaceJSON {
	e.t.Helper()
	res := e.mustGW("list", "--json")
	s := strings.TrimSpace(res.stdout)
	if s == "" || s == "null" {
		return nil
	}
	return decodeJSON[[]workspaceJSON](e.t, res.stdout)
}

func (e *env) showWorkspace(name string) workspaceJSON {
	e.t.Helper()
	res := e.mustGW("ws", "show", name, "--json")
	return decodeJSON[workspaceJSON](e.t, res.stdout)
}

func (e *env) doctorIssues() []doctorIssue {
	e.t.Helper()
	res := e.mustGW("doctor", "--json")
	s := strings.TrimSpace(res.stdout)
	if s == "" || s == "null" {
		return nil
	}
	return decodeJSON[[]doctorIssue](e.t, res.stdout)
}

func (e *env) workspacePath(name string) string {
	return filepath.Join(e.wsDir, name)
}

func (e *env) worktree(workspace, repo string) string {
	return filepath.Join(e.wsDir, workspace, repo)
}

func (e *env) writeConfig(extra string) {
	e.t.Helper()
	if err := os.MkdirAll(e.groveDir, 0o755); err != nil {
		e.t.Fatalf("mkdir grove dir: %v", err)
	}
	body := "repo_dirs = [" + strconv.Quote(e.reposDir) + "]\nworkspace_dir = " + strconv.Quote(e.wsDir) + "\n"
	if extra != "" {
		body += "\n" + extra
	}
	e.writeFile(filepath.Join(e.groveDir, "config.toml"), body)
}

func (e *env) writeFile(path, contents string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		e.t.Fatalf("write %s: %v", path, err)
	}
}

func (e *env) requireExists(path string) {
	e.t.Helper()
	if _, err := os.Stat(path); err != nil {
		e.t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func (e *env) requireMissing(path string) {
	e.t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		e.t.Fatalf("expected %s to be absent, stat err=%v", path, err)
	}
}

func (e *env) requireContains(got, want, label string) {
	e.t.Helper()
	if !strings.Contains(got, want) {
		e.t.Fatalf("%s: expected %q in:\n%s", label, want, got)
	}
}

func workspaceNamed(list []workspaceJSON, name string) *workspaceJSON {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}
