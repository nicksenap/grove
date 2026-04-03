package cmd

import (
	"fmt"
	"os"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/update"
	"github.com/spf13/cobra"
)

// Version is set by goreleaser via -ldflags at build time.
var Version = "0.13.0-go"

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "gw",
	Short: "Grove — Git Worktree Workspace Orchestrator",
	Long:  "Manages multi-repo worktree-based workspaces",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logging.Setup(verbose)
	},
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable debug logging")
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("gw {{.Version}}\n")

	// Register all subcommands
	rootCmd.AddCommand(
		initCmd,
		createCmd,
		listCmd,
		deleteCmd,
		goCmd,
		statusCmd,
		addRepoCmd,
		removeRepoCmd,
		renameCmd,
		syncCmd,
		doctorCmd,
		statsCmd,
		shellInitCmd,
		presetCmd,
		addDirCmd,
		removeDirCmd,
		runCmd,
		hookCmd,
		exploreCmd,
		mcpServeCmd,
	)
}

func Execute() {
	// Non-blocking version check
	if notice := update.NewChecker(config.GroveDir).FormatNotice(Version); notice != "" {
		fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m\n", notice)
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// exitError prints error to stderr and exits.
func exitError(msg string) {
	fmt.Fprintf(os.Stderr, "\033[1;31merror:\033[0m %s\n", msg)
	os.Exit(1)
}
