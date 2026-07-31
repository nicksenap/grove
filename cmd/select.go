package cmd

import (
	"strings"

	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/state"
)

// Shared argument resolution for the commands that take a workspace name and a
// repo list. These were near-copies in create/delete/rename/add-repo/remove-repo,
// and the copies had drifted: the same "no workspaces exist" condition produced
// NO_WORKSPACES (exit 3) in two commands and INTERNAL (exit 1) in another, which
// makes the error code unusable to an agent that does not know which command it
// happened to call.

// pickWorkspaceName resolves a workspace name interactively. In machine mode the
// picker refuses to run and returns a USAGE error, so the caller does not need to
// special-case that.
func pickWorkspaceName(prompt string) string {
	workspaces, err := state.Load()
	if err != nil {
		fail(err)
	}
	if len(workspaces) == 0 {
		fail(noWorkspacesErr())
	}

	choices := make([]string, len(workspaces))
	for i, ws := range workspaces {
		choices[i] = ws.Name
	}

	selected, err := picker.PickOne(prompt, choices)
	if err != nil {
		exitOnPickerErr(err)
	}
	return selected
}

// pickWorkspaceNames resolves one or more workspace names interactively.
func pickWorkspaceNames(prompt string) []string {
	workspaces, err := state.Load()
	if err != nil {
		fail(err)
	}
	if len(workspaces) == 0 {
		fail(noWorkspacesErr())
	}

	choices := make([]string, len(workspaces))
	for i, ws := range workspaces {
		choices[i] = ws.Name
	}

	selected, err := picker.PickMany(prompt, choices)
	if err != nil {
		exitOnPickerErr(err)
	}
	return selected
}

// noWorkspacesErr is the single definition of "there is nothing to operate on".
func noWorkspacesErr() *machine.Error {
	return machine.Errorf(machine.CodeNoWorkspaces, "no workspaces exist").
		WithFix("Create one first").
		WithActions(machine.NextAction("Create a workspace",
			"gw create <name> -r <repo1,repo2> -b <branch> --format json"))
}

// parseRepoList splits a comma-separated --repos value.
//
// It drops empty entries, so `-r "api,"` or `-r "api, ,web"` names the repos the
// user meant instead of reporting `repo  not found` for an empty string.
func parseRepoList(value string) []string {
	var repos []string
	for _, raw := range strings.Split(value, ",") {
		if name := strings.TrimSpace(raw); name != "" {
			repos = append(repos, name)
		}
	}
	return repos
}
