package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
)

// BranchMode determines how a worktree's branch is provisioned.
type BranchMode int

const (
	// BranchModeCreate creates a new branch from the resolved base branch.
	// This is the default and matches Grove's historical behavior.
	BranchModeCreate BranchMode = iota
	// BranchModeTrack checks out an existing remote branch (e.g. a pull-request
	// head) as a tracking branch instead of creating a new one from base.
	BranchModeTrack
)

// CreateOpts carries the inputs for creating a workspace. It groups the original
// positional Create parameters and adds optional branch-mode and provenance.
type CreateOpts struct {
	Branch  string
	Repos   []string
	RepoMap map[string]string // repo name → source path
	Cfg     *models.Config

	// BranchMode is the mode applied to the repos selected by TrackBranchRepo.
	// When TrackBranchRepo is empty, BranchMode applies to every repo; when set,
	// it applies only to that one repo and all others use BranchModeCreate.
	//
	// A PR URL identifies exactly one repo+branch, so a resolver names that repo
	// in TrackBranchRepo — sibling repos added to the same workspace then still
	// get fresh branches from base rather than coincidentally tracking a remote
	// branch of the same name. Track mode always falls back to create mode for
	// any repo where the remote branch does not exist, so a blanket
	// (empty-TrackBranchRepo) track is safe too.
	BranchMode      BranchMode
	TrackBranchRepo string

	// Source, when set, is persisted on the workspace as provenance (e.g. the
	// GitHub PR / Notion page / Slack thread it was seeded from). Opaque to core.
	Source *models.WorkspaceSource
}

// Create creates a new workspace with worktrees for the given repos.
// repoMap is name→source_path. It preserves the historical positional signature
// by delegating to CreateWithOpts.
func (s *Service) Create(name, branch string, repoNames []string, repoMap map[string]string, cfg *models.Config) error {
	return s.CreateWithOpts(name, CreateOpts{
		Branch:  branch,
		Repos:   repoNames,
		RepoMap: repoMap,
		Cfg:     cfg,
	})
}

// CreateWithOpts creates a new workspace from the given options. It is the full
// implementation behind Create, additionally supporting per-repo branch tracking
// (BranchMode/TrackBranchRepo) and a persisted Source link.
func (s *Service) CreateWithOpts(name string, opts CreateOpts) error {
	branch := opts.Branch
	repoNames := opts.Repos
	repoMap := opts.RepoMap
	cfg := opts.Cfg

	// Check duplicate
	existing, err := s.State.GetWorkspace(name)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrWorkspaceExists(name)
	}

	logging.Info("creating workspace %q (branch=%s, repos=%v)", name, branch, repoNames)

	wsPath := filepath.Join(cfg.WorkspaceDir, name)
	if err := os.MkdirAll(wsPath, 0o755); err != nil {
		return fmt.Errorf("creating workspace dir: %w", err)
	}

	ws := models.NewWorkspace(name, wsPath, branch)
	ws.Source = opts.Source

	// Validate all repo names first
	sourcePaths := make([]string, len(repoNames))
	for i, repoName := range repoNames {
		sourcePath, ok := repoMap[repoName]
		if !ok {
			os.RemoveAll(wsPath)
			return ErrRepoNotFound(repoName)
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
				console.Warningf("  %s: fetch failed, using local state", name)
			}
		}(sourcePaths[i], repoName)
	}
	fetchWg.Wait()

	// Phase 2: sequential worktree creation (for rollback safety)
	var created []models.RepoWorktree
	for i, repoName := range repoNames {
		console.Infof("[%d/%d] %s", i+1, len(repoNames), repoName)
		// Resolve the per-repo mode. With no TrackBranchRepo set, opts.BranchMode
		// applies to every repo; otherwise only the named repo uses it.
		mode := opts.BranchMode
		if opts.TrackBranchRepo != "" && repoName != opts.TrackBranchRepo {
			mode = BranchModeCreate
		}
		rw, err := provisionWorktreeNoFetch(sourcePaths[i], repoName, wsPath, branch, mode)
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
	return provisionWorktreeNoFetch(sourcePath, repoName, wsPath, branch, BranchModeCreate)
}

func provisionWorktreeNoFetch(sourcePath, repoName, wsPath, branch string, mode BranchMode) (*models.RepoWorktree, error) {
	wtPath := filepath.Join(wsPath, repoName)

	// Check if branch already has a worktree
	hasWT, _ := gitops.WorktreeHasBranch(sourcePath, branch)
	if hasWT {
		return nil, ErrWorktreeExists(branch, repoName)
	}

	// Track mode: check out an existing remote branch (e.g. a PR head) rather
	// than creating a new branch. Only meaningful when the branch does not
	// already exist locally; otherwise fall through to the normal add below.
	if mode == BranchModeTrack && !gitops.BranchExists(sourcePath, branch) {
		if gitops.RemoteBranchExists(sourcePath, branch) {
			logging.Info("tracking existing remote branch %q in %s", branch, repoName)
			if err := gitops.WorktreeAddTracking(sourcePath, wtPath, branch); err != nil {
				return nil, ErrGit(err, "adding tracking worktree for %s", repoName)
			}
			return &models.RepoWorktree{
				RepoName:     repoName,
				SourceRepo:   sourcePath,
				WorktreePath: wtPath,
				Branch:       branch,
			}, nil
		}
		// Remote branch missing (deleted, force-pushed, or a fork PR whose head
		// lives on another remote). Fall back to create-mode with a warning
		// rather than failing mid-creation.
		console.Warningf("%s: remote branch %s not found — creating a new branch from base instead", repoName, branch)
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
					return nil, ErrGit(err, "creating branch %s in %s", branch, repoName)
				}
			}
		}
	}

	// Add worktree
	if err := gitops.WorktreeAdd(sourcePath, wtPath, branch); err != nil {
		return nil, ErrGit(err, "adding worktree for %s", repoName)
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

// Delete removes a workspace and its worktrees.
func (s *Service) Delete(name string) error {
	ws, err := s.State.GetWorkspace(name)
	if err != nil {
		return err
	}
	if ws == nil {
		return ErrWorkspaceNotFound(name)
	}

	logging.Info("deleting workspace %q", name)

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

			if err := gitops.DeleteBranch(repo.SourceRepo, repo.Branch, true); err != nil {
				logging.Warn("failed to delete branch %q in %s: %s", repo.Branch, repo.RepoName, err)
				console.Warningf("%s: failed to delete branch %s: %s", repo.RepoName, repo.Branch, err)
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
		return ErrWorkspaceNotFound(oldName)
	}

	existing, err := s.State.GetWorkspace(newName)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrWorkspaceExists(newName)
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
		return ErrWorkspaceNotFound(wsName)
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
			return ErrRepoNotFound(repoName)
		}

		rw, err := provisionWorktree(sourcePath, repoName, ws.Path, ws.Branch)
		if err != nil {
			return fmt.Errorf("adding %s: %w", repoName, err)
		}

		ws.Repos = append(ws.Repos, *rw)
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
		return ErrWorkspaceNotFound(wsName)
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
		console.Warningf("%s: fetch failed, using local state: %s", r.RepoName, err)
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
		return ErrWorkspaceNotFound(wsName)
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

// RepoStatus is one repo's git state within a workspace. Ahead/Behind are
// strings because "-" means "could not be determined" — a distinct outcome from
// zero that agents need to see rather than have flattened into 0.
type RepoStatus struct {
	Repo   string         `json:"repo"`
	Branch string         `json:"branch"`
	Status string         `json:"status"`
	Ahead  string         `json:"ahead"`
	Behind string         `json:"behind"`
	PR     *gitops.PRInfo `json:"pr,omitempty"`
}

// Clean reports whether the worktree has no uncommitted changes.
func (r RepoStatus) Clean() bool { return r.Status == "clean" || r.Status == "" }

// StatusReport is the full status of a workspace: the data behind both the human
// table and the machine envelope, so the two can never disagree.
type StatusReport struct {
	Workspace string                  `json:"workspace"`
	Path      string                  `json:"path"`
	Branch    string                  `json:"branch"`
	Source    *models.WorkspaceSource `json:"source,omitempty"`
	Repos     []RepoStatus            `json:"repos"`
}

// Dirty returns the names of repos with uncommitted changes.
func (r *StatusReport) Dirty() []string {
	var names []string
	for _, repo := range r.Repos {
		if !repo.Clean() {
			names = append(names, repo.Repo)
		}
	}
	return names
}

// Behind reports whether any repo has commits to pull in from its base branch.
func (r *StatusReport) Behind() bool {
	for _, repo := range r.Repos {
		if repo.Behind != "" && repo.Behind != "-" && repo.Behind != "0" {
			return true
		}
	}
	return false
}

func collectRepoStatus(r models.RepoWorktree) RepoStatus {
	rs := RepoStatus{
		Repo:   r.RepoName,
		Branch: r.Branch,
	}
	if branch, err := gitops.CurrentBranch(r.WorktreePath); err == nil {
		if branch == "" {
			rs.Branch = "(detached)"
		} else {
			rs.Branch = branch
		}
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

// formatSourceLine renders a workspace's source provenance as a single line for
// status output, or "" if there is no source. e.g.
// "Source: github 1172 — Surface data source status  (https://github.com/...)".
func formatSourceLine(src *models.WorkspaceSource) string {
	if src == nil {
		return ""
	}
	label := src.Provider
	if label == "" {
		label = "source"
	}
	if src.Ref != "" {
		label += " " + src.Ref
	}
	if src.Title != "" {
		label += " — " + src.Title
	}
	if src.URL != "" {
		label += "  (" + src.URL + ")"
	}
	return "Source: " + label
}

// StatusOptions controls status output.
type StatusOptions struct {
	JSON    bool
	Verbose bool
	PR      bool
}

// StatusReport collects git status for a workspace without printing anything.
func (s *Service) StatusReport(wsName string, opts StatusOptions) (*StatusReport, error) {
	ws, err := s.State.GetWorkspace(wsName)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, ErrWorkspaceNotFound(wsName)
	}

	return &StatusReport{
		Workspace: ws.Name,
		Path:      ws.Path,
		Branch:    ws.Branch,
		Source:    ws.Source,
		Repos:     s.fetchStatusResults(ws.Repos, opts.PR),
	}, nil
}

// Status displays git status for a workspace as a human-oriented table.
func (s *Service) Status(wsName string, opts StatusOptions) error {
	report, err := s.StatusReport(wsName, opts)
	if err != nil {
		return err
	}

	if opts.JSON {
		return s.printStatusJSON(report)
	}

	s.printStatusTable(report, opts)
	s.printVerboseStatus(report.Repos, opts)
	return nil
}

func (s *Service) fetchStatusResults(repos []models.RepoWorktree, withPR bool) []RepoStatus {
	results := make([]RepoStatus, len(repos))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func(idx int, repo models.RepoWorktree) {
			defer wg.Done()
			results[idx] = collectRepoStatus(repo)
			if withPR {
				results[idx].PR = gitops.PRStatus(repo.WorktreePath)
			}
		}(i, r)
	}
	wg.Wait()
	return results
}

// printStatusJSON emits the legacy bare-object shape behind the deprecated
// `--json` flag. New consumers should use `--format json`, which wraps the same
// StatusReport in the versioned envelope.
func (s *Service) printStatusJSON(report *StatusReport) error {
	data, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(data))
	return nil
}

func (s *Service) printStatusTable(report *StatusReport, opts StatusOptions) {
	fmt.Fprintf(os.Stdout, "Workspace: %s  (%s)\n", report.Workspace, report.Path)
	if line := formatSourceLine(report.Source); line != "" {
		fmt.Fprintf(os.Stdout, "%s\n", line)
	}
	fmt.Fprintln(os.Stdout)

	headers := []string{"Repo", "Branch", "↑↓", "Status"}
	if opts.PR {
		headers = []string{"Repo", "Branch", "↑↓", "PR", "Status"}
	}
	table := console.NewTable(os.Stdout, headers)

	for _, rs := range report.Repos {
		table.AddRow(statusRow(rs, opts.PR))
	}
	table.Render()
}

func statusRow(rs RepoStatus, withPR bool) []string {
	upDown := formatUpDown(rs.Ahead, rs.Behind)
	statusStr := formatStatus(rs.Status)
	if withPR {
		prStr := "-"
		if rs.PR != nil {
			prStr = formatPR(rs.PR)
		}
		return []string{rs.Repo, rs.Branch, upDown, prStr, statusStr}
	}
	return []string{rs.Repo, rs.Branch, upDown, statusStr}
}

func formatUpDown(ahead, behind string) string {
	if ahead != "-" && behind != "-" && ahead != "" && behind != "" {
		return fmt.Sprintf("%s↑ %s↓", ahead, behind)
	}
	return "-"
}

func formatStatus(status string) string {
	if status == "clean" || status == "" || strings.HasPrefix(status, "error:") {
		return status
	}
	lines := strings.Count(status, "\n") + 1
	return fmt.Sprintf("%d changed", lines)
}

func (s *Service) printVerboseStatus(results []RepoStatus, opts StatusOptions) {
	if !opts.Verbose {
		return
	}
	for _, rs := range results {
		if rs.Status != "clean" && rs.Status != "" && !strings.HasPrefix(rs.Status, "error:") {
			fmt.Fprintf(os.Stderr, "\n%s:\n%s\n", rs.Repo, rs.Status)
		}
	}
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

	for _, ws := range workspaces {
		f, iss := s.checkWorkspaceExists(ws, fix)
		if f > 0 {
			fixed += f
		}
		issues = append(issues, iss...)
		if len(iss) > 0 {
			continue
		}

		f, iss = s.checkStaleMCPConfig(ws, fix)
		fixed += f
		issues = append(issues, iss...)

		f, iss = s.checkWorkspaceRepos(&ws, fix)
		fixed += f
		issues = append(issues, iss...)
	}

	return issues, fixed, nil
}

// checkStaleMCPConfig flags the legacy `grove` entry in a workspace's
// `.mcp.json`, left behind by Grove versions that shipped a built-in MCP
// server. `--fix` removes just that entry.
func (s *Service) checkStaleMCPConfig(ws models.Workspace, fix bool) (int, []models.DoctorIssue) {
	if !StaleMCPEntry(ws.Path) {
		return 0, nil
	}
	issue := models.DoctorIssue{
		Workspace:       ws.Name,
		Repo:            nil,
		Issue:           "stale grove entry in .mcp.json (built-in MCP server was removed)",
		SuggestedAction: "remove the grove entry from .mcp.json",
	}
	if fix && CleanStaleMCPEntry(ws.Path) {
		return 1, []models.DoctorIssue{issue}
	}
	return 0, []models.DoctorIssue{issue}
}

func (s *Service) checkWorkspaceExists(ws models.Workspace, fix bool) (int, []models.DoctorIssue) {
	if _, err := os.Stat(ws.Path); err == nil {
		return 0, nil
	}
	issue := models.DoctorIssue{
		Workspace:       ws.Name,
		Repo:            nil,
		Issue:           "workspace directory missing",
		SuggestedAction: "remove stale state entry",
	}
	if fix {
		s.State.RemoveWorkspace(ws.Name)
		return 1, []models.DoctorIssue{issue}
	}
	return 0, []models.DoctorIssue{issue}
}

func (s *Service) checkWorkspaceRepos(ws *models.Workspace, fix bool) (int, []models.DoctorIssue) {
	var issues []models.DoctorIssue
	var toRemove []string
	fixed := 0

	for _, r := range ws.Repos {
		if iss, shouldRemove := s.checkRepo(ws.Name, r); iss != nil {
			issues = append(issues, *iss)
			if fix && shouldRemove {
				toRemove = append(toRemove, r.RepoName)
				fixed++
			}
		}
	}

	if fix && len(toRemove) > 0 {
		if currentWS, err := s.State.GetWorkspace(ws.Name); err == nil && currentWS != nil {
			for _, name := range toRemove {
				currentWS.RemoveRepo(name)
			}
			s.State.UpdateWorkspace(*currentWS)
		}
	}

	return fixed, issues
}

func (s *Service) checkRepo(wsName string, r models.RepoWorktree) (*models.DoctorIssue, bool) {
	repoName := r.RepoName

	if _, err := os.Stat(r.SourceRepo); os.IsNotExist(err) {
		return &models.DoctorIssue{
			Workspace:       wsName,
			Repo:            &repoName,
			Issue:           "source repo missing",
			SuggestedAction: "remove stale repo entry",
		}, true
	}

	if _, err := os.Stat(r.WorktreePath); os.IsNotExist(err) {
		return &models.DoctorIssue{
			Workspace:       wsName,
			Repo:            &repoName,
			Issue:           "worktree directory missing",
			SuggestedAction: "remove stale repo entry",
		}, true
	}

	return nil, false
}
