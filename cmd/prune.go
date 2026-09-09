package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/operations"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	pruneMinAge int
	pruneYes    bool
	pruneJSON   bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "List or delete workspaces older than --min-age days",
	Long:  "Checks workspaces whose created_at is at least --min-age days old (default 7). Pass --yes to delete them with the same two-phase cleanup as gw delete.",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runPrune(time.Now(), cmd.OutOrStdout()); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	pruneCmd.Flags().IntVar(&pruneMinAge, "min-age", 7, "Minimum age in days")
	pruneCmd.Flags().BoolVar(&pruneYes, "yes", false, "Delete matching workspaces")
	pruneCmd.Flags().BoolVarP(&pruneJSON, "json", "j", false, "Output as JSON")
}

type pruneCandidate struct {
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	CreatedAt string `json:"created_at"`
	AgeDays   int    `json:"age_days"`
}

func validatePruneMinAge(days int) error {
	if days < 0 {
		return fmt.Errorf("--min-age must be >= 0")
	}
	return nil
}

func pruneCandidates(workspaces []models.Workspace, minAgeDays int, now time.Time) []pruneCandidate {
	old := workspace.OlderThan(workspaces, time.Duration(minAgeDays)*24*time.Hour, now)
	out := make([]pruneCandidate, 0, len(old))
	for _, ws := range old {
		created, err := workspace.ParseCreatedAt(ws.CreatedAt, now.Location())
		if err != nil {
			continue
		}
		out = append(out, pruneCandidate{
			Name:      ws.Name,
			Branch:    ws.Branch,
			CreatedAt: ws.CreatedAt,
			AgeDays:   int(now.Sub(created) / (24 * time.Hour)),
		})
	}
	return out
}

func writePrunePreview(candidates []pruneCandidate, minAgeDays int, jsonOutput, deleted bool, stdout io.Writer) error {
	if jsonOutput {
		if candidates == nil {
			candidates = []pruneCandidate{}
		}
		data, err := json.MarshalIndent(candidates, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(data))
		return nil
	}

	if len(candidates) == 0 {
		console.Infof("No workspaces older than %d days.", minAgeDays)
		return nil
	}

	table := console.NewTable(stdout, []string{"Name", "Branch", "Created", "Age"})
	for _, c := range candidates {
		created := c.CreatedAt
		if len(created) > 10 {
			created = created[:10]
		}
		table.AddRow([]string{c.Name, c.Branch, created, fmt.Sprintf("%dd", c.AgeDays)})
	}
	table.Render()
	if !deleted {
		console.Infof("Pass --yes to delete %d workspace(s) older than %d days.", len(candidates), minAgeDays)
	}
	return nil
}

func runPrune(now time.Time, stdout io.Writer) error {
	if err := validatePruneMinAge(pruneMinAge); err != nil {
		return err
	}

	workspaces, err := state.Load()
	if err != nil {
		return err
	}

	candidates := pruneCandidates(workspaces, pruneMinAge, now)
	if !pruneYes {
		return writePrunePreview(candidates, pruneMinAge, pruneJSON, false, stdout)
	}

	svc := operations.NewService()
	for _, c := range candidates {
		if _, err := svc.Delete(operations.DeleteRequest{
			Name:    c.Name,
			Options: workspace.RemoveOptions{Force: true},
		}); err != nil {
			return err
		}
	}
	return writePrunePreview(candidates, pruneMinAge, pruneJSON, true, stdout)
}
