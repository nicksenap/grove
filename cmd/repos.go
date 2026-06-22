package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
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
		"Use --json for machine-readable output.",
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		if len(cfg.RepoDirs) == 0 {
			console.Error("No repo directories configured. Run: gw add-dir <path>")
			return
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

		if reposJSON {
			data, _ := json.MarshalIndent(entries, "", "  ")
			fmt.Println(string(data))
			return
		}

		if len(entries) == 0 {
			console.Info("No repos found.")
			return
		}

		home, _ := os.UserHomeDir()
		table := console.NewTable(os.Stdout, []string{"Name", "Owner/Repo", "Path"})
		for _, e := range entries {
			path := e.Path
			if home != "" {
				path = strings.Replace(path, home, "~", 1)
			}
			table.AddRow([]string{e.Name, e.DisplayName, path})
		}
		table.Render()
	},
}

func init() {
	reposCmd.Flags().BoolVarP(&reposJSON, "json", "j", false, "Output as JSON")
}
