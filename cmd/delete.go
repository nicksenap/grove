package cmd

import (
	"github.com/nicksenap/grove/internal/operations"
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
		if _, err := operations.NewService().Delete(operations.DeleteRequest{
			Name: name, Options: workspace.RemoveOptions{Force: true},
		}); err != nil {
			exitError(err.Error())
		}
	}
}

func init() {
	deleteCmd.ValidArgsFunction = completeWorkspaceNames
	wsDeleteCmd.ValidArgsFunction = completeWorkspaceNames
}
