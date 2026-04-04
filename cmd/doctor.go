package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/models"
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

		// TODO: remove when matured — nudge users to migrate to [hooks] config.
		issues = append(issues, checkMissingHooks()...)

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

// TODO: remove when matured — nudge users to migrate to [hooks] config.
func checkMissingHooks() []models.DoctorIssue {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return nil
	}

	var issues []models.DoctorIssue

	if _, ok := cfg.Hooks["on_close"]; !ok {
		// Only flag if Zellij is present — that's what the old hardcoded behavior supported.
		if os.Getenv("ZELLIJ_SESSION_NAME") != "" {
			issues = append(issues, models.DoctorIssue{
				Workspace:       "—",
				Issue:           "no on_close hook configured (using legacy Zellij fallback)",
				SuggestedAction: "add [hooks] on_close to ~/.grove/config.toml",
			})
		}
	}

	if _, ok := cfg.Hooks["post_create"]; !ok {
		// Only flag if ~/.claude exists — user is a Claude Code user.
		home, _ := os.UserHomeDir()
		if _, err := os.Stat(filepath.Join(home, ".claude")); err == nil {
			issues = append(issues, models.DoctorIssue{
				Workspace:       "—",
				Issue:           "no post_create hook configured (using legacy CLAUDE.md copy)",
				SuggestedAction: `add [hooks] post_create = "cp {path}/../CLAUDE.md {path}/CLAUDE.md 2>/dev/null || true"`,
			})
		}
	}

	return issues
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "Auto-fix issues")
	doctorCmd.Flags().BoolVarP(&doctorJSON, "json", "j", false, "Output as JSON")
}
