package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/claude"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
)

// Create creates a new workspace with worktrees for the given repos.
// repoMap is name→source_path.
func (s *Service) Create(name, branch string, repoNames []string, repoMap map[string]string, cfg *models.Config) error {
	// Check duplicate
	existing, err := s.State.GetWorkspace(name)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("Workspace %s already exists", name)
	}

	logging.Info("creating workspace %q (branch=%s, repos=%v)", name, branch, repoNames)

	wsPath := filepath.Join(cfg.WorkspaceDir, name)
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		return fmt.Errorf("creating workspace dir: %w", err)
	}

	ws := models.NewWorkspace(name, wsPath, branch)

	// Validate all repo names first
	sourcePaths := make([]string, len(repoNames))
	for i, repoName := range repoNames {
		sourcePath, ok := repoMap[repoName]
		if !ok {
			os.RemoveAll(wsPath)
			return fmt.Errorf("repo %s not found", repoName)
		}
		sourcePaths[i] = sourcePath
	}

	// Phase 1: parallel fetch (the slow network part)
	console.Infof("fetching %d repos...", len(repoNames))
	var fetchWg sync.WaitGroup
	for i, repoName := range repoNames {
		fetchWg.Add(1)
		go func(source, name string) {
			defer fetchWg.Done()
			if err := gitops.Fetch(source); err != nil {
				console.Warningf("  %s: fetch failed (continuing)", name)
			}
		}(sourcePaths[i], repoName)
	}
	fetchWg.Wait()

	// Phase 2: sequential worktree creation (for rollback safety)
	var created []models.RepoWorktree
	for i, repoName := range repoNames {
		console.Infof("[%d/%d] %s", i+1, len(repoNames), repoName)
		rw, err := provisionWorktreeNoFetch(sourcePaths[i], repoName, wsPath, branch)
		if err != nil {
			logging.Error("workspace creation failed for %q — rolled back", name)
			rollback(created)
			os.RemoveAll(wsPath)
			return fmt.Errorf("provisioning %s: %w", repoName, err)
		}
		created = append(created, *rw)
	}

	ws.Repos = created

	// Run setup hooks (parallel)
	if hasSetupHooks(ws) {
		console.Infof("running setup hooks...")
	}
	s.runSetupHooks(ws)

	// Save state
	if err := s.State.AddWorkspace(ws); err != nil {
		rollback(created)
		os.RemoveAll(wsPath)
		return err
	}

	// Record stats
	s.Stats.RecordCreated(ws)

	// Rehydrate Claude memory
	if cfg.ClaudeMemorySync {
		for _, r := range ws.Repos {
			if n := claude.RehydrateMemory(s.ClaudeDir, r.SourceRepo, r.WorktreePath); n > 0 {
				logging.Info("rehydrated %d Claude memory file(s) for %s", n, r.RepoName)
			}
		}
	}

	// Write .mcp.json
	writeMCPConfig(ws)

	logging.Info("workspace %q created at %s", name, wsPath)
	console.Successf("Workspace %s created at %s", name, wsPath)

	// Write GROVE_CD_FILE if set
	if cdFile := os.Getenv("GROVE_CD_FILE"); cdFile != "" {
		os.WriteFile(cdFile, []byte(wsPath), 0o644)
	}

	return nil
}

func provisionWorktree(sourcePath, repoName, wsPath, branch string) (*models.RepoWorktree, error) {
	_ = gitops.Fetch(sourcePath)
	return provisionWorktreeNoFetch(sourcePath, repoName, wsPath, branch)
}

func provisionWorktreeNoFetch(sourcePath, repoName, wsPath, branch string) (*models.RepoWorktree, error) {
	wtPath := filepath.Join(wsPath, repoName)

	// Check if branch already has a worktree
	hasWT, _ := gitops.WorktreeHasBranch(sourcePath, branch)
	if hasWT {
		return nil, fmt.Errorf("branch %s already has a worktree in %s", branch, repoName)
	}

	// Create branch if needed
	if !gitops.BranchExists(sourcePath, branch) {
		base, err := gitops.ResolveBaseBranch(sourcePath)
		if err != nil {
			base = "HEAD"
		}
		logging.Info("creating branch %q in %s from %s", branch, repoName, base)
		if err := gitops.CreateBranch(sourcePath, branch, base); err != nil {
			plainBase := strings.TrimPrefix(base, "origin/")
			if err2 := gitops.CreateBranch(sourcePath, branch, plainBase); err2 != nil {
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

func hasSetupHooks(ws models.Workspace) bool {
	for _, r := range ws.Repos {
		groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if groveCfg != nil && len(groveCfg.Setup) > 0 {
			return true
		}
	}
	return false
}

func (s *Service) runSetupHooks(ws models.Workspace) {
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
				if err := s.RunCmd(repo.WorktreePath, cmdStr); err != nil {
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
	var existing map[string]any

	data, err := os.ReadFile(path)
	if err == nil {
		json.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = make(map[string]any)
	}

	servers, ok := existing["mcpServers"].(map[string]any)
	if !ok {
		servers = make(map[string]any)
	}
	servers["grove"] = mcpServerEntry(wsName)
	existing["mcpServers"] = servers

	out, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

func writeMCPConfig(ws models.Workspace) {
	mergeMCPConfig(filepath.Join(ws.Path, ".mcp.json"), ws.Name)
	for _, r := range ws.Repos {
		mergeMCPConfig(filepath.Join(r.WorktreePath, ".mcp.json"), ws.Name)
	}
}

// removeMCPConfig removes the grove entry from .mcp.json files.
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
		var existing map[string]any
		if err := json.Unmarshal(data, &existing); err != nil {
			continue
		}
		servers, ok := existing["mcpServers"].(map[string]any)
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
func (s *Service) Delete(name string) error {
	ws, err := s.State.GetWorkspace(name)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", name)
	}

	logging.Info("deleting workspace %q", name)
	removeMCPConfig(*ws)

	// Harvest Claude memory before destruction
	if s.ClaudeDir != "" {
		for _, r := range ws.Repos {
			if n := claude.HarvestMemory(s.ClaudeDir, r.WorktreePath, r.SourceRepo); n > 0 {
				logging.Info("harvested %d Claude memory file(s) for %s", n, r.RepoName)
			}
		}
	}

	// Parallel teardown+remove for all repos
	succeeded := make([]bool, len(ws.Repos))
	var wg sync.WaitGroup
	for i, r := range ws.Repos {
		wg.Add(1)
		go func(idx int, repo models.RepoWorktree) {
			defer wg.Done()
			groveCfg, _ := gitops.ReadGroveConfig(repo.SourceRepo)
			if groveCfg != nil && groveCfg.Teardown != "" {
				s.RunCmdSilent(repo.WorktreePath, groveCfg.Teardown)
			}

			if err := gitops.WorktreeRemove(repo.SourceRepo, repo.WorktreePath); err != nil {
				if err := os.RemoveAll(repo.WorktreePath); err != nil {
					logging.Warn("failed to remove worktree for %s: %s", repo.RepoName, err)
					console.Warningf("%s: failed to remove worktree: %s", repo.RepoName, err)
					return
				}
			}

			if err := gitops.DeleteBranch(repo.SourceRepo, repo.Branch, false); err != nil {
				logging.Warn("branch %q has unmerged commits in %s — kept", repo.Branch, repo.RepoName)
				console.Warningf("%s: branch %s has unmerged commits, kept", repo.RepoName, repo.Branch)
			} else {
				logging.Info("deleted branch %q in %s", repo.Branch, repo.RepoName)
			}

			succeeded[idx] = true
		}(i, r)
	}
	wg.Wait()

	failCount := 0
	for _, ok := range succeeded {
		if !ok {
			failCount++
		}
	}
	if failCount > 0 {
		logging.Warn("workspace %q: %d worktree(s) failed to remove", name, failCount)
	}

	os.RemoveAll(ws.Path)

	s.Stats.RecordDeleted(*ws)

	_, dirErr := os.Stat(ws.Path)
	if failCount == 0 || os.IsNotExist(dirErr) {
		if err := s.State.RemoveWorkspace(name); err != nil {
			return err
		}
	}

	logging.Info("workspace %q deleted", name)
	console.Successf("Workspace %s deleted", name)
	return nil
}

// Rename renames a workspace using a state-first pattern with rollback.
func (s *Service) Rename(oldName, newName string) error {
	ws, err := s.State.GetWorkspace(oldName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", oldName)
	}

	existing, err := s.State.GetWorkspace(newName)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("Workspace %s already exists", newName)
	}

	oldPath := ws.Path
	newPath := filepath.Join(filepath.Dir(oldPath), newName)

	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("directory %s already exists", newPath)
	}

	origName := ws.Name
	origPath := ws.Path
	origWorktreePaths := make([]string, len(ws.Repos))
	for i := range ws.Repos {
		origWorktreePaths[i] = ws.Repos[i].WorktreePath
	}

	ws.Name = newName
	ws.Path = newPath
	for i := range ws.Repos {
		ws.Repos[i].WorktreePath = strings.Replace(ws.Repos[i].WorktreePath, oldPath, newPath, 1)
	}

	if err := s.State.UpdateWorkspaceByName(*ws, oldName); err != nil {
		return err
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		ws.Name = origName
		ws.Path = origPath
		for i := range ws.Repos {
			ws.Repos[i].WorktreePath = origWorktreePaths[i]
		}
		s.State.UpdateWorkspaceByName(*ws, newName)
		return fmt.Errorf("renaming directory: %w", err)
	}

	for _, r := range ws.Repos {
		gitops.WorktreeRepair(r.SourceRepo, r.WorktreePath)
	}

	if s.ClaudeDir != "" {
		for i := range ws.Repos {
			claude.MigrateMemoryDir(s.ClaudeDir, origWorktreePaths[i], ws.Repos[i].WorktreePath)
		}
	}

	logging.Info("workspace %q renamed to %q", oldName, newName)
	console.Successf("Workspace %s renamed to %s", oldName, newName)
	return nil
}

// AddRepos adds repos to an existing workspace.
func (s *Service) AddRepos(wsName string, repoNames []string, repoMap map[string]string) error {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", wsName)
	}

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
		mergeMCPConfig(filepath.Join(rw.WorktreePath, ".mcp.json"), ws.Name)
	}

	newWS := models.Workspace{Repos: ws.Repos[beforeLen:]}
	s.runSetupHooks(newWS)

	if err := s.State.UpdateWorkspace(*ws); err != nil {
		return err
	}

	logging.Info("added %d repo(s) to workspace %q", len(toAdd), wsName)
	console.Successf("Added %d repo(s) to %s", len(toAdd), wsName)
	return nil
}

// RemoveRepos removes repos from a workspace.
func (s *Service) RemoveRepos(wsName string, repoNames []string) error {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", wsName)
	}

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

	succeeded := make([]bool, len(items))
	var wg sync.WaitGroup
	for i, item := range items {
		wg.Add(1)
		go func(idx int, r models.RepoWorktree) {
			defer wg.Done()
			groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
			if groveCfg != nil && groveCfg.Teardown != "" {
				s.RunCmdSilent(r.WorktreePath, groveCfg.Teardown)
			}

			if err := gitops.WorktreeRemove(r.SourceRepo, r.WorktreePath); err != nil {
				os.RemoveAll(r.WorktreePath)
			}

			gitops.DeleteBranch(r.SourceRepo, r.Branch, false)
			succeeded[idx] = true
		}(i, *item.repo)
	}
	wg.Wait()

	for i, item := range items {
		if succeeded[i] {
			ws.RemoveRepo(item.name)
		}
	}

	if err := s.State.UpdateWorkspace(*ws); err != nil {
		return err
	}

	logging.Info("removed %d repo(s) from workspace %q", len(repoNames), wsName)
	console.Successf("Removed %d repo(s) from %s", len(repoNames), wsName)
	return nil
}

// syncOneRepo syncs a single repo.
func (s *Service) syncOneRepo(r models.RepoWorktree) {
	if err := gitops.Fetch(r.SourceRepo); err != nil {
		console.Warningf("%s: fetch failed (continuing): %s", r.RepoName, err)
	}

	groveCfg, _ := gitops.ReadGroveConfig(r.SourceRepo)

	status, err := gitops.RepoStatus(r.WorktreePath)
	if err != nil {
		console.Warningf("%s: status check failed: %s", r.RepoName, err)
		return
	}
	if status != "" {
		console.Warningf("%s: skipping (dirty working tree)", r.RepoName)
		return
	}

	upstream, err := gitops.ResolveBaseBranch(r.SourceRepo)
	if err != nil {
		console.Warningf("%s: could not determine base branch: %s", r.RepoName, err)
		return
	}

	_, behind, err := gitops.CommitsAheadBehind(r.WorktreePath, upstream)
	if err != nil {
		console.Warningf("%s: cannot determine ahead/behind: %s", r.RepoName, err)
		return
	}

	if behind == 0 {
		console.Infof("%s: ✓ up to date", r.RepoName)
		return
	}

	if groveCfg != nil && groveCfg.PreSync != "" {
		s.RunCmdSilent(r.WorktreePath, groveCfg.PreSync)
	}

	if err := gitops.RebaseOnto(r.WorktreePath, upstream); err != nil {
		console.Errorf("%s: rebase failed: %s", r.RepoName, err)
		gitops.RebaseAbort(r.WorktreePath)
		return
	}

	console.Successf("%s: rebased (%d commits)", r.RepoName, behind)

	if groveCfg != nil && groveCfg.PostSync != "" {
		s.RunCmdSilent(r.WorktreePath, groveCfg.PostSync)
	}
}

// Sync rebases workspace repos onto their base branches.
func (s *Service) Sync(wsName string) error {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", wsName)
	}

	logging.Info("syncing workspace %q", wsName)

	var wg sync.WaitGroup
	for _, r := range ws.Repos {
		wg.Add(1)
		go func(repo models.RepoWorktree) {
			defer wg.Done()
			s.syncOneRepo(repo)
		}(r)
	}
	wg.Wait()

	return nil
}

type repoStatusResult struct {
	Repo   string         `json:"repo"`
	Branch string         `json:"branch"`
	Status string         `json:"status"`
	Ahead  string         `json:"ahead"`
	Behind string         `json:"behind"`
	PR     *gitops.PRInfo `json:"pr,omitempty"`
}

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
func (s *Service) Status(wsName string, opts StatusOptions) error {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return err
	}
	if ws == nil {
		return fmt.Errorf("Workspace %s not found", wsName)
	}

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

	fmt.Fprintf(os.Stdout, "Workspace: %s  (%s)\n\n", ws.Name, ws.Path)

	var headers []string
	if opts.PR {
		headers = []string{"Repo", "Branch", "↑↓", "PR", "Status"}
	} else {
		headers = []string{"Repo", "Branch", "↑↓", "Status"}
	}
	table := console.NewTable(os.Stdout, headers)

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
			table.AddRow([]string{rs.Repo, rs.Branch, upDown, prStr, statusStr})
		} else {
			table.AddRow([]string{rs.Repo, rs.Branch, upDown, statusStr})
		}
	}
	table.Render()

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
func (s *Service) AllWorkspacesSummary() ([]WorkspaceSummary, error) {
	workspaces, err := s.State.Load()
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
func (s *Service) Doctor(fix bool) ([]models.DoctorIssue, int, error) {
	workspaces, err := s.State.Load()
	if err != nil {
		return nil, 0, err
	}

	var issues []models.DoctorIssue
	fixed := 0

	if s.ClaudeDir != "" && s.WorkspaceDir != "" {
		orphaned := claude.FindOrphanedMemoryDirs(s.ClaudeDir, s.WorkspaceDir)
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

	var wsToRemove []string

	for _, ws := range workspaces {
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

		var reposToRemove []string
		for _, r := range ws.Repos {
			repoName := r.RepoName

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

		if fix && len(reposToRemove) > 0 {
			currentWS, _ := s.State.GetWorkspace(ws.Name)
			if currentWS != nil {
				for _, name := range reposToRemove {
					currentWS.RemoveRepo(name)
				}
				s.State.UpdateWorkspace(*currentWS)
			}
		}
	}

	if fix {
		for _, name := range wsToRemove {
			s.State.RemoveWorkspace(name)
		}
	}

	return issues, fixed, nil
}
