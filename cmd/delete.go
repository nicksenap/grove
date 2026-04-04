package cmd

import (
	"fmt"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	deleteCmdForce   bool
	wsDeleteCmdForce bool
)

// deleteCmd is the top-level "gw delete" command.
var deleteCmd = &cobra.Command{
	Use:   "delete [NAME]",
	Short: "Delete a workspace (shortcut for gw ws delete)",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		doDelete(args, deleteCmdForce)
	},
}

// wsDeleteCmd is "gw ws delete".
var wsDeleteCmd = &cobra.Command{
	Use:   "delete [NAME]",
	Short: "Delete a workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		doDelete(args, wsDeleteCmdForce)
	},
}

func doDelete(args []string, force bool) {
	var names []string

	if len(args) > 0 {
		names = []string{args[0]}
	} else {
		// Interactive multi-select
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

	if !force {
		if !console.Confirm(fmt.Sprintf("Delete %s?", strings.Join(names, ", ")), false) {
			return
		}
	}

	for _, name := range names {
		if err := workspace.NewService().Delete(name); err != nil {
			exitError(err.Error())
		}
	}
}

func init() {
	deleteCmd.Flags().BoolVarP(&deleteCmdForce, "force", "f", false, "Skip confirmation")
	deleteCmd.ValidArgsFunction = completeWorkspaceNames

	wsDeleteCmd.Flags().BoolVarP(&wsDeleteCmdForce, "force", "f", false, "Skip confirmation")
	wsDeleteCmd.ValidArgsFunction = completeWorkspaceNames
}
