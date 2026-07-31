package workspace

import (
	"fmt"

	"github.com/nicksenap/grove/internal/machine"
)

// Classified errors for the machine CLI contract. Constructing them here — next
// to the logic that detects each condition — keeps the CLI boundary free of
// message pattern-matching and guarantees the human and machine paths report the
// same cause.

// ErrWorkspaceNotFound reports a workspace name that is not in state.
func ErrWorkspaceNotFound(name string) *machine.Error {
	return machine.Errorf(machine.CodeWorkspaceNotFound, "workspace %s not found", name).
		WithFix("List existing workspaces, or create this one").
		WithActions(
			machine.NextAction("List workspaces", "gw list --format json"),
		)
}

// ErrNotInWorkspace reports that no workspace could be inferred from the cwd.
func ErrNotInWorkspace() *machine.Error {
	return machine.Errorf(machine.CodeWorkspaceNotFound,
		"not inside a workspace. Provide a workspace name or cd into one").
		WithFix("Pass a workspace name explicitly, or run from inside a workspace directory").
		WithActions(
			machine.NextAction("Discover current context", "gw context --format json"),
			machine.NextAction("List workspaces", "gw list --format json"),
		)
}

// ErrWorkspaceExists reports a name collision on create/rename.
func ErrWorkspaceExists(name string) *machine.Error {
	return machine.Errorf(machine.CodeWorkspaceExists, "workspace %s already exists", name).
		WithFix("Pick a different name, or delete the existing workspace first").
		WithActions(
			machine.NextAction("Inspect the existing workspace", "gw status "+name+" --format json"),
		)
}

// ErrRepoNotFound reports a repo that is not discoverable or not in a workspace.
func ErrRepoNotFound(name string) *machine.Error {
	return machine.Errorf(machine.CodeRepoNotFound, "repo %s not found", name).
		WithFix("Check the repo name against discovered repos").
		WithActions(
			machine.NextAction("List discovered repos", "gw repos --format json"),
		)
}

// ErrWorktreeExists reports that a branch already has a worktree in a repo, so
// git cannot check it out again.
func ErrWorktreeExists(branch, repo string) *machine.Error {
	return machine.Errorf(machine.CodeWorktreeExists,
		"branch %s already has a worktree in %s", branch, repo).
		WithFix("Use a different branch name, or remove the existing worktree").
		WithActions(
			machine.NextAction("Find the workspace holding that branch", "gw list --status --format json"),
		)
}

// ErrGit wraps a failed git subprocess.
func ErrGit(err error, format string, args ...any) *machine.Error {
	return machine.Wrap(machine.CodeGitFailed, err, "%s: %s", fmt.Sprintf(format, args...), err)
}
