package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

const backToRepos = "← back to repos dir"

var (
	goBack     bool
	goDelete   bool
	goCloseTab bool
)

var goCmd = &cobra.Command{
	Use:   "go [NAME]",
	Short: "Navigate to a workspace",
	Long:  "Prints workspace path to stdout. Auto-detects from cwd for --back.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if goBack {
			handleGoBack()
			return
		}

		if len(args) == 0 {
			handleGoInteractive()
			return
		}

		name := args[0]
		ws, err := state.GetWorkspace(name)
		if err != nil {
			exitError(err.Error())
		}
		if ws == nil {
			exitError("Workspace not found: " + name)
		}

		// Print path to stdout — shell wrapper does the cd
		fmt.Print(ws.Path)
	},
}

func handleGoBack() {
	cwd, err := os.Getwd()
	if err != nil {
		exitError(err.Error())
	}

	ws, err := state.FindWorkspaceByPath(cwd)
	if err != nil {
		exitError(err.Error())
	}
	if ws == nil {
		exitError("Not inside a workspace")
	}

	// Collect unique parent dirs of source repos
	parents := make(map[string]bool)
	for _, r := range ws.Repos {
		parents[filepath.Dir(r.SourceRepo)] = true
	}

	if len(parents) == 1 {
		for p := range parents {
			fmt.Print(p)
			return
		}
	}

	// If workspace only has repos from one location, use it
	// Otherwise use ResolveWorkspace which handles interactive picking
	ws2, err := workspace.ResolveWorkspace("")
	if err != nil {
		exitError(err.Error())
	}
	fmt.Print(filepath.Dir(ws2.Repos[0].SourceRepo))
}

func handleGoInteractive() {
	workspaces, err := state.Load()
	if err != nil {
		exitError(err.Error())
	}
	if len(workspaces) == 0 {
		exitError("No workspaces. Create one first: gw create ...")
	}

	currentWs, _ := state.FindWorkspaceByPath("")
	choices := make([]string, 0, len(workspaces))
	for _, ws := range workspaces {
		label := ws.Name
		if currentWs != nil && ws.Name == currentWs.Name {
			label = fmt.Sprintf("%s  (current)", ws.Name)
		}
		choices = append(choices, label)
	}

	if currentWs != nil {
		choices = append(choices, backToRepos)
	}

	picked, err := picker.PickOne("Select workspace", choices)
	if err != nil {
		exitError(err.Error())
	}

	if picked == backToRepos {
		cfg := config.RequireConfig()
		if len(cfg.RepoDirs) == 1 {
			fmt.Print(cfg.RepoDirs[0])
		} else if len(cfg.RepoDirs) > 1 {
			dir, err := picker.PickOne("Select repo directory", cfg.RepoDirs)
			if err != nil {
				exitError(err.Error())
			}
			fmt.Print(dir)
		} else {
			exitError("No repo dirs configured. Run: gw add-dir <path>")
		}
		return
	}

	// Strip "(current)" suffix if present
	name := strings.Split(picked, "  (current)")[0]
	ws, err := state.GetWorkspace(name)
	if err != nil {
		exitError(err.Error())
	}
	if ws == nil {
		exitError("Workspace not found: " + name)
	}
	fmt.Print(ws.Path)
}

func init() {
	goCmd.Flags().BoolVarP(&goBack, "back", "b", false, "Go back to source repo")
	goCmd.Flags().BoolVarP(&goDelete, "delete", "d", false, "Delete workspace after navigating away")
	goCmd.Flags().BoolVarP(&goCloseTab, "close-tab", "c", false, "Close current Zellij pane/tab")
}
