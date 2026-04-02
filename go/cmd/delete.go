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

var deleteForce bool

var deleteCmd = &cobra.Command{
	Use:   "delete [NAME]",
	Short: "Delete a workspace",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
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
				exitError(err.Error())
			}
			names = selected
		}

		if !deleteForce {
			if !console.Confirm(fmt.Sprintf("Delete %s?", strings.Join(names, ", ")), false) {
				return
			}
		}

		for _, name := range names {
			if err := workspace.Delete(name); err != nil {
				exitError(err.Error())
			}
		}
	},
}

func init() {
	deleteCmd.Flags().BoolVarP(&deleteForce, "force", "f", false, "Skip confirmation")
	deleteCmd.ValidArgsFunction = completeWorkspaceNames
}
