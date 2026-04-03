package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/console"
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
	wsListCmd.Flags().BoolVarP(&listJSON, "json", "j", false, "Output as JSON")
	wsListCmd.Flags().BoolVarP(&listStatus, "status", "s", false, "Include git status")
	wsShowCmd.Flags().BoolVarP(&wsShowJSON, "json", "j", false, "Output as JSON")
	wsCmd.AddCommand(wsListCmd, wsShowCmd)

	listCmd.Flags().BoolVarP(&listJSON, "json", "j", false, "Output as JSON")
	listCmd.Flags().BoolVarP(&listStatus, "status", "s", false, "Include git status")
}

func doListAll() {
	if listStatus {
		listWithStatus()
		return
	}

	workspaces, err := state.Load()
	if err != nil {
		exitError(err.Error())
	}

	if listJSON {
		data, _ := json.MarshalIndent(workspaces, "", "  ")
		fmt.Println(string(data))
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

func listWithStatus() {
	summaries, err := workspace.NewService().AllWorkspacesSummary()
	if err != nil {
		exitError(err.Error())
	}

	if listJSON {
		data, _ := json.MarshalIndent(summaries, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(summaries) == 0 {
		console.Info("No workspaces.")
		return
	}

	home, _ := os.UserHomeDir()
	table := console.NewTable(os.Stdout, []string{"Name", "Branch", "Repos", "Status", "Path"})
	for _, s := range summaries {
		path := s.Path
		if home != "" {
			path = strings.Replace(path, home, "~", 1)
		}
		table.AddRow([]string{s.Name, s.Branch, fmt.Sprintf("%d", s.Repos), s.Status, path})
	}
	table.Render()
}

func doShowOne(name string) {
	ws, err := state.GetWorkspace(name)
	if err != nil {
		exitError(err.Error())
	}
	if ws == nil {
		exitError("Workspace not found: " + name)
	}

	if wsShowJSON {
		data, _ := json.MarshalIndent(ws, "", "  ")
		fmt.Println(string(data))
		return
	}

	created := ws.CreatedAt
	if len(created) > 19 {
		created = created[:19]
	}

	home, _ := os.UserHomeDir()
	wsPath := ws.Path
	if home != "" {
		wsPath = strings.Replace(wsPath, home, "~", 1)
	}

	fmt.Fprintf(os.Stderr, "Name:      %s\n", ws.Name)
	fmt.Fprintf(os.Stderr, "Branch:    %s\n", ws.Branch)
	fmt.Fprintf(os.Stderr, "Path:      %s\n", wsPath)
	fmt.Fprintf(os.Stderr, "Created:   %s\n", created)
	fmt.Fprintf(os.Stderr, "Repos:     %d\n\n", len(ws.Repos))

	wsPrefix := ws.Path + "/"
	table := console.NewTable(os.Stderr, []string{"Repo", "Branch", "Worktree", "Source"})
	for _, r := range ws.Repos {
		wt := r.WorktreePath
		if after, ok := strings.CutPrefix(wt, wsPrefix); ok {
			wt = after
		} else if home != "" {
			wt = strings.Replace(wt, home, "~", 1)
		}
		src := r.SourceRepo
		if home != "" {
			src = strings.Replace(src, home, "~", 1)
		}
		table.AddRow([]string{r.RepoName, r.Branch, wt, src})
	}
	table.Render()
}
