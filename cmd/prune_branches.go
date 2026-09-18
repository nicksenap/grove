package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	pruneBranchesYes   bool
	pruneBranchesJSON  bool
	pruneBranchesFetch bool
	pruneBranchesGone  bool
)

var pruneBranchesCmd = &cobra.Command{
	Use:   "branches",
	Short: "List or delete stale local branches in your main clones",
	Long: `Inspects every discovered repo for local branches that no worktree uses and
that are merged into the repo's base branch (origin/<base_branch> from
.grove.toml, else the default branch). Branches checked out anywhere, referenced
by a Grove workspace, or equal to the base branch are never candidates.

--gone also lists branches whose upstream no longer exists; those may hold
unmerged commits and are force-deleted. Use --fetch to refresh remote refs first.

Lists by default; pass --yes to delete.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runPruneBranches(cmd.OutOrStdout()); err != nil {
			exitError(err.Error())
		}
	},
}

func init() {
	pruneBranchesCmd.Flags().BoolVar(&pruneBranchesYes, "yes", false, "Delete matching branches")
	pruneBranchesCmd.Flags().BoolVarP(&pruneBranchesJSON, "json", "j", false, "Output as JSON")
	pruneBranchesCmd.Flags().BoolVar(&pruneBranchesFetch, "fetch", false, "Run git fetch --prune on each repo first")
	pruneBranchesCmd.Flags().BoolVar(&pruneBranchesGone, "gone", false, "Also include branches whose upstream was deleted; force-deleted. Accurate only with fresh remote refs (--fetch)")
	pruneCmd.AddCommand(pruneBranchesCmd)
}

func runPruneBranches(stdout io.Writer) error {
	cfg := config.RequireConfig()
	workspaces, err := state.Load()
	if err != nil {
		return err
	}
	var repos []workspace.BranchPruneRepo
	for _, r := range discover.UniqueByName(discover.DiscoverReposWithCache(cfg.RepoDirs)) {
		repos = append(repos, workspace.BranchPruneRepo{Name: r.Name, Path: r.Path})
	}

	candidates, errs := workspace.BranchPruneCandidates(repos, workspaces, workspace.BranchPruneOptions{
		Fetch: pruneBranchesFetch, Gone: pruneBranchesGone,
	})
	for _, err := range errs {
		console.Warning(err.Error())
	}

	if pruneBranchesYes {
		var failed []error
		kept := make([]workspace.BranchCandidate, 0, len(candidates))
		for _, c := range candidates {
			if err := workspace.DeleteBranchCandidate(c); err != nil {
				failed = append(failed, fmt.Errorf("%s: %s: %w", c.Repo, c.Branch, err))
				continue
			}
			kept = append(kept, c)
		}
		if err := writeBranchPrunePreview(kept, pruneBranchesJSON, true, stdout); err != nil {
			return err
		}
		return errors.Join(failed...)
	}
	return writeBranchPrunePreview(candidates, pruneBranchesJSON, false, stdout)
}

func writeBranchPrunePreview(candidates []workspace.BranchCandidate, jsonOutput, deleted bool, stdout io.Writer) error {
	if jsonOutput {
		if candidates == nil {
			candidates = []workspace.BranchCandidate{}
		}
		data, err := json.MarshalIndent(candidates, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, string(data))
		return nil
	}
	if len(candidates) == 0 {
		console.Info("No stale branches.")
		return nil
	}
	table := console.NewTable(stdout, []string{"Repo", "Branch", "Reason"})
	for _, c := range candidates {
		reason := c.Reason
		if c.Base != "" {
			reason += " into " + c.Base
		}
		table.AddRow([]string{c.Repo, c.Branch, reason})
	}
	table.Render()
	if deleted {
		console.Successf("Deleted %d branch(es).", len(candidates))
	} else {
		console.Infof("Pass --yes to delete %d branch(es).", len(candidates))
	}
	return nil
}
