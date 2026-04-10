package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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

// isAuthError checks if git output indicates an authentication failure.
func isAuthError(output string) bool {
	lower := strings.ToLower(output)
	patterns := []string{
		"permission denied (publickey",
		"host key verification failed",
		"terminal prompts disabled",
		"could not read from remote repository",
	}
	for _, p := range patterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// gitEnv is computed once at init and reused for every runGit call.
// This avoids copying os.Environ() and scanning it on every git invocation.
var gitEnv []string

// devNull is opened once and kept for the process lifetime (read-only, safe to share).
var devNull *os.File

func init() {
	env := os.Environ()
	hasSSHCmd := false
	for _, e := range env {
		if strings.HasPrefix(e, "GIT_SSH_COMMAND=") {
			hasSSHCmd = true
			break
		}
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0")
	if !hasSSHCmd {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	gitEnv = env

	devNull, _ = os.Open(os.DevNull)
}

// runGit executes a git command in the given directory.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv
	cmd.Stdin = devNull

	out, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(out))
	if err != nil {
		if isAuthError(output) {
			return output, &GitError{
				Cmd: strings.Join(args, " "),
				Stderr: output + "\n\nHint: Check your SSH keys or credentials. " +
					"Run 'ssh -T git@github.com' to test your connection.",
			}
		}
		return output, &GitError{
			Cmd:    strings.Join(args, " "),
			Stderr: output,
		}
	}
	return output, nil
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
// Returns an error if the default branch cannot be determined.
func DefaultBranch(repo string) (string, error) {
	out, err := runGit(repo, "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	if err == nil {
		// Returns "origin/main" — strip the "origin/" prefix
		parts := strings.SplitN(out, "/", 2)
		if len(parts) == 2 {
			return parts[1], nil
		}
		return out, nil
	}

	// Fallback: probe common branch names
	for _, branch := range []string{"main", "master"} {
		if _, err := runGit(repo, "rev-parse", "--verify", "refs/remotes/origin/"+branch); err == nil {
			return branch, nil
		}
	}
	return "", &GitError{Cmd: "default-branch", Stderr: "could not determine default branch"}
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

// RemoteURL returns the URL for a remote. Returns "" on any error.
func RemoteURL(path, remote string) string {
	out, err := runGit(path, "remote", "get-url", remote)
	if err != nil {
		return ""
	}
	return out
}

// ParseRemoteName extracts "owner/repo" from an SSH or HTTPS git URL.
// Returns "" if the URL cannot be parsed.
func ParseRemoteName(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}

	// SSH: git@github.com:owner/repo.git
	if strings.Contains(url, ":") && !strings.Contains(url, "://") {
		parts := strings.SplitN(url, ":", 2)
		if len(parts) == 2 {
			path := strings.TrimSuffix(parts[1], ".git")
			path = strings.TrimPrefix(path, "/")
			return path
		}
	}

	// HTTPS: https://github.com/owner/repo.git
	if strings.Contains(url, "://") {
		// Remove scheme + host
		idx := strings.Index(url, "://")
		rest := url[idx+3:]
		slashIdx := strings.Index(rest, "/")
		if slashIdx >= 0 {
			path := rest[slashIdx+1:]
			path = strings.TrimSuffix(path, ".git")
			path = strings.TrimPrefix(path, "/")
			return path
		}
	}

	return ""
}

// IsGitURL returns true if s looks like a remote git URL (HTTPS, SSH, or file://).
func IsGitURL(s string) bool {
	// HTTPS/HTTP: https://github.com/owner/repo.git
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		return true
	}
	// file:// protocol (local bare repos, testing)
	if strings.HasPrefix(s, "file://") {
		return true
	}
	// SSH: git@github.com:owner/repo.git
	if strings.Contains(s, ":") && !strings.Contains(s, "://") && strings.Contains(s, "@") {
		return true
	}
	return false
}

// RepoNameFromURL extracts the repository name from a git URL.
// e.g. "https://github.com/owner/my-repo.git" → "my-repo"
func RepoNameFromURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")

	// Take the last path segment
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	// SSH: git@host:owner/repo
	if i := strings.LastIndex(url, ":"); i >= 0 {
		part := url[i+1:]
		if j := strings.LastIndex(part, "/"); j >= 0 {
			return part[j+1:]
		}
		return part
	}
	return ""
}

// Clone clones a remote repository into destDir/<repo-name>.
// Returns the path to the cloned repository.
func Clone(url, destDir string) (string, error) {
	name := RepoNameFromURL(url)
	if name == "" {
		return "", fmt.Errorf("cannot determine repo name from URL: %s", url)
	}
	dest := filepath.Join(destDir, name)

	if _, err := os.Stat(dest); err == nil {
		// Already exists — verify it's a git repo
		if IsGitRepo(dest) {
			return dest, nil
		}
		return "", fmt.Errorf("directory %s already exists but is not a git repo", dest)
	}

	_, err := runGit(destDir, "clone", url, name)
	if err != nil {
		return "", fmt.Errorf("cloning %s: %w", url, err)
	}
	return dest, nil
}

// RepoBaseBranch returns "origin/<base>" from .grove.toml, or "".
func RepoBaseBranch(repo string) string {
	cfg, err := ReadGroveConfig(repo)
	if err != nil || cfg == nil || cfg.BaseBranch == "" {
		return ""
	}
	return "origin/" + cfg.BaseBranch
}

// ResolveBaseBranch returns the base branch for rebasing:
// .grove.toml base_branch > default branch > error.
func ResolveBaseBranch(repo string) (string, error) {
	if base := RepoBaseBranch(repo); base != "" {
		return base, nil
	}
	branch, err := DefaultBranch(repo)
	if err != nil {
		return "", err
	}
	return "origin/" + branch, nil
}

// PRInfo holds merge/pull request status.
type PRInfo struct {
	Number         int    `json:"number"`
	State          string `json:"state"`          // OPEN, MERGED, CLOSED
	ReviewDecision string `json:"reviewDecision"` // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	Provider       string `json:"-"`              // "github" or "gitlab"
}

// PRStatus returns PR/MR info for the current branch, or nil if unavailable.
// Detects GitHub (gh) or GitLab (glab) based on the remote URL.
func PRStatus(path string) *PRInfo {
	remote := RemoteURL(path, "origin")

	// Try GitLab if remote looks like gitlab
	if strings.Contains(remote, "gitlab") {
		if info := glabMRStatus(path); info != nil {
			return info
		}
	}

	// Try GitHub
	if info := ghPRStatus(path); info != nil {
		return info
	}

	// Fallback: try glab even if remote didn't match
	if !strings.Contains(remote, "gitlab") {
		return glabMRStatus(path)
	}

	return nil
}

func ghPRStatus(path string) *PRInfo {
	ghPath, err := exec.LookPath("gh")
	if err != nil {
		return nil
	}
	cmd := exec.Command(ghPath, "pr", "view", "--json", "number,state,reviewDecision")
	cmd.Dir = path
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var info PRInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil
	}
	info.Provider = "github"
	return &info
}

func glabMRStatus(path string) *PRInfo {
	glabPath, err := exec.LookPath("glab")
	if err != nil {
		return nil
	}
	cmd := exec.Command(glabPath, "mr", "view", "--output", "json")
	cmd.Dir = path
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	// glab uses different field names
	var raw struct {
		IID   int    `json:"iid"`
		State string `json:"state"` // "opened", "merged", "closed"
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil
	}

	// Normalize state to match GitHub conventions
	state := strings.ToUpper(raw.State)
	if state == "OPENED" {
		state = "OPEN"
	}

	return &PRInfo{
		Number:   raw.IID,
		State:    state,
		Provider: "gitlab",
	}
}

// groveConfigCache caches .grove.toml reads per-process.
// A map+RWMutex is faster than sync.Map for this low-contention, read-heavy pattern.
var (
	groveConfigMu    sync.RWMutex
	groveConfigCache = make(map[string]*groveConfigEntry)
)

type groveConfigEntry struct {
	cfg *models.GroveConfig
	err error
}

// ReadGroveConfig reads a .grove.toml from a repo directory.
// Results are cached per-process (never re-read within a single command).
func ReadGroveConfig(repoPath string) (*models.GroveConfig, error) {
	groveConfigMu.RLock()
	entry, ok := groveConfigCache[repoPath]
	groveConfigMu.RUnlock()
	if ok {
		return entry.cfg, entry.err
	}

	cfg, err := readGroveConfigFromDisk(repoPath)
	groveConfigMu.Lock()
	groveConfigCache[repoPath] = &groveConfigEntry{cfg: cfg, err: err}
	groveConfigMu.Unlock()
	return cfg, err
}

// ClearGroveConfigCache clears the cache. Call in tests.
func ClearGroveConfigCache() {
	groveConfigMu.Lock()
	groveConfigCache = make(map[string]*groveConfigEntry)
	groveConfigMu.Unlock()
}

func readGroveConfigFromDisk(repoPath string) (*models.GroveConfig, error) {
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
