package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/operations"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	pruneMinAge string
	pruneYes    bool
	pruneJSON   bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "List or delete workspaces older than --min-age (e.g. 7d, 2w)",
	Long:  "Checks workspaces whose created_at is at least --min-age old (default 7d; units: h, d, w), plus workspaces whose directory no longer exists on disk (stale state records, listed regardless of age). Pass --yes to delete them with the same two-phase cleanup as gw delete; every candidate is attempted and failures are reported per workspace with a non-zero exit.",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runPrune(time.Now(), cmd.OutOrStdout()); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	pruneCmd.Flags().StringVar(&pruneMinAge, "min-age", "7d", "Minimum age: h, d, or w suffix (e.g. 12h, 7d, 2w); bare number means days")
	pruneCmd.Flags().BoolVar(&pruneYes, "yes", false, "Delete matching workspaces")
	pruneCmd.Flags().BoolVarP(&pruneJSON, "json", "j", false, "Output as JSON")
}

type pruneCandidate struct {
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	CreatedAt string `json:"created_at"`
	AgeDays   int    `json:"age_days"`
	// Missing is set when the workspace directory no longer exists on disk.
	// Such records are candidates regardless of age.
	Missing bool `json:"missing"`
	// Error is set after --yes when deleting this workspace failed.
	Error string `json:"error,omitempty"`
}

// parseMinAge parses a --min-age value such as "12h", "7d", or "2w". A bare
// number is treated as days to preserve the original --min-age N behavior.
// Negative values are rejected.
func parseMinAge(value string) (time.Duration, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0, fmt.Errorf("--min-age is required (e.g. 7d, 2w, 12h)")
	}
	unit := byte('d')
	digits := v
	if last := v[len(v)-1]; last < '0' || last > '9' {
		unit = last
		digits = v[:len(v)-1]
	}
	num, err := strconv.Atoi(digits)
	if err != nil {
		return 0, fmt.Errorf("invalid --min-age %q: expected <number>[h|d|w] (e.g. 7d)", value)
	}
	if num < 0 {
		return 0, fmt.Errorf("--min-age must be >= 0")
	}
	switch unit {
	case 'h':
		return time.Duration(num) * time.Hour, nil
	case 'd':
		return time.Duration(num) * 24 * time.Hour, nil
	case 'w':
		return time.Duration(num) * 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid --min-age %q: unit must be h, d, or w", value)
	}
}

// pruneCandidates returns workspaces older than minAge plus workspaces
// whose directory is missing on disk, in state order. exists reports whether
// a path is present; pass nil to use os.Stat.
func pruneCandidates(workspaces []models.Workspace, minAge time.Duration, now time.Time, exists func(string) bool) []pruneCandidate {
	if exists == nil {
		exists = func(path string) bool {
			_, err := os.Stat(path)
			return !os.IsNotExist(err)
		}
	}
	var out []pruneCandidate
	for _, ws := range workspaces {
		missing := ws.Path != "" && !exists(ws.Path)
		c := pruneCandidate{Name: ws.Name, Branch: ws.Branch, CreatedAt: ws.CreatedAt, Missing: missing}
		old := false
		if created, err := workspace.ParseCreatedAt(ws.CreatedAt, now.Location()); err == nil {
			c.AgeDays = int(now.Sub(created) / (24 * time.Hour))
			old = !created.After(now.Add(-minAge))
		}
		if old || missing {
			out = append(out, c)
		}
	}
	return out
}

func writePrunePreview(candidates []pruneCandidate, minAge string, jsonOutput, deleted bool, stdout io.Writer) error {
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
		console.Infof("No workspaces older than %s or with missing directories.", minAge)
		return nil
	}

	table := console.NewTable(stdout, []string{"Name", "Branch", "Created", "Age", "Note"})
	failed := 0
	for _, c := range candidates {
		created := c.CreatedAt
		if len(created) > 10 {
			created = created[:10]
		}
		var notes []string
		if c.Missing {
			notes = append(notes, "directory missing")
		}
		if c.Error != "" {
			failed++
			notes = append(notes, "FAILED: "+c.Error)
		} else if deleted {
			notes = append(notes, "deleted")
		}
		table.AddRow([]string{c.Name, c.Branch, created, fmt.Sprintf("%dd", c.AgeDays), strings.Join(notes, "; ")})
	}
	table.Render()
	if !deleted {
		console.Infof("Pass --yes to delete %d workspace(s).", len(candidates))
	} else if failed > 0 {
		console.Warningf("Deleted %d workspace(s), %d failed.", len(candidates)-failed, failed)
	}
	return nil
}

// deleteCandidates attempts every candidate, recording per-workspace failures
// in Error instead of stopping at the first one. It returns the joined errors.
func deleteCandidates(candidates []pruneCandidate, del func(name string) error) error {
	var errs []error
	for i := range candidates {
		if err := del(candidates[i].Name); err != nil {
			candidates[i].Error = err.Error()
			errs = append(errs, fmt.Errorf("%s: %w", candidates[i].Name, err))
		}
	}
	return errors.Join(errs...)
}

func runPrune(now time.Time, stdout io.Writer) error {
	minAge, err := parseMinAge(pruneMinAge)
	if err != nil {
		return err
	}

	workspaces, err := state.Load()
	if err != nil {
		return err
	}

	candidates := pruneCandidates(workspaces, minAge, now, nil)
	if !pruneYes {
		return writePrunePreview(candidates, pruneMinAge, pruneJSON, false, stdout)
	}

	svc := operations.NewService()
	deleteErr := deleteCandidates(candidates, func(name string) error {
		_, err := svc.Delete(operations.DeleteRequest{
			Name:    name,
			Options: workspace.RemoveOptions{Force: true},
		})
		return err
	})
	if err := writePrunePreview(candidates, pruneMinAge, pruneJSON, true, stdout); err != nil {
		return err
	}
	return deleteErr
}
