package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	listJSON   bool
	listStatus bool
	wsShowJSON bool
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
	wsListCmd.Flags().BoolVarP(&listJSON, "json", "j", false, legacyJSONUsage)
	wsListCmd.Flags().BoolVarP(&listStatus, "status", "s", false, "Include git status")
	wsShowCmd.Flags().BoolVarP(&wsShowJSON, "json", "j", false, legacyJSONUsage)
	wsCmd.AddCommand(wsListCmd, wsShowCmd, wsDeleteCmd)

	listCmd.Flags().BoolVarP(&listJSON, "json", "j", false, legacyJSONUsage)
	listCmd.Flags().BoolVarP(&listStatus, "status", "s", false, "Include git status")
}

// listResult is the machine payload for `gw list`. Count is included so a client
// can assert on it without walking the array.
type listResult struct {
	Workspaces []models.Workspace `json:"workspaces"`
	Count      int                `json:"count"`
}

// listStatusResult is the machine payload for `gw list --status`.
type listStatusResult struct {
	Workspaces []workspace.WorkspaceSummary `json:"workspaces"`
	Count      int                          `json:"count"`
}

func doListAll() {
	if listStatus {
		listWithStatus()
		return
	}

	workspaces, err := state.Load()
	if err != nil {
		fail(err)
	}

	if machine.Enabled() {
		machine.Emit(listResult{Workspaces: workspaces, Count: len(workspaces)}, listNextActions(workspaces)...)
		return
	}

	if listJSON {
		emitLegacyJSON(workspaces)
		return
	}

	if len(workspaces) == 0 {
		console.Info("No workspaces. Create one with: gw create <name> -r repo1,repo2 -b branch")
		return
	}

	table := console.NewTable(os.Stdout, []string{"Name", "Branch", "Repos", "Created"})
	for _, ws := range workspaces {
		repoCount := fmt.Sprintf("%d", len(ws.Repos))
		created := ws.CreatedAt
		if len(created) > 10 {
			created = created[:10]
		}
		table.AddRow([]string{ws.Name, ws.Branch, repoCount, created})
	}
	table.Render()
}

// listNextActions points at the cheapest useful follow-up: inspecting a real
// workspace, or creating the first one when there are none.
func listNextActions(workspaces []models.Workspace) []machine.Action {
	if len(workspaces) == 0 {
		return []machine.Action{
			machine.NextAction("Create the first workspace",
				"gw create <name> -r <repo1,repo2> -b <branch> --format json"),
		}
	}
	return []machine.Action{
		machine.NextAction("Inspect repo state for a workspace",
			"gw status "+workspaces[0].Name+" --format json"),
	}
}

func listWithStatus() {
	summaries, err := workspace.NewService().AllWorkspacesSummary()
	if err != nil {
		fail(err)
	}

	if machine.Enabled() {
		machine.Emit(listStatusResult{Workspaces: summaries, Count: len(summaries)})
		return
	}

	if listJSON {
		emitLegacyJSON(summaries)
		return
	}

	if len(summaries) == 0 {
		console.Info("No workspaces.")
		return
	}

	table := console.NewTable(os.Stdout, []string{"Name", "Branch", "Repos", "Status", "Path"})
	for _, s := range summaries {
		table.AddRow([]string{s.Name, s.Branch, fmt.Sprintf("%d", s.Repos), s.Status, shortenPath(s.Path)})
	}
	table.Render()
}

func doShowOne(name string) {
	ws, err := state.GetWorkspace(name)
	if err != nil {
		fail(err)
	}
	if ws == nil {
		fail(workspace.ErrWorkspaceNotFound(name))
	}

	if machine.Enabled() {
		machine.Emit(map[string]any{"workspace": ws},
			machine.NextAction("Inspect repo state", "gw status "+ws.Name+" --format json"))
		return
	}

	if wsShowJSON {
		emitLegacyJSON(ws)
		return
	}

	created := ws.CreatedAt
	if len(created) > 19 {
		created = created[:19]
	}

	fmt.Fprintf(os.Stderr, "Name:      %s\n", ws.Name)
	fmt.Fprintf(os.Stderr, "Branch:    %s\n", ws.Branch)
	fmt.Fprintf(os.Stderr, "Path:      %s\n", shortenPath(ws.Path))
	fmt.Fprintf(os.Stderr, "Created:   %s\n", created)
	fmt.Fprintf(os.Stderr, "Repos:     %d\n\n", len(ws.Repos))

	wsPrefix := ws.Path + "/"
	table := console.NewTable(os.Stderr, []string{"Repo", "Branch", "Worktree", "Source"})
	for _, r := range ws.Repos {
		wt, relative := strings.CutPrefix(r.WorktreePath, wsPrefix)
		if !relative {
			wt = shortenPath(r.WorktreePath)
		}
		table.AddRow([]string{r.RepoName, r.Branch, wt, shortenPath(r.SourceRepo)})
	}
	table.Render()
}

// emitLegacyJSON prints the pre-envelope bare JSON shape behind the deprecated
// `--json` flag. Kept byte-compatible for existing scripts and plugins; new
// consumers should use `--format json`.
func emitLegacyJSON(v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fail(machine.Wrap(machine.CodeInternal, err, "could not serialize output: %s", err))
	}
	fmt.Println(string(data))
}
