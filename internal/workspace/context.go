package workspace

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
)

// Context is the answer to "where am I and what can I do?" — the single
// read-only snapshot an agent fetches before deciding anything.
//
// It is a projection over existing state and git, not a new state model: every
// field is derived from ~/.grove/config.toml, ~/.grove/state.json, or a git
// query. Nothing here is cached or persisted.
//
// Fields for features that do not exist in core yet (blueprint identity,
// preparation/Oven status) are deliberately absent rather than stubbed as null;
// they are additive when those features land, which the schema policy allows.
type Context struct {
	// Environment.
	GroveVersion string `json:"grove_version"`
	Initialized  bool   `json:"initialized"`
	ConfigPath   string `json:"config_path"`
	Cwd          string `json:"cwd"`

	// Configuration.
	RepoDirs     []string `json:"repo_dirs"`
	WorkspaceDir string   `json:"workspace_dir"`
	Presets      []string `json:"presets"`

	// Current position. Workspace is null when the cwd is not inside one, which
	// is the signal that a name must be passed explicitly to other commands.
	Workspace *WorkspaceContext `json:"workspace"`

	// Everything else that exists.
	WorkspaceCount int      `json:"workspace_count"`
	Workspaces     []string `json:"workspaces"`
}

// WorkspaceContext describes the workspace the caller is currently inside.
type WorkspaceContext struct {
	Name   string                  `json:"name"`
	Path   string                  `json:"path"`
	Branch string                  `json:"branch"`
	Source *models.WorkspaceSource `json:"source,omitempty"`
	Repos  []RepoContext           `json:"repos"`
}

// RepoContext is one repo's live state. It embeds RepoStatus so `gw context` and
// `gw status` report identical git state for the same repo — two commands
// describing the same thing differently is how agents get confused.
type RepoContext struct {
	RepoStatus
	SourceRepo string `json:"source_repo"`
	Path       string `json:"path"`
	Remote     string `json:"remote,omitempty"`
	BaseBranch string `json:"base_branch,omitempty"`
	Dirty      bool   `json:"dirty"`
}

// Context assembles a snapshot for the given working directory. cfg may be nil,
// which reports Initialized: false instead of failing — an agent's first call is
// exactly how it should discover that Grove needs `gw init`.
//
// Only local git queries are used (no fetch, no PR lookups), so the command stays
// cheap enough to call before every decision.
func (s *Service) Context(cwd, version string, cfg *models.Config) (*Context, error) {
	ctx := &Context{
		GroveVersion: version,
		Initialized:  cfg != nil,
		ConfigPath:   config.ConfigPath,
		Cwd:          cwd,
		RepoDirs:     []string{},
		Presets:      []string{},
		Workspaces:   []string{},
	}

	if cfg != nil {
		if cfg.RepoDirs != nil {
			ctx.RepoDirs = cfg.RepoDirs
		}
		ctx.WorkspaceDir = cfg.WorkspaceDir
		for name := range cfg.Presets {
			ctx.Presets = append(ctx.Presets, name)
		}
		sort.Strings(ctx.Presets)
	}

	all, err := s.State.Load()
	if err != nil {
		return nil, err
	}
	ctx.WorkspaceCount = len(all)
	for _, ws := range all {
		ctx.Workspaces = append(ctx.Workspaces, ws.Name)
	}

	current := findWorkspaceByPath(all, cwd)
	if current == nil {
		return ctx, nil
	}

	ctx.Workspace = &WorkspaceContext{
		Name:   current.Name,
		Path:   current.Path,
		Branch: current.Branch,
		Source: current.Source,
		Repos:  s.repoContexts(current.Repos),
	}
	return ctx, nil
}

// repoContexts collects live git state for each repo in parallel — the same
// concurrency the rest of the service uses, so context cost is one git round
// regardless of repo count.
func (s *Service) repoContexts(repos []models.RepoWorktree) []RepoContext {
	out := make([]RepoContext, len(repos))
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func(idx int, repo models.RepoWorktree) {
			defer wg.Done()
			status := collectRepoStatus(repo)
			// The same resolution sync uses, so the reported base branch is the
			// one a rebase would actually target.
			base, _ := gitops.ResolveBaseBranch(repo.SourceRepo)
			out[idx] = RepoContext{
				RepoStatus: status,
				SourceRepo: repo.SourceRepo,
				Path:       repo.WorktreePath,
				Remote:     gitops.RemoteURL(repo.WorktreePath, "origin"),
				BaseBranch: base,
				Dirty:      !status.Clean(),
			}
		}(i, r)
	}
	wg.Wait()
	return out
}

// findWorkspaceByPath resolves the innermost workspace containing path. It works
// on an already-loaded slice so Context does a single state read.
func findWorkspaceByPath(all []models.Workspace, path string) *models.Workspace {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}

	var best *models.Workspace
	for i := range all {
		wsPath := all[i].Path
		if resolved, err := filepath.EvalSymlinks(wsPath); err == nil {
			wsPath = resolved
		}
		if abs == wsPath || isSubPath(wsPath, abs) {
			// Prefer the deepest match, so a workspace nested inside another
			// workspace's directory still resolves to itself.
			if best == nil || len(wsPath) > len(best.Path) {
				best = &all[i]
			}
		}
	}
	return best
}

// isSubPath reports whether child is inside parent. It rejects ".." results so a
// sibling directory sharing a name prefix is never mistaken for a child.
func isSubPath(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	if filepath.IsAbs(rel) || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
