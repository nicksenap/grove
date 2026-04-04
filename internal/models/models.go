package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// RepoWorktree represents a single repo's worktree within a workspace.
type RepoWorktree struct {
	RepoName     string `json:"repo_name"`
	SourceRepo   string `json:"source_repo"`
	WorktreePath string `json:"worktree_path"`
	Branch       string `json:"branch"`
}

// Workspace represents a named collection of worktrees.
type Workspace struct {
	Name      string         `json:"name"`
	Path      string         `json:"path"`
	Branch    string         `json:"branch"`
	CreatedAt string         `json:"created_at"`
	Repos     []RepoWorktree `json:"repos"`
}

// NewWorkspace creates a workspace with the current timestamp.
func NewWorkspace(name, path, branch string) Workspace {
	return Workspace{
		Name:      name,
		Path:      path,
		Branch:    branch,
		CreatedAt: time.Now().Format("2006-01-02T15:04:05.000000"),
		Repos:     []RepoWorktree{},
	}
}

// FindRepo finds a repo by name within the workspace.
func (w *Workspace) FindRepo(name string) *RepoWorktree {
	for i := range w.Repos {
		if w.Repos[i].RepoName == name {
			return &w.Repos[i]
		}
	}
	return nil
}

// RemoveRepo removes a repo by name, returns true if found.
func (w *Workspace) RemoveRepo(name string) bool {
	for i, r := range w.Repos {
		if r.RepoName == name {
			w.Repos = append(w.Repos[:i], w.Repos[i+1:]...)
			return true
		}
	}
	return false
}

// RepoNames returns the list of repo names.
func (w *Workspace) RepoNames() []string {
	names := make([]string, len(w.Repos))
	for i, r := range w.Repos {
		names[i] = r.RepoName
	}
	return names
}

// Preset defines a named set of repos.
type Preset struct {
	Repos []string `toml:"repos" json:"repos"`
}

// Config is the global Grove configuration (~/.grove/config.toml).
type Config struct {
	RepoDirs     []string          `toml:"repo_dirs"`
	WorkspaceDir string            `toml:"workspace_dir"`
	Presets      map[string]Preset `toml:"presets"`
	Hooks        map[string]string `toml:"hooks"`
	// Legacy field — auto-migrated to RepoDirs
	ReposDir string `toml:"repos_dir"`
}

// GroveConfig is per-repo .grove.toml configuration.
type GroveConfig struct {
	BaseBranch string       `toml:"base_branch"`
	Setup      StringOrList `toml:"setup"`
	Run        StringOrList `toml:"run"`
	PreRun     string       `toml:"pre_run"`
	PostRun    string       `toml:"post_run"`
	PreSync    string       `toml:"pre_sync"`
	PostSync   string       `toml:"post_sync"`
	Teardown   string       `toml:"teardown"`
}

// StringOrList handles TOML values that can be a string or list of strings.
type StringOrList []string

func (s *StringOrList) UnmarshalTOML(data interface{}) error {
	switch v := data.(type) {
	case string:
		*s = []string{v}
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if str, ok := item.(string); ok {
				result = append(result, str)
			} else {
				return fmt.Errorf("expected string in list, got %T", item)
			}
		}
		*s = result
	default:
		return fmt.Errorf("expected string or list, got %T", data)
	}
	return nil
}

// StatsEvent records a workspace lifecycle event.
type StatsEvent struct {
	Event         string   `json:"event"`
	Timestamp     string   `json:"timestamp"`
	WorkspaceName string   `json:"workspace_name"`
	Branch        string   `json:"branch"`
	RepoNames     []string `json:"repo_names"`
	RepoCount     int      `json:"repo_count"`
}

// DoctorIssue represents a health check finding.
type DoctorIssue struct {
	Workspace       string  `json:"workspace"`
	Repo            *string `json:"repo"`
	Issue           string  `json:"issue"`
	SuggestedAction string  `json:"suggested_action"`
}

// MCPConfig is the .mcp.json structure written to workspaces.
type MCPConfig struct {
	MCPServers map[string]MCPServer `json:"mcpServers"`
}

type MCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// ToJSON marshals to indented JSON.
func ToJSON(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
