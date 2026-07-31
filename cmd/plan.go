package cmd

import (
	"fmt"
	"os"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

// `gw plan` previews a mutation; `gw apply` executes a previewed one. Together
// they give an agent a review step before destructive work, and a guarantee that
// what runs is what was reviewed (see internal/workspace/plan.go).

var (
	planBranch      string
	planRepos       string
	planPreset      string
	planAll         bool
	planTrack       bool
	planSourceURL   string
	planSourceProv  string
	planSourceRef   string
	planSourceTitle string
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Preview a mutation without performing it",
	Long: `Produce a reviewable description of what a command would change.

A plan lists every repository, path, and branch that would be created or
destroyed, and carries a fingerprint of the state it was computed against.
"gw apply" refuses the plan if that state has changed.

Plans are non-interactive by design: pass repos and branch explicitly.`,
	Example: `  gw plan create feat-x -r svc-auth,api-gateway -b feat/x --format json > plan.json
  gw plan delete feat-x --format json
  gw apply plan.json --format json`,
}

var planCreateCmd = &cobra.Command{
	Use:   "create NAME",
	Short: "Preview creating a workspace",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		cfg := config.RequireConfig()
		repos := discover.FindAllRepos(cfg.RepoDirs)
		repoMap := discover.RepoMap(repos)

		repoNames := planRepoNames(cfg, repos)

		opts := workspace.CreateOpts{
			Branch:  planBranch,
			Repos:   repoNames,
			RepoMap: repoMap,
			Cfg:     cfg,
			Source:  planSource(),
		}
		if planTrack {
			opts.BranchMode = workspace.BranchModeTrack
		}

		plan, err := workspace.NewService().PlanCreate(name, opts, Version)
		if err != nil {
			fail(err)
		}
		emitPlan(plan)
	},
}

var planDeleteCmd = &cobra.Command{
	Use:   "delete [NAME]",
	Short: "Preview deleting a workspace",
	Long:  "Auto-detects the workspace from cwd if NAME is omitted.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		ws, err := workspace.ResolveWorkspace(name)
		if err != nil {
			fail(err)
		}

		plan, err := workspace.NewService().PlanDelete(ws.Name, Version)
		if err != nil {
			fail(err)
		}
		emitPlan(plan)
	},
	ValidArgsFunction: completeWorkspaceNames,
}

var applyCmd = &cobra.Command{
	Use:   "apply PLAN",
	Short: "Execute a plan produced by gw plan",
	Long: `Execute a previously reviewed plan.

The plan is re-validated against current state and refused with STATE_CHANGED if
anything relevant has moved, so a reviewed plan cannot execute against a
different world. Accepts a plan file, a saved "--format json" envelope, or "-"
for stdin.`,
	Example: `  gw plan delete feat-x --format json > plan.json
  gw apply plan.json --format json
  gw plan create feat-x -r api -b feat/x --format json | gw apply - --format json`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		plan, err := workspace.LoadPlan(args[0], os.Stdin)
		if err != nil {
			fail(err)
		}

		result, err := workspace.NewService().Apply(plan, Version)
		if err != nil {
			fail(err)
		}

		if machine.Enabled() {
			machine.Emit(result, applyNextActions(plan)...)
			return
		}
		console.Successf("Applied %s plan for %s", plan.Kind, plan.Workspace)
	},
}

// planRepoNames resolves the repo set from flags only. Plans intentionally do not
// fall back to interactive selection: a plan exists to be reviewed and replayed,
// which requires it to be reproducible from its inputs.
func planRepoNames(cfg *models.Config, repos []discover.Repo) []string {
	switch {
	case planPreset != "":
		preset, ok := cfg.Presets[planPreset]
		if !ok {
			fail(machine.Errorf(machine.CodeUsage, "preset %s not found", planPreset).
				WithActions(machine.NextAction("List presets", "gw preset list --format json")))
		}
		return preset.Repos
	case planAll:
		names := make([]string, len(repos))
		for i, r := range repos {
			names[i] = r.Name
		}
		return names
	case planRepos != "":
		return parseRepoList(planRepos)
	default:
		fail(machine.Errorf(machine.CodeUsage, "repos are required when planning").
			WithFix("Pass --repos / -r, --preset / -p, or --all").
			WithActions(machine.NextAction("List discovered repos", "gw repos --format json")))
		return nil
	}
}

func planSource() *models.WorkspaceSource {
	if planSourceURL == "" && planSourceProv == "" {
		return nil
	}
	return &models.WorkspaceSource{
		Provider: planSourceProv,
		URL:      planSourceURL,
		Ref:      planSourceRef,
		Title:    planSourceTitle,
	}
}

func emitPlan(plan *workspace.Plan) {
	if machine.Enabled() {
		machine.Emit(plan, planNextActions(plan)...)
		return
	}
	printPlan(plan)
}

func planNextActions(plan *workspace.Plan) []machine.Action {
	return []machine.Action{
		machine.NextAction("Apply this plan after review",
			fmt.Sprintf("gw plan %s %s --format json > plan.json && gw apply plan.json --format json",
				plan.Kind, plan.Workspace)),
	}
}

func applyNextActions(plan *workspace.Plan) []machine.Action {
	if plan.Kind == workspace.PlanKindDelete {
		return []machine.Action{
			machine.NextAction("List remaining workspaces", "gw list --format json"),
		}
	}
	return []machine.Action{
		machine.NextAction("Inspect repo state", "gw status "+plan.Workspace+" --format json"),
	}
}

func printPlan(plan *workspace.Plan) {
	fmt.Fprintf(os.Stdout, "Plan:       %s %s\n", plan.Kind, plan.Workspace)
	fmt.Fprintf(os.Stdout, "Path:       %s\n", plan.Path)
	if plan.Branch != "" {
		fmt.Fprintf(os.Stdout, "Branch:     %s\n", plan.Branch)
	}
	if plan.Destructive {
		fmt.Fprintf(os.Stdout, "Destructive: yes — %d of %d changes destroy data\n",
			len(plan.DestructiveChanges()), len(plan.Changes))
	}
	fmt.Fprintln(os.Stdout)

	table := console.NewTable(os.Stdout, []string{"", "Action", "Repo", "Target", "Detail"})
	for _, c := range plan.Changes {
		marker := "+"
		if c.Destructive {
			marker = "-"
		}
		target := c.Path
		if target == "" {
			target = c.Branch
		}
		table.AddRow([]string{marker, c.Action, c.Repo, target, c.Detail})
	}
	table.Render()

	for _, w := range plan.Warnings {
		console.Warning(w)
	}
	console.Infof("fingerprint %s", plan.Fingerprint[:min(12, len(plan.Fingerprint))])
}

func init() {
	planCreateCmd.Flags().StringVarP(&planBranch, "branch", "b", "", "Branch name")
	planCreateCmd.Flags().StringVarP(&planRepos, "repos", "r", "", "Comma-separated repo names")
	planCreateCmd.Flags().StringVarP(&planPreset, "preset", "p", "", "Use named preset")
	planCreateCmd.Flags().BoolVar(&planAll, "all", false, "Use all discovered repos")
	planCreateCmd.Flags().BoolVar(&planTrack, "track", false,
		"Check out an existing remote branch (e.g. a PR head) instead of creating a new one")
	planCreateCmd.Flags().StringVar(&planSourceURL, "source-url", "", "Record the source URL this workspace was seeded from")
	planCreateCmd.Flags().StringVar(&planSourceProv, "source-provider", "", "Source provider label (e.g. github, notion, slack)")
	planCreateCmd.Flags().StringVar(&planSourceRef, "source-ref", "", "Source ref (PR number, page id, message ts)")
	planCreateCmd.Flags().StringVar(&planSourceTitle, "source-title", "", "Human-readable source title for display")
	planCreateCmd.RegisterFlagCompletionFunc("repos", completeRepoNames)
	planCreateCmd.RegisterFlagCompletionFunc("preset", completePresetNames)

	planCmd.AddCommand(planCreateCmd, planDeleteCmd)
}
