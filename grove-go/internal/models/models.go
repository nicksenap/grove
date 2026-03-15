// Package models defines the core data types for Grove.
package models

import (
	"path/filepath"
	"time"
)

// Config holds the Grove configuration loaded from ~/.grove/config.toml.
type Config struct {
	RepoDirs      []string            `toml:"repo_dirs" json:"repo_dirs"`
	WorkspaceDir  string              `toml:"workspace_dir" json:"workspace_dir"`
	Presets       map[string][]string `toml:"presets" json:"presets,omitempty"`
	ClaudeMemSync bool                `toml:"claude_memory_sync" json:"claude_memory_sync"`
}

// RepoWorktree represents a single repo's worktree inside a workspace.
type RepoWorktree struct {
	RepoName     string `json:"repo_name"`
	SourceRepo   string `json:"source_repo"`
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
}

// Workspace represents a named collection of repo worktrees sharing a branch.
type Workspace struct {
	Name      string         `json:"name"`
	Path      string         `json:"path"`
	Branch    string         `json:"branch"`
	Repos     []RepoWorktree `json:"repos"`
	CreatedAt string         `json:"created_at"`
}

// NewWorkspace creates a workspace with the current timestamp.
func NewWorkspace(name, path, branch string, repos []RepoWorktree) Workspace {
	return Workspace{
		Name:      name,
		Path:      path,
		Branch:    branch,
		Repos:     repos,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// RepoWorktreeDir returns the worktree directory for a repo within a workspace.
func RepoWorktreeDir(workspacePath, repoName string) string {
	return filepath.Join(workspacePath, repoName)
}

// GroveToml represents per-repo configuration from .grove.toml.
type GroveToml struct {
	BaseBranch string   `toml:"base_branch"`
	Setup      []string `toml:"-"` // Handled manually (can be string or []string)
	Teardown   []string `toml:"-"`
	Run        []string `toml:"-"`
	PreSync    []string `toml:"-"`
	PostSync   []string `toml:"-"`
	PreRun     []string `toml:"-"`
	PostRun    []string `toml:"-"`
}

// RepoInfo holds discovered repository metadata.
type RepoInfo struct {
	Name        string
	Path        string
	Remote      string
	DisplayName string
}

// DoctorIssue represents a problem found by the diagnose command.
type DoctorIssue struct {
	Level     string // "error" or "warning"
	Message   string
	Fix       string // Description of auto-fix action
	FixFunc   func() error
	Workspace string
	RepoName  string
}
