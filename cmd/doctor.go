package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	doctorFix  bool
	doctorJSON bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose workspace health issues",
	Run: func(cmd *cobra.Command, args []string) {
		issues, fixed, err := workspace.NewService().Doctor(doctorFix)
		if err != nil {
			exitError(err.Error())
		}


		if doctorJSON {
			data, _ := json.MarshalIndent(issues, "", "  ")
			fmt.Println(string(data))
			return
		}

		if len(issues) == 0 {
			console.Success("All workspaces healthy")
			return
		}

		table := console.NewTable(os.Stdout, []string{"Workspace", "Repo", "Issue", "Action"})
		for _, issue := range issues {
			repo := "—"
			if issue.Repo != nil {
				repo = *issue.Repo
			}
			table.AddRow([]string{issue.Workspace, repo, issue.Issue, issue.SuggestedAction})
		}
		table.Render()

		if doctorFix {
			console.Successf("Fixed %d issue(s)", fixed)
		}
	},
}


func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Auto-fix issues")
	doctorCmd.Flags().BoolVarP(&doctorJSON, "json", "j", false, "Output as JSON")
}
