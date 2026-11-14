package output

import "github.com/nicksenap/grove/internal/models"

// Workspace is the JSON shape of a workspace on stdout (`gw list`, `gw ws show`).
//
// This is the public contract for plugins and scripts. It is deliberately a
// separate type from models.Workspace, which is the private state.json format:
// the state file may change freely, this shape may only gain fields.
type Workspace struct {
	Name      string           `json:"name"`
	Path      string           `json:"path"`
	Branch    string           `json:"branch"`
	CreatedAt string           `json:"created_at"`
	Repos     []WorkspaceRepo  `json:"repos"`
	Source    *WorkspaceSource `json:"source,omitempty"`
}

// WorkspaceRepo is one repo worktree inside a Workspace.
type WorkspaceRepo struct {
	RepoName       string `json:"repo_name"`
	SourceRepo     string `json:"source_repo"`
	WorktreePath   string `json:"worktree_path"`
	Branch         string `json:"branch"`
	PreserveBranch bool   `json:"preserve_branch,omitempty"`
}

// WorkspaceSource is where a workspace was seeded from (e.g. a GitHub PR).
type WorkspaceSource struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
	Ref      string `json:"ref,omitempty"`
	Title    string `json:"title,omitempty"`
}

// FromWorkspace converts a stored workspace to its stdout shape.
func FromWorkspace(ws models.Workspace) Workspace {
	repos := make([]WorkspaceRepo, len(ws.Repos))
	for i, r := range ws.Repos {
		repos[i] = WorkspaceRepo{
			RepoName:       r.RepoName,
			SourceRepo:     r.SourceRepo,
			WorktreePath:   r.WorktreePath,
			Branch:         r.Branch,
			PreserveBranch: r.PreserveBranch,
		}
	}
	return Workspace{
		Name:      ws.Name,
		Path:      ws.Path,
		Branch:    ws.Branch,
		CreatedAt: ws.CreatedAt,
		Repos:     repos,
		Source:    FromWorkspaceSource(ws.Source),
	}
}

// FromWorkspaces converts stored workspaces to their stdout shape.
func FromWorkspaces(workspaces []models.Workspace) []Workspace {
	out := make([]Workspace, len(workspaces))
	for i, ws := range workspaces {
		out[i] = FromWorkspace(ws)
	}
	return out
}

// FromWorkspaceSource converts a stored source to its stdout shape; nil stays nil.
func FromWorkspaceSource(src *models.WorkspaceSource) *WorkspaceSource {
	if src == nil {
		return nil
	}
	return &WorkspaceSource{Provider: src.Provider, URL: src.URL, Ref: src.Ref, Title: src.Title}
}
