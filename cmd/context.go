package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context",
	Short: "Show the current Grove context (workspace, repos, git state)",
	Long: `One read-only call that answers "where am I and what can I do?".

Reports the workspace containing the current directory (if any), each repo's live
branch and dirty/ahead/behind state, configured repo dirs and presets, and the
safe next commands. Intended as an agent's first call before deciding anything.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cwd, err := os.Getwd()
		if err != nil {
			fail(machine.Wrap(machine.CodeInternal, err, "cannot determine working directory: %s", err))
		}

		// A missing config is reported as initialized:false, not an error: an
		// agent's first call is exactly how it should learn Grove needs setup.
		cfg, _ := config.Load()

		ctx, err := workspace.NewService().Context(cwd, Version, cfg)
		if err != nil {
			fail(err)
		}

		if machine.Enabled() {
			machine.Emit(ctx, contextNextActions(ctx)...)
			return
		}
		printContext(ctx)
	},
}

// contextNextActions offers only commands that are safe and relevant right now:
// setup when uninitialized, workspace-scoped actions when inside one, discovery
// otherwise.
func contextNextActions(ctx *workspace.Context) []machine.Action {
	if !ctx.Initialized {
		return []machine.Action{
			machine.NextAction("Initialize Grove with a directory containing git repos",
				"gw init <repo-dir>"),
		}
	}

	if ctx.Workspace == nil {
		actions := []machine.Action{
			machine.NextAction("List workspaces", "gw list --format json"),
			machine.NextAction("Create a workspace",
				"gw create <name> -r <repo1,repo2> -b <branch> --format json"),
		}
		if len(ctx.RepoDirs) == 0 {
			return append([]machine.Action{
				machine.NextAction("Register a directory containing git repos", "gw add-dir <path>"),
			}, actions...)
		}
		return actions
	}

	ws := ctx.Workspace
	actions := []machine.Action{
		machine.NextAction("Inspect repo state", "gw status "+ws.Name+" --format json"),
	}
	for _, r := range ws.Repos {
		if r.Behind != "" && r.Behind != "-" && r.Behind != "0" {
			actions = append(actions, machine.NextAction("Rebase repos onto their base branches",
				"gw sync "+ws.Name+" --format json"))
			break
		}
	}
	actions = append(actions, machine.NextAction("Preview deleting this workspace",
		"gw plan delete "+ws.Name+" --format json"))
	return actions
}

func printContext(ctx *workspace.Context) {
	if !ctx.Initialized {
		console.Warning("Grove is not initialized. Run: gw init <repo-dir>")
		return
	}

	home, _ := os.UserHomeDir()
	short := func(p string) string {
		if home != "" && p != "" {
			return strings.Replace(p, home, "~", 1)
		}
		return p
	}

	fmt.Fprintf(os.Stdout, "Grove:      %s\n", ctx.GroveVersion)
	fmt.Fprintf(os.Stdout, "Config:     %s\n", short(ctx.ConfigPath))
	fmt.Fprintf(os.Stdout, "Repo dirs:  %s\n", strings.Join(shortAll(ctx.RepoDirs, short), ", "))
	if len(ctx.Presets) > 0 {
		fmt.Fprintf(os.Stdout, "Presets:    %s\n", strings.Join(ctx.Presets, ", "))
	}
	fmt.Fprintf(os.Stdout, "Workspaces: %d\n", ctx.WorkspaceCount)

	if ctx.Workspace == nil {
		fmt.Fprintf(os.Stdout, "\nNot inside a workspace (%s)\n", short(ctx.Cwd))
		return
	}

	ws := ctx.Workspace
	fmt.Fprintf(os.Stdout, "\nWorkspace:  %s  (%s)\n", ws.Name, short(ws.Path))
	fmt.Fprintf(os.Stdout, "Branch:     %s\n", ws.Branch)
	if ws.Source != nil && ws.Source.URL != "" {
		fmt.Fprintf(os.Stdout, "Source:     %s\n", ws.Source.URL)
	}
	fmt.Fprintln(os.Stdout)

	table := console.NewTable(os.Stdout, []string{"Repo", "Branch", "Base", "↑↓", "State"})
	for _, r := range ws.Repos {
		state := "clean"
		if r.Dirty {
			state = "modified"
		}
		table.AddRow([]string{r.Repo, r.Branch, r.BaseBranch, r.Ahead + "↑ " + r.Behind + "↓", state})
	}
	table.Render()
}

func shortAll(paths []string, short func(string) string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = short(p)
	}
	return out
}
