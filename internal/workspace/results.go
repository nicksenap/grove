package workspace

import "github.com/nicksenap/grove/internal/models"

// Per-repo outcome vocabulary. These strings are part of the machine CLI
// contract (docs/agent-cli.md): an agent branches on them to decide what to do
// next, so they are stable identifiers rather than prose.
const (
	// Create / add.
	OutcomeCreated       = "created"
	OutcomeAdded         = "added"
	OutcomeAlreadyExists = "already_present"

	// Delete / remove.
	OutcomeRemoved  = "removed"
	OutcomeNotFound = "not_found"

	// Sync.
	OutcomeRebased  = "rebased"
	OutcomeUpToDate = "up_to_date"
	OutcomeSkipped  = "skipped"

	// Run.
	OutcomeExited = "exited"

	// Any operation.
	OutcomeFailed = "failed"
)

// RepoResult is one repo's outcome within a multi-repo operation. Multi-repo
// work is partially failable by nature, so every operation reports per-repo
// results instead of collapsing them into a single boolean.
type RepoResult struct {
	Repo    string `json:"repo"`
	Outcome string `json:"outcome"`
	Branch  string `json:"branch,omitempty"`
	Path    string `json:"path,omitempty"`
	// Detail explains a non-obvious outcome: why a repo was skipped, what
	// failed, or how many commits a rebase moved.
	Detail string `json:"detail,omitempty"`
}

// Failed reports whether this repo's operation did not achieve its goal.
func (r RepoResult) Failed() bool { return r.Outcome == OutcomeFailed }

// CreateResult describes a created workspace.
type CreateResult struct {
	Name     string                  `json:"name"`
	Path     string                  `json:"path"`
	Branch   string                  `json:"branch"`
	Source   *models.WorkspaceSource `json:"source,omitempty"`
	Repos    []RepoResult            `json:"repos"`
	Replaced string                  `json:"replaced,omitempty"`
}

// DeleteResult describes a deleted workspace. StateRemoved is false when some
// worktree could not be removed and the state entry was deliberately kept, so an
// agent can tell "gone" from "partially gone".
type DeleteResult struct {
	Name         string       `json:"name"`
	Path         string       `json:"path"`
	Repos        []RepoResult `json:"repos"`
	StateRemoved bool         `json:"state_removed"`
}

// SyncResult describes a sync across a workspace's repos.
type SyncResult struct {
	Workspace string       `json:"workspace"`
	Repos     []RepoResult `json:"repos"`
}

// ReposChangeResult describes an add-repo / remove-repo operation.
type ReposChangeResult struct {
	Workspace string       `json:"workspace"`
	Repos     []RepoResult `json:"repos"`
}

// RunResult describes one `gw run` invocation. ExitCode is the child process's
// status; -1 means it never started.
type RunResult struct {
	Workspace string          `json:"workspace"`
	Repos     []RunRepoResult `json:"repos"`
}

// RunRepoResult is one repo's run-hook outcome.
type RunRepoResult struct {
	Repo     string `json:"repo"`
	Outcome  string `json:"outcome"`
	ExitCode int    `json:"exit_code"`
	Detail   string `json:"detail,omitempty"`
}

// FailedRepos returns the names of repos whose operation failed.
func FailedRepos(results []RepoResult) []string {
	var names []string
	for _, r := range results {
		if r.Failed() {
			names = append(names, r.Repo)
		}
	}
	return names
}
