package models

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// RepoWorktree represents a single repo's worktree within a workspace.
type RepoWorktree struct {
	RepoName       string `json:"repo_name"`
	SourceRepo     string `json:"source_repo"`
	WorktreePath   string `json:"worktree_path"`
	Branch         string `json:"branch"`
	PreserveBranch bool   `json:"preserve_branch,omitempty"`
}

// Workspace represents a named collection of worktrees.
type Workspace struct {
	Name      string           `json:"name"`
	Path      string           `json:"path"`
	Branch    string           `json:"branch"`
	CreatedAt string           `json:"created_at"`
	Repos     []RepoWorktree   `json:"repos"`
	Source    *WorkspaceSource `json:"source,omitempty"`
}

// WorkspaceSource records where a workspace was seeded from (e.g. a GitHub PR,
// a Notion page, a Slack thread). All fields are opaque to Grove core — they are
// stored and displayed but never interpreted here; resolution lives in plugins.
// The pointer + omitempty keeps existing state.json entries round-tripping
// unchanged (a nil Source is omitted entirely).
type WorkspaceSource struct {
	Provider string `json:"provider"`        // e.g. "github", "gitlab", "notion", "slack"
	URL      string `json:"url"`             // the original source URL
	Ref      string `json:"ref,omitempty"`   // provider ref: PR number, page id, message ts
	Title    string `json:"title,omitempty"` // human-readable title for display
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
	Hooks        map[string]Hook   `toml:"hooks"`
	// Legacy field — auto-migrated to RepoDirs
	ReposDir string `toml:"repos_dir"`
}

// Hook is a global lifecycle hook in ~/.grove/config.toml [hooks]. It accepts
// either a bare command string or a table that adds metadata describing what
// the hook does and how it should run:
//
//	[hooks]
//	pre_delete = "gw claude sync harvest {path}"   # bare string (simple form)
//
//	[hooks.post_create]                            # table form (with metadata)
//	command     = "npm install && npm run build"
//	description = "Install deps and build assets"
//	stream      = true                             # stream output live
//	timeout     = "5m"                             # abort if it runs too long
//	on_failure  = "warn"                           # "warn" (default) or "abort"
//
// When stream is false (the default) the hook's output is captured and only
// shown if the hook fails, so a clean run stays quiet but failures are no
// longer an opaque "exit status 1".
type Hook struct {
	Command     string `toml:"command"`
	Description string `toml:"description"`
	Stream      bool   `toml:"stream"`
	// Timeout is a Go duration string (e.g. "30s", "5m"). Empty means no limit.
	Timeout string `toml:"timeout"`
	// OnFailure is "warn" (default — log a warning and continue) or "abort"
	// (treat a hook failure as a fatal error for the command).
	OnFailure string `toml:"on_failure"`
}

// UnmarshalTOML accepts either a bare command string or a table with metadata,
// so existing string-form hooks keep working unchanged.
func (h *Hook) UnmarshalTOML(data interface{}) error {
	switch v := data.(type) {
	case string:
		h.Command = v
	case map[string]interface{}:
		if c, ok := v["command"].(string); ok {
			h.Command = c
		}
		if d, ok := v["description"].(string); ok {
			h.Description = d
		}
		if s, ok := v["stream"].(bool); ok {
			h.Stream = s
		}
		if t, ok := v["timeout"].(string); ok {
			h.Timeout = t
		}
		if f, ok := v["on_failure"].(string); ok {
			h.OnFailure = f
		}
	default:
		return fmt.Errorf("expected a hook command string or table, got %T", data)
	}
	return nil
}

// MarshalTOML keeps the on-disk format minimal: a hook with no metadata is
// written back as a bare string (matching how most users author it), while a
// hook carrying metadata is written as an inline table. This means
// config.Save() never rewrites a plain string hook into a verbose table.
func (h Hook) MarshalTOML() ([]byte, error) {
	if h.isBare() {
		return []byte(strconv.Quote(h.Command)), nil
	}
	var parts []string
	parts = append(parts, "command = "+strconv.Quote(h.Command))
	if h.Description != "" {
		parts = append(parts, "description = "+strconv.Quote(h.Description))
	}
	if h.Stream {
		parts = append(parts, "stream = true")
	}
	if h.Timeout != "" {
		parts = append(parts, "timeout = "+strconv.Quote(h.Timeout))
	}
	if h.OnFailure != "" {
		parts = append(parts, "on_failure = "+strconv.Quote(h.OnFailure))
	}
	return []byte("{" + strings.Join(parts, ", ") + "}"), nil
}

// isBare reports whether the hook carries no metadata beyond its command.
func (h Hook) isBare() bool {
	return h.Description == "" && !h.Stream && h.Timeout == "" && h.OnFailure == ""
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

// ToJSON marshals to indented JSON.
func ToJSON(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}
