package cmd

import (
	"os"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	doctorFix  bool
	doctorJSON bool
)

// doctorResult is the machine payload for `gw doctor`. Healthy is explicit so a
// client does not have to infer it from an empty array, and Fixed reports what
// --fix actually changed.
type doctorResult struct {
	Healthy bool                 `json:"healthy"`
	Issues  []models.DoctorIssue `json:"issues"`
	Fixed   int                  `json:"fixed"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose workspace health issues",
	Run: func(cmd *cobra.Command, args []string) {
		issues, fixed, err := workspace.NewService().Doctor(doctorFix)
		if err != nil {
			fail(err)
		}
		if issues == nil {
			issues = []models.DoctorIssue{}
		}

		if machine.Enabled() {
			// Reporting problems is a successful diagnosis, not a failed command:
			// ok stays true and the issues live in the result.
			machine.Emit(doctorResult{
				Healthy: len(issues) == 0,
				Issues:  issues,
				Fixed:   fixed,
			}, doctorNextActions(issues, doctorFix)...)
			return
		}

		if doctorJSON {
			emitLegacyJSON(issues)
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

func doctorNextActions(issues []models.DoctorIssue, fixed bool) []machine.Action {
	if len(issues) == 0 || fixed {
		return nil
	}
	return []machine.Action{
		machine.NextAction("Repair the reported issues", "gw doctor --fix --format json"),
	}
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Auto-fix issues")
	doctorCmd.Flags().BoolVarP(&doctorJSON, "json", "j", false, legacyJSONUsage)
}
