package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (e *env) git(dir string, args ...string) string {
	e.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = e.cmdEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (e *env) gitAllowFail(dir string, args ...string) (string, error) {
	e.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = e.cmdEnv()
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (e *env) createRepo(name string) string {
	e.t.Helper()
	path := filepath.Join(e.reposDir, name)
	e.git(e.home, "init", "-q", path)
	e.git(path, "commit", "--allow-empty", "-q", "-m", "initial commit")
	return path
}

func (e *env) createRepoWithHistory(name string, commits int) string {
	e.t.Helper()
	origin := filepath.Join(e.home, name+"-origin.git")
	path := filepath.Join(e.reposDir, name)
	e.git(e.home, "init", "-q", "--bare", origin)
	e.git(e.home, "clone", "-q", origin, path)
	for i := 1; i <= commits; i++ {
		e.writeFile(filepath.Join(path, "README.md"), strings.Repeat("v\n", i))
		e.git(path, "add", ".")
		e.git(path, "commit", "-q", "-m", commitMessage(i))
	}
	e.git(path, "push", "-q", "origin", "HEAD")
	return path
}

func commitMessage(i int) string {
	switch i {
	case 1:
		return "first"
	case 2:
		return "second"
	case 3:
		return "third"
	default:
		return "commit"
	}
}

func (e *env) addGroveTOML(repo, contents string) {
	e.t.Helper()
	path := filepath.Join(e.reposDir, repo)
	e.writeFile(filepath.Join(path, ".grove.toml"), contents)
	e.git(path, "add", ".grove.toml")
	e.git(path, "commit", "-q", "-m", "add grove config")
}

func (e *env) currentBranch(path string) string {
	e.t.Helper()
	return e.git(path, "branch", "--show-current")
}

func (e *env) upstream(path string) string {
	e.t.Helper()
	out, err := e.gitAllowFail(path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "none"
	}
	return out
}

func (e *env) branchExists(repo, branch string) bool {
	e.t.Helper()
	out := e.git(repo, "branch", "--list", branch)
	return strings.TrimSpace(out) != ""
}

func (e *env) commitsBehind(worktree, upstream string) string {
	e.t.Helper()
	out, err := e.gitAllowFail(worktree, "rev-list", "--count", "HEAD.."+upstream)
	if err != nil {
		return "?"
	}
	return out
}

func (e *env) createBareOrigin(name string) string {
	e.t.Helper()
	origin := filepath.Join(e.home, name)
	e.git(e.home, "init", "-q", "--bare", origin)
	return origin
}

func (e *env) seedBareOrigin(origin, contents string) {
	e.t.Helper()
	tmp := filepath.Join(e.home, filepath.Base(origin)+"-seed")
	e.git(e.home, "clone", "-q", origin, tmp)
	e.writeFile(filepath.Join(tmp, "README.md"), contents)
	e.git(tmp, "add", ".")
	e.git(tmp, "commit", "-q", "-m", "initial")
	e.git(tmp, "push", "-q", "origin", "HEAD")
	if err := os.RemoveAll(tmp); err != nil {
		e.t.Fatalf("cleanup seed clone: %v", err)
	}
}

func (e *env) pushRemoteBranch(repo, branch, marker string) {
	e.t.Helper()
	e.git(repo, "checkout", "-q", "-b", branch)
	e.writeFile(filepath.Join(repo, marker), marker)
	e.git(repo, "add", ".")
	e.git(repo, "commit", "-q", "-m", "remote work on "+branch)
	e.git(repo, "push", "-q", "origin", branch)
	e.git(repo, "checkout", "-q", "-")
	e.git(repo, "branch", "-q", "-D", branch)
	e.git(repo, "update-ref", "-d", "refs/remotes/origin/"+branch)
}

func fileURL(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "file://" + path
	}
	return "file://" + abs
}
