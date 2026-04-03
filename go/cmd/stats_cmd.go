package cmd

import (
	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/stats"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show workspace usage statistics",
	Run: func(cmd *cobra.Command, args []string) {
		tr := stats.NewTracker(config.GroveDir)
		if err := tr.PrintStats(); err != nil {
			exitError(err.Error())
		}
	},
}
