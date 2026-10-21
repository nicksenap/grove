package workspace

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"syscall"

	"github.com/nicksenap/grove/internal/models"
)

// SortedRepos returns the workspace repos ordered by repo name.
func SortedRepos(ws *models.Workspace) []models.RepoWorktree {
	repos := append([]models.RepoWorktree(nil), ws.Repos...)
	sort.Slice(repos, func(i, j int) bool { return repos[i].RepoName < repos[j].RepoName })
	return repos
}

// Diff runs `git diff <args>` in every repo of ws, in repo-name order, with
// paths prefixed a/<repo>/ and b/<repo>/ so the concatenated stdout reads as
// one monorepo diff. Every repo runs even if an earlier one fails. It returns
// the highest git exit code: 1 means differences under --exit-code/--quiet,
// higher means a git error. Like git, once the reader of stdout is gone (the
// pager quit, or `| head` exited) it stops silently without counting a failure.
func Diff(ws *models.Workspace, args []string, stdout, stderr io.Writer) int {
	code := 0
	for _, r := range SortedRepos(ws) {
		gitArgs := append([]string{
			"--no-pager", "-C", r.WorktreePath, "diff",
			"--src-prefix=a/" + r.RepoName + "/",
			"--dst-prefix=b/" + r.RepoName + "/",
		}, args...)
		cmd := exec.Command("git", gitArgs...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		err := cmd.Run()
		if brokenPipe(err) {
			return code
		}
		code = max(code, diffExitCode(r.RepoName, err, stderr))
	}
	return code
}

func diffExitCode(repo string, err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() > 0 {
		return exitErr.ExitCode()
	}
	// Not started, or killed by a signal other than SIGPIPE.
	fmt.Fprintf(stderr, "gw diff: %s: %v\n", repo, err)
	return 1
}

// brokenPipe reports whether git was killed by SIGPIPE, i.e. stdout's reader exited.
func brokenPipe(err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	return ok && ws.Signaled() && ws.Signal() == syscall.SIGPIPE
}
