package cmd

import (
	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [DIR...]",
	Short: "Initialize Grove with repo directories",
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Init(args)
		if err != nil {
			exitError(err.Error())
		}

		console.Success("Initialized Grove")

		repos := discover.FindAllRepos(cfg.RepoDirs)
		if len(repos) > 0 {
			console.Infof("Found %d repo(s) in configured directories", len(repos))
		}
	},
}
