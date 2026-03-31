package cmd

import (
	"github.com/nicksenap/grove/internal/stats"
	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show workspace usage statistics",
	Run: func(cmd *cobra.Command, args []string) {
		if err := stats.PrintStats(); err != nil {
			exitError(err.Error())
		}
	},
}
