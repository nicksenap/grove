package gitops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/nicksenap/grove/internal/models"
)

// GitError represents a git command failure.
type GitError struct {
	Cmd    string
	Stderr string
}

func (e *GitError) Error() string {
	return fmt.Sprintf("git %s: %s", e.Cmd, e.Stderr)
}

// runGit executes a git command in the given directory.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	cmd.Stdin = nil

	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		return output, &GitError{
			Cmd:    strings.Join(args, " "),
			Stderr: output,
		}
	}
	return output, nil
}

// runGitSeparate runs git and returns stdout and stderr separately.
func runGitSeparate(dir string, args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	cmd.Stdin = nil

	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err = cmd.Run()
	stdout = strings.TrimSpace(outBuf.String())
	stderr = strings.TrimSpace(errBuf.String())
	if err != nil {
		err = &GitError{
			Cmd:    strings.Join(args, " "),
			Stderr: stderr,
		}
	}
	return
}

// IsGitRepo checks if a directory is a git repository.
func IsGitRepo(path string) bool {
	_, err := runGit(path, "rev-parse", "--git-dir")
	return err == nil
}

// Fetch fetches all remotes.
func Fetch(repo string) error {
	_, err := runGit(repo, "fetch", "--all", "--quiet")
	return err
}

// DefaultBranch returns the default branch name for a repo.
func DefaultBranch(repo string) string {
	out, err := runGit(repo, "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	if err == nil {
		// Returns "origin/main" — strip the "origin/" prefix
		parts := strings.SplitN(out, "/", 2)
		if len(parts) == 2 {
			return parts[1]
		}
		return out
	}

	// Fallback: probe common branch names
	for _, branch := range []string{"main", "master"} {
		if _, err := runGit(repo, "rev-parse", "--verify", "refs/remotes/origin/"+branch); err == nil {
			return branch
		}
	}
	return "main"
}

// BranchExists checks if a local branch exists.
func BranchExists(repo, branch string) bool {
	_, err := runGit(repo, "rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

// CreateBranch creates a new branch at the given start point.
func CreateBranch(repo, branch, startPoint string) error {
	args := []string{"branch", branch}
	if startPoint != "" {
		args = append(args, startPoint)
	}
	_, err := runGit(repo, args...)
	return err
}

// DeleteBranch deletes a local branch. force uses -D instead of -d.
func DeleteBranch(repo, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := runGit(repo, "branch", flag, branch)
	return err
}

// WorktreeAdd creates a worktree for the given branch.
func WorktreeAdd(repo, path, branch string) error {
	_, err := runGit(repo, "worktree", "add", path, branch)
	return err
}

// WorktreeRemove removes a worktree.
func WorktreeRemove(repo, path string) error {
	_, err := runGit(repo, "worktree", "remove", path, "--force")
	return err
}

// WorktreeRepair repairs worktree references.
func WorktreeRepair(repo, path string) error {
	_, err := runGit(repo, "worktree", "repair", path)
	return err
}

// WorktreeEntry represents a parsed worktree list entry.
type WorktreeEntry struct {
	Path   string
	Branch string
}

// WorktreeList returns all worktrees for a repo.
func WorktreeList(repo string) ([]WorktreeEntry, error) {
	out, err := runGit(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var entries []WorktreeEntry
	var current WorktreeEntry

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				entries = append(entries, current)
			}
			current = WorktreeEntry{}
			continue
		}
		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch refs/heads/") {
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	if current.Path != "" {
		entries = append(entries, current)
	}
	return entries, nil
}

// WorktreeHasBranch checks if any worktree uses the given branch.
func WorktreeHasBranch(repo, branch string) (bool, error) {
	entries, err := WorktreeList(repo)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.Branch == branch {
			return true, nil
		}
	}
	return false, nil
}

// RepoStatus returns git status --short output.
func RepoStatus(path string) (string, error) {
	return runGit(path, "status", "--short")
}

// CurrentBranch returns the current branch name.
func CurrentBranch(path string) (string, error) {
	return runGit(path, "branch", "--show-current")
}

// RebaseOnto rebases the current branch onto the given base.
func RebaseOnto(path, base string) error {
	_, err := runGit(path, "rebase", base)
	return err
}

// RebaseAbort aborts an in-progress rebase.
func RebaseAbort(path string) error {
	_, err := runGit(path, "rebase", "--abort")
	return err
}

// CommitsAheadBehind returns (ahead, behind) relative to upstream.
func CommitsAheadBehind(path, upstream string) (int, int, error) {
	out, err := runGit(path, "rev-list", "--left-right", "--count", upstream+"...HEAD")
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output: %s", out)
	}
	behind, err1 := strconv.Atoi(parts[0])
	ahead, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("parsing rev-list counts: %s", out)
	}
	return ahead, behind, nil
}

// RemoteURL returns the URL for a remote.
func RemoteURL(path, remote string) (string, error) {
	return runGit(path, "remote", "get-url", remote)
}

// ReadGroveConfig reads a .grove.toml from a repo directory.
func ReadGroveConfig(repoPath string) (*models.GroveConfig, error) {
	cfgPath := filepath.Join(repoPath, ".grove.toml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cfg models.GroveConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", cfgPath, err)
	}
	return &cfg, nil
}
