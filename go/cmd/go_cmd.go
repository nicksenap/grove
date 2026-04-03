package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"os/exec"
	"syscall"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/state"
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
		// --close-tab: close current Zellij tab/pane
		if goCloseTab {
			if goDelete {
				// Delete current workspace first
				cwd, _ := os.Getwd()
				ws, _ := state.FindWorkspaceByPath(cwd)
				if ws != nil {
					deleteAsync(ws.Name)
				}
			}
			// Close Zellij pane
			zellijClose()
			return
		}

		if goBack {
			target := resolveGoBack()
			if goDelete {
				// Delete current workspace asynchronously
				cwd, _ := os.Getwd()
				ws, _ := state.FindWorkspaceByPath(cwd)
				if ws != nil {
					deleteAsync(ws.Name)
				}
			}
			fmt.Print(target)
			return
		}

		var name string
		if len(args) > 0 {
			name = args[0]
		} else {
			name = pickWorkspaceForGo()
		}

		// pickWorkspaceForGo returns a directory path for "← back to repos"
		if filepath.IsAbs(name) {
			fmt.Print(name)
			return
		}

		ws, err := state.GetWorkspace(name)
		if err != nil {
			exitError(err.Error())
		}
		if ws == nil {
			exitError("Workspace not found: " + name)
		}

		if goDelete {
			// Check we're not deleting the workspace we're navigating to
			cwd, _ := os.Getwd()
			currentWs, _ := state.FindWorkspaceByPath(cwd)
			if currentWs != nil && currentWs.Name == ws.Name {
				console.Warning("Cannot delete workspace you're navigating to")
			} else if currentWs != nil {
				deleteAsync(currentWs.Name)
			}
		}

		fmt.Print(ws.Path)
	},
}

func resolveGoBack() string {
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

	if len(ws.Repos) == 0 {
		exitError("Workspace has no repos")
	}

	// Collect unique parent dirs of source repos
	parents := make(map[string]bool)
	var parentList []string
	for _, r := range ws.Repos {
		p := filepath.Dir(r.SourceRepo)
		if !parents[p] {
			parents[p] = true
			parentList = append(parentList, p)
		}
	}

	if len(parentList) == 1 {
		return parentList[0]
	}

	// Multiple parent dirs — let user pick
	picked, err := picker.PickOne("Select repo directory:", parentList)
	if err != nil {
		exitError(err.Error())
	}
	return picked
}

func pickWorkspaceForGo() string {
	workspaces, err := state.Load()
	if err != nil {
		exitError(err.Error())
	}
	if len(workspaces) == 0 {
		exitError("No workspaces. Create one first: gw create ...")
	}

	cwd, _ := os.Getwd()
	currentWs, _ := state.FindWorkspaceByPath(cwd)
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
			return cfg.RepoDirs[0]
		} else if len(cfg.RepoDirs) > 1 {
			dir, err := picker.PickOne("Select repo directory", cfg.RepoDirs)
			if err != nil {
				exitError(err.Error())
			}
			return dir
		}
		exitError("No repo dirs configured. Run: gw add-dir <path>")
	}

	// Strip "(current)" suffix if present
	return strings.Split(picked, "  (current)")[0]
}

// deleteAsync spawns a detached subprocess to delete a workspace.
// The subprocess survives after this process exits.
func deleteAsync(name string) {
	exe, err := os.Executable()
	if err != nil {
		exe = "gw"
	}
	cmd := exec.Command(exe, "delete", "--force", name)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	cmd.Start() // fire and forget
}

func zellijClose() {
	sessionName := os.Getenv("ZELLIJ_SESSION_NAME")
	if sessionName == "" {
		exitError("Not inside a Zellij session")
	}
	cmd := exec.Command("zellij", "action", "close-pane")
	cmd.Run()
}

func init() {
	goCmd.Flags().BoolVarP(&goBack, "back", "b", false, "Go back to source repo")
	goCmd.Flags().BoolVarP(&goDelete, "delete", "d", false, "Delete workspace after navigating away")
	goCmd.Flags().BoolVarP(&goCloseTab, "close-tab", "c", false, "Close current Zellij pane/tab")
	goCmd.ValidArgsFunction = completeWorkspaceNames
}
