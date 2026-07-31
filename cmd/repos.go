package cmd

import (
	"os"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/spf13/cobra"
)

var reposJSON bool

// repoEntry is the machine-readable shape emitted by `gw repos --json`.
// It pairs each discovered repo's local path with its remote identity so that
// resolver tooling (e.g. a source-URL plugin) can map an "owner/repo" from a
// PR/MR URL back to a local clone.
type repoEntry struct {
	Name        string `json:"name"`         // folder name
	Path        string `json:"path"`         // absolute local path
	Remote      string `json:"remote"`       // origin URL (may be empty)
	DisplayName string `json:"display_name"` // "owner/repo" from remote, or folder name
}

var reposCmd = &cobra.Command{
	Use:   "repos",
	Short: "List discovered repos with their remotes",
	Long: "Lists git repositories found in the configured repo directories, " +
		"including each repo's origin remote and derived owner/repo name. " +
		"Use --format json for machine-readable output.",
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		if len(cfg.RepoDirs) == 0 {
			// A missing repo dir is a precondition failure, not an empty result:
			// an agent must not read this as "the machine has no repos".
			fail(machine.Errorf(machine.CodeNotInitialized, "no repo directories configured").
				WithFix("Register at least one directory containing git repos").
				WithActions(machine.NextAction("Add a repo directory", "gw add-dir <path>")))
		}

		infos := discover.DiscoverReposWithCache(cfg.RepoDirs)
		entries := make([]repoEntry, len(infos))
		for i, r := range infos {
			entries[i] = repoEntry{
				Name:        r.Name,
				Path:        r.Path,
				Remote:      r.Remote,
				DisplayName: r.DisplayName,
			}
		}

		if machine.Enabled() {
			machine.Emit(map[string]any{"repos": entries, "count": len(entries)})
			return
		}

		if reposJSON {
			emitLegacyJSON(entries)
			return
		}

		if len(entries) == 0 {
			console.Info("No repos found.")
			return
		}

		table := console.NewTable(os.Stdout, []string{"Name", "Owner/Repo", "Path"})
		for _, e := range entries {
			table.AddRow([]string{e.Name, e.DisplayName, shortenPath(e.Path)})
		}
		table.Render()
	},
}

func init() {
	reposCmd.Flags().BoolVarP(&reposJSON, "json", "j", false, legacyJSONUsage)
}
