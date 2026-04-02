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
)

var listCmd = &cobra.Command{
	Use:   "list [NAME]",
	Short: "List workspaces or show details for one",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 0 {
			listOne(args[0])
			return
		}
		listAll()
	},
}

func init() {
	listCmd.Flags().BoolVarP(&listJSON, "json", "j", false, "Output as JSON")
	listCmd.Flags().BoolVarP(&listStatus, "status", "s", false, "Include git status")
}

func listAll() {
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

	table := console.NewTable(os.Stdout, []string{"Name", "Branch", "Repos", "Path", "Created"})
	for _, ws := range workspaces {
		repos := strings.Join(ws.RepoNames(), ", ")
		created := ws.CreatedAt
		if len(created) > 10 {
			created = created[:10]
		}
		table.AddRow([]string{ws.Name, ws.Branch, repos, ws.Path, created})
	}
	table.Render()
}

func listWithStatus() {
	summaries, err := workspace.AllWorkspacesSummary()
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

	table := console.NewTable(os.Stdout, []string{"Name", "Branch", "Repos", "Status", "Path"})
	for _, s := range summaries {
		table.AddRow([]string{s.Name, s.Branch, fmt.Sprintf("%d", s.Repos), s.Status, s.Path})
	}
	table.Render()
}

func listOne(name string) {
	ws, err := state.GetWorkspace(name)
	if err != nil {
		exitError(err.Error())
	}
	if ws == nil {
		exitError("Workspace not found: " + name)
	}

	if listJSON {
		data, _ := json.MarshalIndent(ws, "", "  ")
		fmt.Println(string(data))
		return
	}

	created := ws.CreatedAt
	if len(created) > 19 {
		created = created[:19]
	}

	fmt.Fprintf(os.Stderr, "Name:      %s\n", ws.Name)
	fmt.Fprintf(os.Stderr, "Branch:    %s\n", ws.Branch)
	fmt.Fprintf(os.Stderr, "Path:      %s\n", ws.Path)
	fmt.Fprintf(os.Stderr, "Created:   %s\n", created)
	fmt.Fprintf(os.Stderr, "Repos:     %d\n\n", len(ws.Repos))

	table := console.NewTable(os.Stderr, []string{"Repo", "Branch", "Worktree Path", "Source Repo"})
	for _, r := range ws.Repos {
		table.AddRow([]string{r.RepoName, r.Branch, r.WorktreePath, r.SourceRepo})
	}
	table.Render()
}
