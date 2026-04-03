package cmd

import (
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [NAME]",
	Short: "Run .grove.toml run hooks across workspace repos",
	Long:  "Runs configured processes for each repo, printing output with [repo] prefixes. Auto-detects workspace from cwd if name omitted.",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := ""
		if len(args) > 0 {
			name = args[0]
		}

		if err := workspace.Run(name); err != nil {
			exitError(err.Error())
		}
	},
}
