package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/stats"
)

// Create creates a new workspace with worktrees for the given repos.
// repoMap is name→source_path.
func Create(name, branch string, repoNames []string, repoMap map[string]string, cfg *models.Config) error {
	// Check duplicate
	existing, err := state.GetWorkspace(name)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("Workspace %s already exists", name)
	}

	wsPath := filepath.Join(cfg.WorkspaceDir, name)
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		return fmt.Errorf("creating workspace dir: %w", err)
	}

	ws := models.NewWorkspace(name, wsPath, branch)

	// Provision worktrees
	var created []models.RepoWorktree
	for _, repoName := range repoNames {
		sourcePath, ok := repoMap[repoName]
		if !ok {
			rollback(created)
			os.RemoveAll(wsPath)
			return fmt.Errorf("repo %s not found", repoName)
		}

		rw, err := provisionWorktree(sourcePath, repoName, wsPath, branch)
		if err != nil {
			rollback(created)
			os.RemoveAll(wsPath)
			return fmt.Errorf("provisioning %s: %w", repoName, err)
		}
		created = append(created, *rw)
	}

	ws.Repos = created

	// Run setup hooks (parallel)
	runSetupHooks(ws)

	// Save state
	if err := state.AddWorkspace(ws); err != nil {
		rollback(created)
		os.RemoveAll(wsPath)
		return err
	}

	// Record stats
	stats.RecordCreated(ws)

	// Write .mcp.json
	writeMCPConfig(ws)

	console.Successf("Workspace %s created at %s", name, wsPath)

	// Write GROVE_CD_FILE if set
	if cdFile := os.Getenv("GROVE_CD_FILE"); cdFile != "" {
		os.WriteFile(cdFile, []byte(wsPath), 0o644)
	}

	return nil
}

func provisionWorktree(sourcePath, repoName, wsPath, branch string) (*models.RepoWorktree, error) {
	wtPath := filepath.Join(wsPath, repoName)

	// Fetch
	_ = gitops.Fetch(sourcePath)

	// Determine base branch
	baseBranch := gitops.DefaultBranch(sourcePath)
	groveCfg, _ := gitops.ReadGroveConfig(sourcePath)
	if groveCfg != nil && groveCfg.BaseBranch != "" {
		baseBranch = groveCfg.BaseBranch
	}

	// Check if branch already has a worktree
	hasWT, _ := gitops.WorktreeHasBranch(sourcePath, branch)
	if hasWT {
		return nil, fmt.Errorf("branch %s already has a worktree in %s", branch, repoName)
	}

	// Create branch if needed
	if !gitops.BranchExists(sourcePath, branch) {
		startPoint := "origin/" + baseBranch
		if err := gitops.CreateBranch(sourcePath, branch, startPoint); err != nil {
			// Try without remote prefix
			if err2 := gitops.CreateBranch(sourcePath, branch, baseBranch); err2 != nil {
				return nil, fmt.Errorf("creating branch: %w", err)
			}
		}
	}

	// Add worktree
	if err := gitops.WorktreeAdd(sourcePath, wtPath, branch); err != nil {
		return nil, fmt.Errorf("adding worktree: %w", err)
	}

	return &models.RepoWorktree{
		RepoName:     repoName,
		SourceRepo:   sourcePath,
		WorktreePath: wtPath,
		Branch:       branch,
	}, nil
}

func rollback(repos []models.RepoWorktree) {
	for _, r := range repos {
		gitops.WorktreeRemove(r.SourceRepo, r.WorktreePath)
	}
}

func runSetupHooks(ws models.Workspace) {
	var wg sync.WaitGroup
	for _, r := range ws.Repos {
		groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if groveCfg == nil || len(groveCfg.Setup) == 0 {
			continue
		}
		wg.Add(1)
		go func(repo models.RepoWorktree, cmds []string) {
			defer wg.Done()
			for _, cmdStr := range cmds {
				cmd := exec.Command("sh", "-c", cmdStr)
				cmd.Dir = repo.WorktreePath
				cmd.Stdout = os.Stderr
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					console.Warningf("setup hook failed in %s: %s", repo.RepoName, err)
				}
			}
		}(r, []string(groveCfg.Setup))
	}
	wg.Wait()
}

func writeMCPConfig(ws models.Workspace) {
	mcpCfg := models.MCPConfig{
		MCPServers: map[string]models.MCPServer{
			"grove": {
				Command: "gw",
				Args:    []string{"mcp-serve", "--workspace", ws.Name},
			},
		},
	}

	data, err := json.MarshalIndent(mcpCfg, "", "  ")
	if err != nil {
		return
	}

	// Write to workspace root
	os.WriteFile(filepath.Join(ws.Path, ".mcp.json"), data, 0o644)

	// Write to each worktree
	for _, r := range ws.Repos {
		os.WriteFile(filepath.Join(r.WorktreePath, ".mcp.json"), data, 0o644)
	}
}

// Delete removes a workspace and its worktrees.
func Delete(name string) error {
	ws, err := state.GetWorkspace(name)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", name)
	}

	// Run teardown hooks and remove worktrees
	for _, r := range ws.Repos {
		// Teardown hook
		groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if groveCfg != nil && groveCfg.Teardown != "" {
			cmd := exec.Command("sh", "-c", groveCfg.Teardown)
			cmd.Dir = r.WorktreePath
			cmd.Run()
		}

		// Remove worktree
		if err := gitops.WorktreeRemove(r.SourceRepo, r.WorktreePath); err != nil {
			// Fallback: remove directory
			os.RemoveAll(r.WorktreePath)
		}

		// Try to delete the branch (safe delete, skip if unmerged)
		gitops.DeleteBranch(r.SourceRepo, r.Branch, false)
	}

	// Remove workspace directory
	os.RemoveAll(ws.Path)

	// Record stats before removing state
	stats.RecordDeleted(*ws)

	// Remove from state
	if err := state.RemoveWorkspace(name); err != nil {
		return err
	}

	console.Successf("Workspace %s deleted", name)
	return nil
}

// Rename renames a workspace.
func Rename(oldName, newName string) error {
	ws, err := state.GetWorkspace(oldName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", oldName)
	}

	// Check new name doesn't exist
	existing, err := state.GetWorkspace(newName)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("Workspace %s already exists", newName)
	}

	oldPath := ws.Path
	newPath := filepath.Join(filepath.Dir(oldPath), newName)

	// Rename directory
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("renaming directory: %w", err)
	}

	// Update state
	if err := state.RenameWorkspace(oldName, newName, newPath); err != nil {
		// Rollback directory rename
		os.Rename(newPath, oldPath)
		return err
	}

	// Repair worktrees
	updatedWS, _ := state.GetWorkspace(newName)
	if updatedWS != nil {
		for _, r := range updatedWS.Repos {
			gitops.WorktreeRepair(r.SourceRepo, r.WorktreePath)
		}
	}

	console.Successf("Workspace %s renamed to %s", oldName, newName)
	return nil
}

// AddRepos adds repos to an existing workspace.
func AddRepos(wsName string, repoNames []string, repoMap map[string]string) error {
	ws, err := state.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", wsName)
	}

	// Filter out already-present repos
	existing := make(map[string]bool)
	for _, r := range ws.Repos {
		existing[r.RepoName] = true
	}

	var toAdd []string
	for _, name := range repoNames {
		if !existing[name] {
			toAdd = append(toAdd, name)
		}
	}

	if len(toAdd) == 0 {
		console.Info("All repos already in workspace")
		return nil
	}

	// Provision new worktrees
	for _, repoName := range toAdd {
		sourcePath, ok := repoMap[repoName]
		if !ok {
			return fmt.Errorf("repo %s not found", repoName)
		}

		rw, err := provisionWorktree(sourcePath, repoName, ws.Path, ws.Branch)
		if err != nil {
			return fmt.Errorf("adding %s: %w", repoName, err)
		}

		ws.Repos = append(ws.Repos, *rw)

		// Write .mcp.json to new worktree
		mcpCfg := models.MCPConfig{
			MCPServers: map[string]models.MCPServer{
				"grove": {
					Command: "gw",
					Args:    []string{"mcp-serve", "--workspace", ws.Name},
				},
			},
		}
		data, _ := json.MarshalIndent(mcpCfg, "", "  ")
		os.WriteFile(filepath.Join(rw.WorktreePath, ".mcp.json"), data, 0o644)
	}

	// Run setup hooks on new repos
	newWS := models.Workspace{Repos: ws.Repos[len(ws.Repos)-len(toAdd):]}
	runSetupHooks(newWS)

	// Update state
	if err := state.UpdateWorkspace(*ws); err != nil {
		return err
	}

	console.Successf("Added %d repo(s) to %s", len(toAdd), wsName)
	return nil
}

// RemoveRepos removes repos from a workspace.
func RemoveRepos(wsName string, repoNames []string) error {
	ws, err := state.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", wsName)
	}

	for _, repoName := range repoNames {
		r := ws.FindRepo(repoName)
		if r == nil {
			continue
		}

		// Teardown hook
		groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if groveCfg != nil && groveCfg.Teardown != "" {
			cmd := exec.Command("sh", "-c", groveCfg.Teardown)
			cmd.Dir = r.WorktreePath
			cmd.Run()
		}

		// Remove worktree
		if err := gitops.WorktreeRemove(r.SourceRepo, r.WorktreePath); err != nil {
			os.RemoveAll(r.WorktreePath)
		}

		// Try branch cleanup
		gitops.DeleteBranch(r.SourceRepo, r.Branch, false)

		ws.RemoveRepo(repoName)
	}

	if err := state.UpdateWorkspace(*ws); err != nil {
		return err
	}

	console.Successf("Removed %d repo(s) from %s", len(repoNames), wsName)
	return nil
}

// Sync rebases workspace repos onto their base branches.
func Sync(wsName string) error {
	ws, err := state.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", wsName)
	}

	for _, r := range ws.Repos {
		// pre_sync hook
		groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if groveCfg != nil && groveCfg.PreSync != "" {
			cmd := exec.Command("sh", "-c", groveCfg.PreSync)
			cmd.Dir = r.WorktreePath
			cmd.Run()
		}

		// Fetch (best-effort — refs may already be current)
		if err := gitops.Fetch(r.WorktreePath); err != nil {
			// Don't abort — local refs may already be updated
			console.Warningf("%s: fetch failed (continuing): %s", r.RepoName, err)
		}

		// Check if dirty
		status, err := gitops.RepoStatus(r.WorktreePath)
		if err != nil {
			console.Warningf("%s: status check failed: %s", r.RepoName, err)
			continue
		}
		if status != "" {
			console.Warningf("%s: skipping (dirty working tree)", r.RepoName)
			continue
		}

		// Determine base
		baseBranch := gitops.DefaultBranch(r.SourceRepo)
		if groveCfg != nil && groveCfg.BaseBranch != "" {
			baseBranch = groveCfg.BaseBranch
		}

		upstream := "origin/" + baseBranch

		// Check ahead/behind
		_, behind, err := gitops.CommitsAheadBehind(r.WorktreePath, upstream)
		if err != nil {
			console.Warningf("%s: cannot determine ahead/behind: %s", r.RepoName, err)
			continue
		}

		if behind == 0 {
			console.Infof("%s: ✓ up to date", r.RepoName)
			continue
		}

		// Rebase
		if err := gitops.RebaseOnto(r.WorktreePath, upstream); err != nil {
			console.Errorf("%s: rebase failed: %s", r.RepoName, err)
			gitops.RebaseAbort(r.WorktreePath)
			continue
		}

		console.Successf("%s: rebased (%d commits)", r.RepoName, behind)

		// post_sync hook
		if groveCfg != nil && groveCfg.PostSync != "" {
			cmd := exec.Command("sh", "-c", groveCfg.PostSync)
			cmd.Dir = r.WorktreePath
			cmd.Run()
		}
	}

	return nil
}

// Status displays git status for a workspace.
func Status(wsName string, jsonOutput bool) error {
	ws, err := state.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", wsName)
	}

	if jsonOutput {
		type repoStatus struct {
			Repo   string `json:"repo"`
			Branch string `json:"branch"`
			Status string `json:"status"`
			Ahead  string `json:"ahead"`
			Behind string `json:"behind"`
		}
		type wsStatus struct {
			Workspace string       `json:"workspace"`
			Path      string       `json:"path"`
			Repos     []repoStatus `json:"repos"`
		}

		result := wsStatus{
			Workspace: ws.Name,
			Path:      ws.Path,
		}

		for _, r := range ws.Repos {
			rs := repoStatus{
				Repo:   r.RepoName,
				Branch: r.Branch,
			}
			status, err := gitops.RepoStatus(r.WorktreePath)
			if err != nil {
				rs.Status = "error: " + err.Error()
			} else if status == "" {
				rs.Status = "clean"
			} else {
				rs.Status = status
			}

			baseBranch := gitops.DefaultBranch(r.SourceRepo)
			groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
			if groveCfg != nil && groveCfg.BaseBranch != "" {
				baseBranch = groveCfg.BaseBranch
			}
			ahead, behind, err := gitops.CommitsAheadBehind(r.WorktreePath, "origin/"+baseBranch)
			if err == nil {
				rs.Ahead = fmt.Sprintf("%d", ahead)
				rs.Behind = fmt.Sprintf("%d", behind)
			}

			result.Repos = append(result.Repos, rs)
		}

		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Table output
	fmt.Fprintf(os.Stderr, "Workspace: %s  (%s)\n\n", ws.Name, ws.Path)
	fmt.Fprintf(os.Stderr, "%-20s %-25s %-8s %s\n", "Repo", "Branch", "↑↓", "Status")
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("─", 80))

	for _, r := range ws.Repos {
		status, err := gitops.RepoStatus(r.WorktreePath)
		statusStr := "clean"
		if err != nil {
			statusStr = "error: " + err.Error()
		} else if status != "" {
			lines := strings.Count(status, "\n") + 1
			statusStr = fmt.Sprintf("%d changed", lines)
		}

		baseBranch := gitops.DefaultBranch(r.SourceRepo)
		groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if groveCfg != nil && groveCfg.BaseBranch != "" {
			baseBranch = groveCfg.BaseBranch
		}

		upDown := "-"
		ahead, behind, err := gitops.CommitsAheadBehind(r.WorktreePath, "origin/"+baseBranch)
		if err == nil {
			upDown = fmt.Sprintf("%d↑ %d↓", ahead, behind)
		}

		fmt.Fprintf(os.Stderr, "%-20s %-25s %-8s %s\n", r.RepoName, r.Branch, upDown, statusStr)
	}

	return nil
}

// Doctor checks workspace health and returns issues.
func Doctor(fix bool) ([]models.DoctorIssue, int, error) {
	workspaces, err := state.Load()
	if err != nil {
		return nil, 0, err
	}

	var issues []models.DoctorIssue
	fixed := 0

	// Collect workspaces to remove (iterate safely)
	var wsToRemove []string

	for _, ws := range workspaces {
		// Check workspace directory
		if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
			issues = append(issues, models.DoctorIssue{
				Workspace:       ws.Name,
				Repo:            nil,
				Issue:           "workspace directory missing",
				SuggestedAction: "remove stale state entry",
			})
			if fix {
				wsToRemove = append(wsToRemove, ws.Name)
				fixed++
			}
			continue
		}

		// Check each repo
		var reposToRemove []string
		for _, r := range ws.Repos {
			repoName := r.RepoName

			// Check source repo
			if _, err := os.Stat(r.SourceRepo); os.IsNotExist(err) {
				issues = append(issues, models.DoctorIssue{
					Workspace:       ws.Name,
					Repo:            &repoName,
					Issue:           "source repo missing",
					SuggestedAction: "remove stale repo entry",
				})
				if fix {
					reposToRemove = append(reposToRemove, repoName)
					fixed++
				}
				continue
			}

			// Check worktree directory
			if _, err := os.Stat(r.WorktreePath); os.IsNotExist(err) {
				issues = append(issues, models.DoctorIssue{
					Workspace:       ws.Name,
					Repo:            &repoName,
					Issue:           "worktree directory missing",
					SuggestedAction: "remove stale repo entry",
				})
				if fix {
					reposToRemove = append(reposToRemove, repoName)
					fixed++
				}
				continue
			}
		}

		// Apply repo removals
		if fix && len(reposToRemove) > 0 {
			currentWS, _ := state.GetWorkspace(ws.Name)
			if currentWS != nil {
				for _, name := range reposToRemove {
					currentWS.RemoveRepo(name)
				}
				state.UpdateWorkspace(*currentWS)
			}
		}
	}

	// Apply workspace removals
	if fix {
		for _, name := range wsToRemove {
			state.RemoveWorkspace(name)
		}
	}

	return issues, fixed, nil
}
