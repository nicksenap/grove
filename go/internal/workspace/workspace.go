package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/claude"
	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/stats"
)

// ClaudeDir is the path to ~/.claude. Override in tests.
var ClaudeDir string

func init() {
	home, _ := os.UserHomeDir()
	ClaudeDir = filepath.Join(home, ".claude")
}

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

	// Rehydrate Claude memory
	if cfg.ClaudeMemorySync {
		for _, r := range ws.Repos {
			claude.RehydrateMemory(ClaudeDir, r.SourceRepo, r.WorktreePath)
		}
	}

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

	// Fetch (best-effort)
	_ = gitops.Fetch(sourcePath)

	// Check if branch already has a worktree
	hasWT, _ := gitops.WorktreeHasBranch(sourcePath, branch)
	if hasWT {
		return nil, fmt.Errorf("branch %s already has a worktree in %s", branch, repoName)
	}

	// Create branch if needed
	if !gitops.BranchExists(sourcePath, branch) {
		base, err := gitops.ResolveBaseBranch(sourcePath)
		if err != nil {
			// No remote — branch from current HEAD
			base = "HEAD"
		}
		if err := gitops.CreateBranch(sourcePath, branch, base); err != nil {
			// Try without "origin/" prefix
			plainBase := strings.TrimPrefix(base, "origin/")
			if err2 := gitops.CreateBranch(sourcePath, branch, plainBase); err2 != nil {
				// Last resort: branch from HEAD
				if err3 := gitops.CreateBranch(sourcePath, branch, "HEAD"); err3 != nil {
					return nil, fmt.Errorf("creating branch: %w", err)
				}
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
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					console.Warningf("setup hook failed in %s: %s", repo.RepoName, err)
				}
			}
		}(r, []string(groveCfg.Setup))
	}
	wg.Wait()
}

// mcpServerEntry returns the grove MCP server config.
func mcpServerEntry(wsName string) models.MCPServer {
	return models.MCPServer{
		Command: "gw",
		Args:    []string{"mcp-serve", "--workspace", wsName},
	}
}

// mergeMCPConfig reads existing .mcp.json, adds/updates the grove entry, writes back.
func mergeMCPConfig(path string, wsName string) {
	var existing map[string]interface{}

	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = make(map[string]interface{})
	}

	servers, ok := existing["mcpServers"].(map[string]interface{})
	if !ok {
		servers = make(map[string]interface{})
	}
	servers["grove"] = mcpServerEntry(wsName)
	existing["mcpServers"] = servers

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return
	}
	// Atomic write
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

func writeMCPConfig(ws models.Workspace) {
	// Write to workspace root
	mergeMCPConfig(filepath.Join(ws.Path, ".mcp.json"), ws.Name)

	// Write to each worktree
	for _, r := range ws.Repos {
		mergeMCPConfig(filepath.Join(r.WorktreePath, ".mcp.json"), ws.Name)
	}
}

// removeMCPConfig removes the grove entry from .mcp.json files.
// Deletes the file if no other servers remain.
func removeMCPConfig(ws models.Workspace) {
	paths := []string{filepath.Join(ws.Path, ".mcp.json")}
	for _, r := range ws.Repos {
		paths = append(paths, filepath.Join(r.WorktreePath, ".mcp.json"))
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var existing map[string]interface{}
		if err := json.Unmarshal(data, &existing); err != nil {
			continue
		}

		servers, ok := existing["mcpServers"].(map[string]interface{})
		if !ok {
			continue
		}

		delete(servers, "grove")

		if len(servers) == 0 {
			os.Remove(path)
		} else {
			existing["mcpServers"] = servers
			out, _ := json.MarshalIndent(existing, "", "  ")
			os.WriteFile(path, out, 0o644)
		}
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

	// Remove MCP config
	removeMCPConfig(*ws)

	// Harvest Claude memory before destruction
	cfg, _ := config.Load()
	if cfg != nil && cfg.ClaudeMemorySync {
		for _, r := range ws.Repos {
			claude.HarvestMemory(ClaudeDir, r.WorktreePath, r.SourceRepo)
		}
	}

	// Parallel teardown+remove for all repos
	succeeded := make([]bool, len(ws.Repos))
	var wg sync.WaitGroup
	for i, r := range ws.Repos {
		wg.Add(1)
		go func(idx int, repo models.RepoWorktree) {
			defer wg.Done()
			// Teardown hook
			groveCfg, _ := gitops.ReadGroveConfig(repo.SourceRepo)
			if groveCfg != nil && groveCfg.Teardown != "" {
				cmd := exec.Command("sh", "-c", groveCfg.Teardown)
				cmd.Dir = repo.WorktreePath
				cmd.Run()
			}

			// Remove worktree
			if err := gitops.WorktreeRemove(repo.SourceRepo, repo.WorktreePath); err != nil {
				// Force cleanup: try removing directory directly
				if err := os.RemoveAll(repo.WorktreePath); err != nil {
					console.Warningf("%s: failed to remove worktree: %s", repo.RepoName, err)
					return // leave succeeded[idx] = false
				}
			}

			// Try to delete the branch (safe delete, warn on unmerged)
			if err := gitops.DeleteBranch(repo.SourceRepo, repo.Branch, false); err != nil {
				console.Warningf("%s: branch %s has unmerged commits, kept", repo.RepoName, repo.Branch)
			}

			succeeded[idx] = true
		}(i, r)
	}
	wg.Wait()

	// Count failures
	failCount := 0
	for _, ok := range succeeded {
		if !ok {
			failCount++
		}
	}

	// Remove workspace directory
	os.RemoveAll(ws.Path)

	// Record stats before removing state
	stats.RecordDeleted(*ws)

	// Only remove from state if no failures or dir is gone
	_, dirErr := os.Stat(ws.Path)
	if failCount == 0 || os.IsNotExist(dirErr) {
		if err := state.RemoveWorkspace(name); err != nil {
			return err
		}
	}

	console.Successf("Workspace %s deleted", name)
	return nil
}

// Rename renames a workspace using a state-first pattern with rollback.
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

	// Check target dir doesn't exist
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("directory %s already exists", newPath)
	}

	// Save original values for rollback
	origName := ws.Name
	origPath := ws.Path
	origWorktreePaths := make([]string, len(ws.Repos))
	for i := range ws.Repos {
		origWorktreePaths[i] = ws.Repos[i].WorktreePath
	}

	// Mutate in memory
	ws.Name = newName
	ws.Path = newPath
	for i := range ws.Repos {
		ws.Repos[i].WorktreePath = strings.Replace(ws.Repos[i].WorktreePath, oldPath, newPath, 1)
	}

	// State-first: update state atomically using match_name
	if err := state.UpdateWorkspaceByName(*ws, oldName); err != nil {
		return err
	}

	// Now rename filesystem
	if err := os.Rename(oldPath, newPath); err != nil {
		// Rollback state
		ws.Name = origName
		ws.Path = origPath
		for i := range ws.Repos {
			ws.Repos[i].WorktreePath = origWorktreePaths[i]
		}
		state.UpdateWorkspaceByName(*ws, newName)
		return fmt.Errorf("renaming directory: %w", err)
	}

	// Repair worktrees (best-effort)
	for _, r := range ws.Repos {
		gitops.WorktreeRepair(r.SourceRepo, r.WorktreePath)
	}

	// Migrate Claude memory dirs
	cfg, _ := config.Load()
	if cfg != nil && cfg.ClaudeMemorySync {
		for i := range ws.Repos {
			claude.MigrateMemoryDir(ClaudeDir, origWorktreePaths[i], ws.Repos[i].WorktreePath)
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
	beforeLen := len(ws.Repos)
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
		mergeMCPConfig(filepath.Join(rw.WorktreePath, ".mcp.json"), ws.Name)
	}

	// Run setup hooks on new repos only
	newWS := models.Workspace{Repos: ws.Repos[beforeLen:]}
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

	// Collect repos to remove
	type removeItem struct {
		name string
		repo *models.RepoWorktree
	}
	var items []removeItem
	for _, repoName := range repoNames {
		r := ws.FindRepo(repoName)
		if r != nil {
			items = append(items, removeItem{name: repoName, repo: r})
		}
	}

	// Parallel teardown+remove
	succeeded := make([]bool, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(idx int, r models.RepoWorktree) {
			defer wg.Done()
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
			succeeded[idx] = true
		}(i, *item.repo)
	}
	wg.Wait()

	// Only remove successfully removed repos from state
	for i, item := range items {
		if succeeded[i] {
			ws.RemoveRepo(item.name)
		}
	}

	if err := state.UpdateWorkspace(*ws); err != nil {
		return err
	}

	console.Successf("Removed %d repo(s) from %s", len(repoNames), wsName)
	return nil
}

// syncOneRepo syncs a single repo. Returns a message for output ordering.
func syncOneRepo(r models.RepoWorktree) {
	// Fetch from source repo (worktrees share the git database)
	if err := gitops.Fetch(r.SourceRepo); err != nil {
		console.Warningf("%s: fetch failed (continuing): %s", r.RepoName, err)
	}

	groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)

	// Check if dirty
	status, err := gitops.RepoStatus(r.WorktreePath)
	if err != nil {
		console.Warningf("%s: status check failed: %s", r.RepoName, err)
		return
	}
	if status != "" {
		console.Warningf("%s: skipping (dirty working tree)", r.RepoName)
		return
	}

	// Determine base
	upstream, err := gitops.ResolveBaseBranch(r.SourceRepo)
	if err != nil {
		console.Warningf("%s: could not determine base branch: %s", r.RepoName, err)
		return
	}

	// Check ahead/behind
	_, behind, err := gitops.CommitsAheadBehind(r.WorktreePath, upstream)
	if err != nil {
		console.Warningf("%s: cannot determine ahead/behind: %s", r.RepoName, err)
		return
	}

	if behind == 0 {
		console.Infof("%s: ✓ up to date", r.RepoName)
		return
	}

	// pre_sync hook
	if groveCfg != nil && groveCfg.PreSync != "" {
		cmd := exec.Command("sh", "-c", groveCfg.PreSync)
		cmd.Dir = r.WorktreePath
		cmd.Run()
	}

	// Rebase
	if err := gitops.RebaseOnto(r.WorktreePath, upstream); err != nil {
		console.Errorf("%s: rebase failed: %s", r.RepoName, err)
		gitops.RebaseAbort(r.WorktreePath)
		return
	}

	console.Successf("%s: rebased (%d commits)", r.RepoName, behind)

	// post_sync hook
	if groveCfg != nil && groveCfg.PostSync != "" {
		cmd := exec.Command("sh", "-c", groveCfg.PostSync)
		cmd.Dir = r.WorktreePath
		cmd.Run()
	}
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

	// Parallel sync per repo
	var wg sync.WaitGroup
	for _, r := range ws.Repos {
		wg.Add(1)
		go func(repo models.RepoWorktree) {
			defer wg.Done()
			syncOneRepo(repo)
		}(r)
	}
	wg.Wait()

	return nil
}

type repoStatusResult struct {
	Repo   string          `json:"repo"`
	Branch string          `json:"branch"`
	Status string          `json:"status"`
	Ahead  string          `json:"ahead"`
	Behind string          `json:"behind"`
	PR     *gitops.PRInfo  `json:"pr,omitempty"`
}

// collectRepoStatus gathers status for a single repo.
func collectRepoStatus(r models.RepoWorktree) repoStatusResult {
	rs := repoStatusResult{
		Repo:   r.RepoName,
		Branch: r.Branch,
	}
	status, err := gitops.RepoStatus(r.WorktreePath)
	if err != nil {
		rs.Status = "error: " + err.Error()
		rs.Ahead = "-"
		rs.Behind = "-"
		return rs
	}
	if status == "" {
		rs.Status = "clean"
	} else {
		rs.Status = status
	}

	upstream, _ := gitops.ResolveBaseBranch(r.SourceRepo)
	if upstream == "" {
		upstream = "origin/main"
	}
	ahead, behind, err := gitops.CommitsAheadBehind(r.WorktreePath, upstream)
	if err == nil {
		rs.Ahead = fmt.Sprintf("%d", ahead)
		rs.Behind = fmt.Sprintf("%d", behind)
	} else {
		rs.Ahead = "-"
		rs.Behind = "-"
	}

	return rs
}

// formatPR returns a display string for PR status.
func formatPR(pr *gitops.PRInfo) string {
	if pr == nil {
		return "-"
	}
	switch pr.State {
	case "MERGED":
		return fmt.Sprintf("#%d merged", pr.Number)
	case "CLOSED":
		return fmt.Sprintf("#%d closed", pr.Number)
	case "OPEN":
		switch pr.ReviewDecision {
		case "APPROVED":
			return fmt.Sprintf("#%d ✓", pr.Number)
		case "CHANGES_REQUESTED":
			return fmt.Sprintf("#%d ✗", pr.Number)
		default:
			return fmt.Sprintf("#%d open", pr.Number)
		}
	default:
		return fmt.Sprintf("#%d %s", pr.Number, pr.State)
	}
}

// StatusOptions controls status output.
type StatusOptions struct {
	JSON    bool
	Verbose bool
	PR      bool
}

// Status displays git status for a workspace.
func Status(wsName string, opts StatusOptions) error {
	ws, err := state.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", wsName)
	}

	// Collect status in parallel, results in input order
	results := make([]repoStatusResult, len(ws.Repos))
	var wg sync.WaitGroup
	for i, r := range ws.Repos {
		wg.Add(1)
		go func(idx int, repo models.RepoWorktree) {
			defer wg.Done()
			results[idx] = collectRepoStatus(repo)
			if opts.PR {
				results[idx].PR = gitops.PRStatus(repo.WorktreePath)
			}
		}(i, r)
	}
	wg.Wait()

	if opts.JSON {
		type wsStatus struct {
			Workspace string             `json:"workspace"`
			Path      string             `json:"path"`
			Repos     []repoStatusResult `json:"repos"`
		}
		result := wsStatus{
			Workspace: ws.Name,
			Path:      ws.Path,
			Repos:     results,
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	// Table output (to stdout for piping)
	fmt.Fprintf(os.Stdout, "Workspace: %s  (%s)\n\n", ws.Name, ws.Path)
	if opts.PR {
		fmt.Fprintf(os.Stdout, "%-20s %-25s %-8s %-12s %s\n", "Repo", "Branch", "↑↓", "PR", "Status")
	} else {
		fmt.Fprintf(os.Stdout, "%-20s %-25s %-8s %s\n", "Repo", "Branch", "↑↓", "Status")
	}
	fmt.Fprintf(os.Stdout, "%s\n", strings.Repeat("─", 80))

	for _, rs := range results {
		statusStr := rs.Status
		if rs.Status != "clean" && rs.Status != "" && !strings.HasPrefix(rs.Status, "error:") {
			lines := strings.Count(rs.Status, "\n") + 1
			statusStr = fmt.Sprintf("%d changed", lines)
		}

		upDown := "-"
		if rs.Ahead != "-" && rs.Behind != "-" && rs.Ahead != "" && rs.Behind != "" {
			upDown = fmt.Sprintf("%s↑ %s↓", rs.Ahead, rs.Behind)
		}

		if opts.PR {
			prStr := "-"
			if rs.PR != nil {
				prStr = formatPR(rs.PR)
			}
			fmt.Fprintf(os.Stdout, "%-20s %-25s %-8s %-12s %s\n", rs.Repo, rs.Branch, upDown, prStr, statusStr)
		} else {
			fmt.Fprintf(os.Stdout, "%-20s %-25s %-8s %s\n", rs.Repo, rs.Branch, upDown, statusStr)
		}
	}

	// Verbose: print raw git status for dirty repos
	if opts.Verbose {
		for _, rs := range results {
			if rs.Status != "clean" && rs.Status != "" && !strings.HasPrefix(rs.Status, "error:") {
				fmt.Fprintf(os.Stderr, "\n%s:\n%s\n", rs.Repo, rs.Status)
			}
		}
	}

	return nil
}

// WorkspaceSummary holds summary info for list --status.
type WorkspaceSummary struct {
	Name   string `json:"name"`
	Branch string `json:"branch"`
	Repos  int    `json:"repos"`
	Status string `json:"status"`
	Path   string `json:"path"`
}

// AllWorkspacesSummary returns a status summary for all workspaces.
// Runs status checks in parallel across workspaces.
func AllWorkspacesSummary() ([]WorkspaceSummary, error) {
	workspaces, err := state.Load()
	if err != nil {
		return nil, err
	}

	if len(workspaces) == 0 {
		return []WorkspaceSummary{}, nil
	}

	results := make([]WorkspaceSummary, len(workspaces))
	var wg sync.WaitGroup
	for i, ws := range workspaces {
		wg.Add(1)
		go func(idx int, w models.Workspace) {
			defer wg.Done()
			summary := WorkspaceSummary{
				Name:   w.Name,
				Branch: w.Branch,
				Repos:  len(w.Repos),
				Path:   w.Path,
			}

			// Collect status per repo
			clean, dirty, errCount := 0, 0, 0
			for _, r := range w.Repos {
				status, err := gitops.RepoStatus(r.WorktreePath)
				if err != nil {
					errCount++
				} else if status == "" {
					clean++
				} else {
					dirty++
				}
			}

			parts := []string{}
			if clean > 0 {
				parts = append(parts, fmt.Sprintf("%d clean", clean))
			}
			if dirty > 0 {
				parts = append(parts, fmt.Sprintf("%d modified", dirty))
			}
			if errCount > 0 {
				parts = append(parts, fmt.Sprintf("%d error", errCount))
			}
			summary.Status = strings.Join(parts, ", ")
			if summary.Status == "" {
				summary.Status = "empty"
			}

			results[idx] = summary
		}(i, ws)
	}
	wg.Wait()

	return results, nil
}

// Doctor checks workspace health and returns issues.
func Doctor(fix bool) ([]models.DoctorIssue, int, error) {
	workspaces, err := state.Load()
	if err != nil {
		return nil, 0, err
	}

	var issues []models.DoctorIssue
	fixed := 0

	// Check for orphaned Claude memory directories
	cfg, _ := config.Load()
	if cfg != nil && cfg.ClaudeMemorySync {
		orphaned := claude.FindOrphanedMemoryDirs(ClaudeDir, cfg.WorkspaceDir)
		for _, dir := range orphaned {
			issues = append(issues, models.DoctorIssue{
				Workspace:       "",
				Repo:            nil,
				Issue:           fmt.Sprintf("orphaned Claude memory: %s", filepath.Base(dir)),
				SuggestedAction: "remove orphaned Claude memory directory",
			})
			if fix {
				os.RemoveAll(dir)
				fixed++
			}
		}
	}

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
