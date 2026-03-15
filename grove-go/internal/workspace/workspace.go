// Package workspace implements worktree orchestration — the core business logic.
package workspace

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/claude"
	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/git"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
)

var log = slog.Default().With("pkg", "workspace")

// ParallelResult holds the outcome of a parallel operation on a single item.
type ParallelResult struct {
	Name   string
	Result interface{}
	Err    error
}

// parallel runs fn for each item concurrently, returning results in order.
func parallel(items []string, fn func(name string) (interface{}, error)) []ParallelResult {
	results := make([]ParallelResult, len(items))
	var wg sync.WaitGroup
	for i, name := range items {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			result, err := fn(name)
			results[i] = ParallelResult{Name: name, Result: result, Err: err}
		}(i, name)
	}
	wg.Wait()
	return results
}

// provisionWorktrees creates worktrees for the given repos.
func provisionWorktrees(repoPaths map[string]string, branch, workspacePath string, cfg *models.Config) ([]models.RepoWorktree, error) {
	repoNames := make([]string, 0, len(repoPaths))
	for name := range repoPaths {
		repoNames = append(repoNames, name)
	}

	// Validate no duplicate branches
	for name, path := range repoPaths {
		if git.WorktreeHasBranch(path, branch) {
			return nil, fmt.Errorf("repo %q already has a worktree on branch %q", name, branch)
		}
	}

	// Parallel fetch
	parallel(repoNames, func(name string) (interface{}, error) {
		return nil, git.Fetch(repoPaths[name])
	})

	// Create branches + worktrees
	var created []models.RepoWorktree
	for _, name := range repoNames {
		repoPath := repoPaths[name]
		wtPath := filepath.Join(workspacePath, name)

		// Create branch if it doesn't exist
		if !git.BranchExists(repoPath, branch) {
			base := git.ResolveBaseBranch(repoPath)
			startPoint := "origin/" + base
			if err := git.CreateBranch(repoPath, branch, startPoint); err != nil {
				// Rollback: remove already-created worktrees
				for _, wt := range created {
					git.WorktreeRemove(wt.SourceRepo, wt.WorktreePath)
				}
				return nil, fmt.Errorf("creating branch %q in %s: %w", branch, name, err)
			}
		}

		if err := git.WorktreeAdd(repoPath, wtPath, branch); err != nil {
			for _, wt := range created {
				git.WorktreeRemove(wt.SourceRepo, wt.WorktreePath)
			}
			return nil, fmt.Errorf("creating worktree for %s: %w", name, err)
		}

		created = append(created, models.RepoWorktree{
			RepoName:     name,
			SourceRepo:   repoPath,
			WorktreePath: wtPath,
			Branch:       branch,
		})
	}

	// Run setup hooks (parallel, best-effort)
	parallel(repoNames, func(name string) (interface{}, error) {
		path := repoPaths[name]
		cmds := git.RepoHookCommands(path, "setup")
		wtPath := filepath.Join(workspacePath, name)
		for _, cmd := range cmds {
			runHook(name, path, wtPath, cmd)
		}
		return nil, nil
	})

	return created, nil
}

// Create creates a new workspace.
func Create(name string, repoPaths map[string]string, branch string, cfg *models.Config) (*models.Workspace, error) {
	// Check for existing workspace
	existing, _ := state.GetWorkspace(name)
	if existing != nil {
		return nil, fmt.Errorf("workspace %q already exists", name)
	}

	wsPath := filepath.Join(cfg.WorkspaceDir, name)
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		return nil, fmt.Errorf("creating workspace dir: %w", err)
	}

	repos, err := provisionWorktrees(repoPaths, branch, wsPath, cfg)
	if err != nil {
		os.Remove(wsPath)
		return nil, err
	}

	ws := models.NewWorkspace(name, wsPath, branch, repos)
	if err := state.AddWorkspace(ws); err != nil {
		return nil, err
	}

	// Rehydrate Claude memory if enabled
	if cfg.ClaudeMemSync {
		for _, repo := range repos {
			claude.Rehydrate(repo.SourceRepo, repo.WorktreePath)
		}
	}

	return &ws, nil
}

// Delete removes a workspace and its worktrees.
func Delete(name string) (bool, error) {
	ws, err := state.GetWorkspace(name)
	if err != nil {
		return false, err
	}
	if ws == nil {
		return false, fmt.Errorf("workspace %q not found", name)
	}

	// Load config to check claude sync
	cfg, _ := config.Load()

	// Harvest Claude memory first
	if cfg != nil && cfg.ClaudeMemSync {
		for _, repo := range ws.Repos {
			claude.Harvest(repo.WorktreePath, repo.SourceRepo)
		}
	}

	// Parallel worktree removal
	repoNames := make([]string, len(ws.Repos))
	repoMap := make(map[string]models.RepoWorktree)
	for i, repo := range ws.Repos {
		repoNames[i] = repo.RepoName
		repoMap[repo.RepoName] = repo
	}

	results := parallel(repoNames, func(name string) (interface{}, error) {
		repo := repoMap[name]
		err := git.WorktreeRemove(repo.SourceRepo, repo.WorktreePath)
		if err != nil {
			// Force cleanup: remove the directory directly
			os.RemoveAll(repo.WorktreePath)
		}
		return nil, nil
	})

	// Check for errors (log but continue)
	for _, r := range results {
		if r.Err != nil {
			log.Warn("failed to remove worktree", "repo", r.Name, "error", r.Err)
		}
	}

	// Remove workspace directory
	os.RemoveAll(ws.Path)

	// Remove from state
	return true, state.RemoveWorkspace(name)
}

// AddRepo adds repos to an existing workspace.
func AddRepo(ws *models.Workspace, repoPaths map[string]string, cfg *models.Config) ([]models.RepoWorktree, error) {
	// Filter out already-present repos
	existing := make(map[string]bool)
	for _, repo := range ws.Repos {
		existing[repo.RepoName] = true
	}
	filtered := make(map[string]string)
	for name, path := range repoPaths {
		if !existing[name] {
			filtered[name] = path
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("all specified repos are already in the workspace")
	}

	repos, err := provisionWorktrees(filtered, ws.Branch, ws.Path, cfg)
	if err != nil {
		return nil, err
	}

	ws.Repos = append(ws.Repos, repos...)
	if err := state.UpdateWorkspace(*ws, ""); err != nil {
		return nil, err
	}

	// Rehydrate Claude memory
	if cfg.ClaudeMemSync {
		for _, repo := range repos {
			claude.Rehydrate(repo.SourceRepo, repo.WorktreePath)
		}
	}

	return repos, nil
}

// RemoveRepo removes repos from a workspace.
func RemoveRepo(ws *models.Workspace, repoNames []string, force bool, cfg *models.Config) error {
	toRemove := make(map[string]bool)
	for _, name := range repoNames {
		toRemove[name] = true
	}

	// Harvest Claude memory first
	if cfg != nil && cfg.ClaudeMemSync {
		for _, repo := range ws.Repos {
			if toRemove[repo.RepoName] {
				claude.Harvest(repo.WorktreePath, repo.SourceRepo)
			}
		}
	}

	// Remove worktrees (with teardown hooks)
	for _, repo := range ws.Repos {
		if !toRemove[repo.RepoName] {
			continue
		}
		// Run teardown hooks
		cmds := git.RepoHookCommands(repo.SourceRepo, "teardown")
		for _, cmd := range cmds {
			runHook(repo.RepoName, repo.SourceRepo, repo.WorktreePath, cmd)
		}
		if err := git.WorktreeRemove(repo.SourceRepo, repo.WorktreePath); err != nil {
			if !force {
				return fmt.Errorf("removing worktree for %s: %w", repo.RepoName, err)
			}
			os.RemoveAll(repo.WorktreePath)
		}
	}

	// Update state
	var remaining []models.RepoWorktree
	for _, repo := range ws.Repos {
		if !toRemove[repo.RepoName] {
			remaining = append(remaining, repo)
		}
	}
	ws.Repos = remaining
	return state.UpdateWorkspace(*ws, "")
}

// Rename renames a workspace.
func Rename(oldName, newName string, cfg *models.Config) error {
	ws, err := state.GetWorkspace(oldName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("workspace %q not found", oldName)
	}

	// Check new name isn't taken
	if existing, _ := state.GetWorkspace(newName); existing != nil {
		return fmt.Errorf("workspace %q already exists", newName)
	}

	oldPath := ws.Path
	newPath := filepath.Join(filepath.Dir(ws.Path), newName)

	// Update state first (easier rollback)
	ws.Name = newName
	ws.Path = newPath
	// Update worktree paths
	for i := range ws.Repos {
		ws.Repos[i].WorktreePath = filepath.Join(newPath, ws.Repos[i].RepoName)
	}
	if err := state.UpdateWorkspace(*ws, oldName); err != nil {
		return err
	}

	// Rename directory
	if err := os.Rename(oldPath, newPath); err != nil {
		// Rollback state
		ws.Name = oldName
		ws.Path = oldPath
		for i := range ws.Repos {
			ws.Repos[i].WorktreePath = filepath.Join(oldPath, ws.Repos[i].RepoName)
		}
		state.UpdateWorkspace(*ws, newName)
		return fmt.Errorf("renaming directory: %w", err)
	}

	// Repair git worktree linkage (best-effort)
	for _, repo := range ws.Repos {
		git.WorktreeRepair(repo.SourceRepo, repo.WorktreePath)
	}

	// Migrate Claude memory dirs
	if cfg.ClaudeMemSync {
		for _, repo := range ws.Repos {
			oldWT := filepath.Join(oldPath, repo.RepoName)
			claude.MigrateMemoryDir(oldWT, repo.WorktreePath)
		}
	}

	return nil
}

// StatusEntry holds status info for a single repo in a workspace.
type StatusEntry struct {
	Repo   string
	Branch string
	Status string
	Ahead  int
	Behind int
}

// Status collects git status for all repos in a workspace.
func Status(ws *models.Workspace) []StatusEntry {
	entries := make([]StatusEntry, len(ws.Repos))
	var wg sync.WaitGroup

	for i, repo := range ws.Repos {
		wg.Add(1)
		go func(i int, repo models.RepoWorktree) {
			defer wg.Done()
			entry := StatusEntry{
				Repo:   repo.RepoName,
				Branch: repo.Branch,
			}

			if status, err := git.RepoStatus(repo.WorktreePath); err == nil {
				entry.Status = status
			}

			base := git.ResolveBaseBranch(repo.SourceRepo)
			upstream := "origin/" + base
			ahead, behind, err := git.CommitsAheadBehind(repo.WorktreePath, upstream)
			if err == nil {
				entry.Ahead = ahead
				entry.Behind = behind
			}

			entries[i] = entry
		}(i, repo)
	}
	wg.Wait()
	return entries
}

// SyncResult holds the result of syncing a single repo.
type SyncResult struct {
	Repo   string
	Base   string
	Result string
}

// Sync rebases all repos onto their base branches.
func Sync(ws *models.Workspace) []SyncResult {
	results := make([]SyncResult, len(ws.Repos))
	var wg sync.WaitGroup

	for i, repo := range ws.Repos {
		wg.Add(1)
		go func(i int, repo models.RepoWorktree) {
			defer wg.Done()
			results[i] = syncOneRepo(repo)
		}(i, repo)
	}
	wg.Wait()
	return results
}

func syncOneRepo(repo models.RepoWorktree) SyncResult {
	base := git.ResolveBaseBranch(repo.SourceRepo)
	result := SyncResult{Repo: repo.RepoName, Base: base}

	// Fetch first
	if err := git.Fetch(repo.SourceRepo); err != nil {
		result.Result = fmt.Sprintf("fetch failed: %v", err)
		return result
	}

	// Check if dirty
	status, _ := git.RepoStatus(repo.WorktreePath)
	if strings.TrimSpace(status) != "" {
		result.Result = "skipped (dirty)"
		return result
	}

	// Check ahead/behind
	upstream := "origin/" + base
	ahead, behind, err := git.CommitsAheadBehind(repo.WorktreePath, upstream)
	if err != nil {
		result.Result = fmt.Sprintf("failed: %v", err)
		return result
	}

	if behind == 0 {
		if ahead == 0 {
			result.Result = "up to date"
		} else {
			result.Result = fmt.Sprintf("up to date (%d ahead)", ahead)
		}
		return result
	}

	// Run pre_sync hooks
	cmds := git.RepoHookCommands(repo.SourceRepo, "pre_sync")
	for _, cmd := range cmds {
		runHook(repo.RepoName, repo.SourceRepo, repo.WorktreePath, cmd)
	}

	// Rebase
	if err := git.RebaseOnto(repo.WorktreePath, upstream); err != nil {
		git.RebaseAbort(repo.WorktreePath)
		result.Result = "conflict (aborted rebase)"
		return result
	}

	// Run post_sync hooks
	cmds = git.RepoHookCommands(repo.SourceRepo, "post_sync")
	for _, cmd := range cmds {
		runHook(repo.RepoName, repo.SourceRepo, repo.WorktreePath, cmd)
	}

	result.Result = fmt.Sprintf("rebased (%d commits)", behind)
	return result
}

// SummaryEntry holds a summary for one workspace.
type SummaryEntry struct {
	Name   string
	Branch string
	Repos  int
	Status string
	Path   string
}

// AllWorkspacesSummary returns a summary of all workspaces.
func AllWorkspacesSummary() ([]SummaryEntry, error) {
	workspaces, err := state.LoadWorkspaces()
	if err != nil {
		return nil, err
	}

	entries := make([]SummaryEntry, len(workspaces))
	var wg sync.WaitGroup

	for i, ws := range workspaces {
		wg.Add(1)
		go func(i int, ws models.Workspace) {
			defer wg.Done()

			status := "ok"
			if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
				status = "missing"
			}

			entries[i] = SummaryEntry{
				Name:   ws.Name,
				Branch: ws.Branch,
				Repos:  len(ws.Repos),
				Status: status,
				Path:   ws.Path,
			}
		}(i, ws)
	}
	wg.Wait()
	return entries, nil
}

// Runnable returns repos that have a "run" hook configured.
func Runnable(ws *models.Workspace) []struct {
	Repo models.RepoWorktree
	Cmds []string
} {
	var result []struct {
		Repo models.RepoWorktree
		Cmds []string
	}
	for _, repo := range ws.Repos {
		cmds := git.RepoHookCommands(repo.SourceRepo, "run")
		if len(cmds) > 0 {
			result = append(result, struct {
				Repo models.RepoWorktree
				Cmds []string
			}{Repo: repo, Cmds: cmds})
		}
	}
	return result
}

// Diagnose checks for workspace issues.
func Diagnose(cfg *models.Config) []models.DoctorIssue {
	var issues []models.DoctorIssue

	workspaces, err := state.LoadWorkspaces()
	if err != nil {
		issues = append(issues, models.DoctorIssue{
			Level:   "error",
			Message: fmt.Sprintf("failed to load state: %v", err),
		})
		return issues
	}

	for _, ws := range workspaces {
		// Check workspace dir exists
		if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
			wsName := ws.Name
			issues = append(issues, models.DoctorIssue{
				Level:     "error",
				Message:   fmt.Sprintf("workspace %q directory missing: %s", ws.Name, ws.Path),
				Fix:       "remove stale state entry",
				Workspace: ws.Name,
				FixFunc: func() error {
					return state.RemoveWorkspace(wsName)
				},
			})
			continue
		}

		for _, repo := range ws.Repos {
			// Check source repo exists
			if _, err := os.Stat(repo.SourceRepo); os.IsNotExist(err) {
				issues = append(issues, models.DoctorIssue{
					Level:     "warning",
					Message:   fmt.Sprintf("[%s] source repo missing: %s", ws.Name, repo.SourceRepo),
					Workspace: ws.Name,
					RepoName:  repo.RepoName,
				})
			}

			// Check worktree dir exists
			if _, err := os.Stat(repo.WorktreePath); os.IsNotExist(err) {
				issues = append(issues, models.DoctorIssue{
					Level:     "error",
					Message:   fmt.Sprintf("[%s] worktree missing for %s: %s", ws.Name, repo.RepoName, repo.WorktreePath),
					Workspace: ws.Name,
					RepoName:  repo.RepoName,
				})
			}

			// Check git worktree registration
			entries, err := git.WorktreeList(repo.SourceRepo)
			if err == nil {
				found := false
				for _, e := range entries {
					if e.Worktree == repo.WorktreePath {
						found = true
						break
					}
				}
				if !found {
					issues = append(issues, models.DoctorIssue{
						Level:     "warning",
						Message:   fmt.Sprintf("[%s] worktree for %s not registered in git", ws.Name, repo.RepoName),
						Workspace: ws.Name,
						RepoName:  repo.RepoName,
					})
				}
			}
		}
	}

	// Check orphaned Claude memory dirs
	if cfg.ClaudeMemSync {
		var activePaths []string
		for _, ws := range workspaces {
			for _, repo := range ws.Repos {
				activePaths = append(activePaths, repo.WorktreePath)
			}
		}
		orphaned := claude.FindOrphanedMemoryDirs(activePaths)
		for _, dir := range orphaned {
			d := dir
			issues = append(issues, models.DoctorIssue{
				Level:   "warning",
				Message: fmt.Sprintf("orphaned Claude memory dir: %s", dir),
				Fix:     "remove orphaned directory",
				FixFunc: func() error {
					return os.RemoveAll(d)
				},
			})
		}
	}

	return issues
}

// FixIssues auto-fixes the given issues. Returns count of fixes applied.
func FixIssues(issues []models.DoctorIssue) int {
	count := 0
	for _, issue := range issues {
		if issue.FixFunc != nil {
			if err := issue.FixFunc(); err == nil {
				count++
			}
		}
	}
	return count
}

// runHook executes a shell command in the worktree directory (best-effort).
func runHook(repoName, sourceRepo, worktreePath, command string) {
	log.Debug("running hook", "repo", repoName, "cmd", command, "cwd", worktreePath)
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = worktreePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Warn("hook failed", "repo", repoName, "cmd", command, "error", err)
	}
}
