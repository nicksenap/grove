package cmd

import (
	"os"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/output"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	listJSON     bool
	listOutput   string
	listStatus   bool
	wsShowJSON   bool
	wsShowOutput string
)

// wsCmd is the "gw ws" subcommand group.
var wsCmd = &cobra.Command{
	Use:   "ws",
	Short: "Manage workspaces",
}

// wsListCmd is "gw ws list".
var wsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workspaces",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doListAll()
	},
}

// wsShowCmd is "gw ws show <name>".
var wsShowCmd = &cobra.Command{
	Use:   "show NAME",
	Short: "Show details for a workspace",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		doShowOne(args[0])
	},
	ValidArgsFunction: completeWorkspaceNames,
}

// listCmd is the top-level alias "gw list" → "gw ws list".
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workspaces (shortcut for gw ws list)",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		doListAll()
	},
}

func init() {
	wsListCmd.Flags().BoolVarP(&listJSON, "json", "j", false, "Output as JSON (compatibility alias for --output json)")
	wsListCmd.Flags().StringVarP(&listOutput, "output", "o", "", "Output format: table, json, jsonl, tsv, name, path")
	wsListCmd.Flags().BoolVarP(&listStatus, "status", "s", false, "Include git status")
	wsShowCmd.Flags().BoolVarP(&wsShowJSON, "json", "j", false, "Output as JSON (compatibility alias for --output json)")
	wsShowCmd.Flags().StringVarP(&wsShowOutput, "output", "o", "", "Output format: table, json, jsonl, tsv, name, path")
	wsCmd.AddCommand(wsListCmd, wsShowCmd, wsDeleteCmd)

	listCmd.Flags().BoolVarP(&listJSON, "json", "j", false, "Output as JSON (compatibility alias for --output json)")
	listCmd.Flags().StringVarP(&listOutput, "output", "o", "", "Output format: table, json, jsonl, tsv, name, path")
	listCmd.Flags().BoolVarP(&listStatus, "status", "s", false, "Include git status")
}

func doListAll() {
	format, err := resolveQueryOutput(listOutput, listJSON)
	if err != nil {
		exitError(err.Error())
	}

	if listStatus {
		listWithStatus(format)
		return
	}

	workspaces, err := state.Load()
	if err != nil {
		exitError(err.Error())
	}

	if len(workspaces) == 0 && format == output.Table {
		console.Info("No workspaces. Create one with: gw create <name> -r repo1,repo2 -b branch")
		return
	}
	if err := writeWorkspaceList(os.Stdout, workspaces, format); err != nil {
		exitError(err.Error())
	}
}

func listWithStatus(format output.Format) {
	summaries, err := workspace.NewService().AllWorkspacesSummary()
	if err != nil {
		exitError(err.Error())
	}

	if len(summaries) == 0 && format == output.Table {
		console.Info("No workspaces.")
		return
	}
	if err := writeWorkspaceSummaries(os.Stdout, summaries, format); err != nil {
		exitError(err.Error())
	}
}

func doShowOne(name string) {
	format, err := resolveQueryOutput(wsShowOutput, wsShowJSON)
	if err != nil {
		exitError(err.Error())
	}

	ws, err := state.GetWorkspace(name)
	if err != nil {
		exitError(err.Error())
	}
	if ws == nil {
		exitError("Workspace not found: " + name)
	}
	if err := writeWorkspaceShow(os.Stdout, ws, format); err != nil {
		exitError(err.Error())
	}
}
