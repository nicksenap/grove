package cmd

import (
	"errors"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/lifecycle"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

// deleteCmd is the top-level "gw delete" command.
var deleteCmd = &cobra.Command{
	Use:   "delete [NAME]",
	Short: "Delete a workspace (shortcut for gw ws delete)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		doDelete(args)
	},
}

// wsDeleteCmd is "gw ws delete".
var wsDeleteCmd = &cobra.Command{
	Use:   "delete [NAME]",
	Short: "Delete a workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		doDelete(args)
	},
}

func doDelete(args []string) {
	var names []string

	if len(args) > 0 {
		names = []string{args[0]}
	} else {
		workspaces, err := state.Load()
		if err != nil {
			exitError(err.Error())
		}
		if len(workspaces) == 0 {
			exitError("No workspaces to delete")
		}
		choices := make([]string, len(workspaces))
		for i, ws := range workspaces {
			choices[i] = ws.Name
		}
		selected, err := picker.PickMany("Select workspaces to delete:", choices)
		if err != nil {
			exitOnPickerErr(err)
		}
		names = selected
	}

	for _, name := range names {
		// pre_delete is the safety-policy extension point: configure a hook with
		// on_failure = "abort" to inspect or block this destructive operation.
		ws, _ := state.GetWorkspace(name)
		if ws != nil {
			vars := lifecycle.Vars{Name: name, Path: ws.Path, Branch: ws.Branch}
			if err := lifecycle.Run("pre_delete", vars); err != nil && !errors.Is(err, lifecycle.ErrNoHook) {
				if lifecycle.ShouldAbort(err) {
					exitError(err.Error())
				}
				console.Warning(err.Error())
			}
		}

		if err := workspace.NewService().DeleteWithOptions(name, workspace.RemoveOptions{Force: true}); err != nil {
			exitError(err.Error())
		}
	}
}

func init() {
	deleteCmd.ValidArgsFunction = completeWorkspaceNames
	wsDeleteCmd.ValidArgsFunction = completeWorkspaceNames
}
