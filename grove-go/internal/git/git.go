// Package git wraps git subprocess calls.
package git

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/nicksenap/grove/internal/models"
)

// Error is returned when a git command fails.
type Error struct {
	Cmd    string
	Stderr string
}

func (e *Error) Error() string {
	return fmt.Sprintf("git %s: %s", e.Cmd, e.Stderr)
}

// run executes a git command in the given directory.
func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if err != nil {
		return result, &Error{Cmd: strings.Join(args, " "), Stderr: result}
	}
	return result, nil
}

// runSilent runs a git command, returning only stdout.
func runSilent(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	result := strings.TrimSpace(string(out))
	if err != nil {
		stderr := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		return result, &Error{Cmd: strings.Join(args, " "), Stderr: stderr}
	}
	return result, nil
}

// IsGitRepo checks if the given path is a git repository.
func IsGitRepo(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}

// RemoteURL returns the URL for the given remote.
func RemoteURL(repoPath, remote string) (string, error) {
	out, err := runSilent(repoPath, "remote", "get-url", remote)
	if err != nil {
		return "", err
	}
	return out, nil
}

var remoteNameRegexp = regexp.MustCompile(`[:/]([^/]+/[^/]+?)(?:\.git)?$`)

// ParseRemoteName extracts "owner/repo" from a git remote URL.
func ParseRemoteName(url string) string {
	m := remoteNameRegexp.FindStringSubmatch(url)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// BranchExists checks if a branch exists in the repo.
func BranchExists(repoPath, branch string) bool {
	_, err := runSilent(repoPath, "rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

// DefaultBranch detects the default branch (main or master).
func DefaultBranch(repoPath string) string {
	out, err := runSilent(repoPath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err == nil {
		parts := strings.Split(out, "/")
		return parts[len(parts)-1]
	}
	// Fallback: check for main, then master
	if BranchExists(repoPath, "main") {
		return "main"
	}
	return "master"
}

// CurrentBranch returns the current branch name.
func CurrentBranch(path string) (string, error) {
	return runSilent(path, "rev-parse", "--abbrev-ref", "HEAD")
}

// CreateBranch creates a new branch, optionally from a start point.
func CreateBranch(repoPath, branch, startPoint string) error {
	args := []string{"branch", branch}
	if startPoint != "" {
		args = append(args, startPoint)
	}
	_, err := run(repoPath, args...)
	return err
}

// WorktreeAdd creates a new git worktree.
func WorktreeAdd(repoPath, worktreePath, branch string) error {
	_, err := run(repoPath, "worktree", "add", worktreePath, branch)
	return err
}

// WorktreeRemove removes a git worktree.
func WorktreeRemove(repoPath, worktreePath string) error {
	_, err := run(repoPath, "worktree", "remove", "--force", worktreePath)
	return err
}

// WorktreeRepair repairs worktree linkage after moves.
func WorktreeRepair(repoPath, worktreePath string) error {
	_, err := run(repoPath, "worktree", "repair", worktreePath)
	return err
}

// WorktreeEntry represents a single worktree from porcelain output.
type WorktreeEntry struct {
	Worktree string
	HEAD     string
	Branch   string
	Bare     bool
	Detached bool
}

// WorktreeList returns all worktrees for a repo.
func WorktreeList(repoPath string) ([]WorktreeEntry, error) {
	out, err := runSilent(repoPath, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var entries []WorktreeEntry
	var current WorktreeEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Worktree != "" {
				entries = append(entries, current)
			}
			current = WorktreeEntry{}
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Worktree = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			current.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			branch := strings.TrimPrefix(line, "branch ")
			// Strip refs/heads/ prefix
			current.Branch = strings.TrimPrefix(branch, "refs/heads/")
		case line == "bare":
			current.Bare = true
		case line == "detached":
			current.Detached = true
		}
	}
	if current.Worktree != "" {
		entries = append(entries, current)
	}
	return entries, nil
}

// WorktreeHasBranch checks if any worktree already uses the given branch.
func WorktreeHasBranch(repoPath, branch string) bool {
	entries, err := WorktreeList(repoPath)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Branch == branch {
			return true
		}
	}
	return false
}

// Fetch runs git fetch in the repo.
func Fetch(repoPath string) error {
	_, err := run(repoPath, "fetch", "--quiet")
	return err
}

// RebaseOnto rebases the current branch onto the given base.
func RebaseOnto(path, base string) error {
	_, err := run(path, "rebase", base)
	return err
}

// RebaseAbort aborts an in-progress rebase.
func RebaseAbort(path string) error {
	_, err := run(path, "rebase", "--abort")
	return err
}

// CommitsAheadBehind returns (ahead, behind) counts relative to upstream.
func CommitsAheadBehind(path, upstream string) (int, int, error) {
	out, err := runSilent(path, "rev-list", "--left-right", "--count", upstream+"...HEAD")
	if err != nil {
		return 0, 0, err
	}
	parts := strings.Fields(out)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected rev-list output: %q", out)
	}
	behind, _ := strconv.Atoi(parts[0])
	ahead, _ := strconv.Atoi(parts[1])
	return ahead, behind, nil
}

// RepoStatus returns the short git status.
func RepoStatus(path string) (string, error) {
	return runSilent(path, "status", "--short")
}

// PRStatus returns the PR status from gh CLI (best-effort).
func PRStatus(path string) (map[string]string, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "number,title,state,url")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var result map[string]string
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// --- Per-repo .grove.toml ---

var (
	groveConfigCache sync.Map // path → *models.GroveToml
)

// ReadGroveConfig reads and caches .grove.toml from a repo.
func ReadGroveConfig(repoPath string) *models.GroveToml {
	configPath := filepath.Join(repoPath, ".grove.toml")

	if cached, ok := groveConfigCache.Load(configPath); ok {
		return cached.(*models.GroveToml)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	// Parse into a generic map first for flexible field handling
	var raw map[string]interface{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil
	}

	cfg := &models.GroveToml{}

	if v, ok := raw["base_branch"].(string); ok {
		cfg.BaseBranch = v
	}

	// Parse hook fields (can be string or []string)
	cfg.Setup = parseHookField(raw, "setup")
	cfg.Teardown = parseHookField(raw, "teardown")
	cfg.Run = parseHookField(raw, "run")
	cfg.PreSync = parseHookField(raw, "pre_sync")
	cfg.PostSync = parseHookField(raw, "post_sync")
	cfg.PreRun = parseHookField(raw, "pre_run")
	cfg.PostRun = parseHookField(raw, "post_run")

	groveConfigCache.Store(configPath, cfg)
	return cfg
}

func parseHookField(raw map[string]interface{}, key string) []string {
	v, ok := raw[key]
	if !ok {
		return nil
	}
	switch val := v.(type) {
	case string:
		return []string{val}
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// RepoBaseBranch reads the base_branch from .grove.toml.
func RepoBaseBranch(repoPath string) string {
	cfg := ReadGroveConfig(repoPath)
	if cfg != nil {
		return cfg.BaseBranch
	}
	return ""
}

// RepoHookCommands returns hook commands from .grove.toml.
func RepoHookCommands(sourceRepo, hook string) []string {
	cfg := ReadGroveConfig(sourceRepo)
	if cfg == nil {
		return nil
	}
	switch hook {
	case "setup":
		return cfg.Setup
	case "teardown":
		return cfg.Teardown
	case "run":
		return cfg.Run
	case "pre_sync":
		return cfg.PreSync
	case "post_sync":
		return cfg.PostSync
	case "pre_run":
		return cfg.PreRun
	case "post_run":
		return cfg.PostRun
	}
	return nil
}

// ResolveBaseBranch returns the base branch for a repo: .grove.toml > auto-detect.
func ResolveBaseBranch(repoPath string) string {
	if b := RepoBaseBranch(repoPath); b != "" {
		return b
	}
	return DefaultBranch(repoPath)
}
