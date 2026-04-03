package cmd

import (
	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/spf13/cobra"
)

var exploreCmd = &cobra.Command{
	Use:   "explore",
	Short: "Deep-scan configured directories for git repos",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		if len(cfg.RepoDirs) == 0 {
			console.Error("No repo directories configured. Run: gw add-dir <path>")
			return
		}

		repos := discover.Explore(cfg.RepoDirs)
		discover.PrintExploreResults(repos)
	},
}
