package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/plugin"
	"github.com/nicksenap/grove/internal/update"
	"github.com/spf13/cobra"
)

// Version is set by goreleaser via -ldflags at build time.
var Version = "1.0.0"

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "gw",
	Short: "Grove — Git Worktree Workspace Orchestrator",
	Long:  "Manages multi-repo worktree-based workspaces",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logging.Setup(verbose)
		logging.Info("gw %s", cmd.Name())
	},
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable debug logging")
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("gw {{.Version}}\n")

	// Silence cobra's default error/usage output so we can handle plugin fallback cleanly
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	// Register all subcommands
	rootCmd.AddCommand(
		initCmd,
		createCmd,
		listCmd,
		wsCmd,
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
		pluginCmd,
	)
}

func Execute() {
	// Non-blocking version check
	if notice := update.NewChecker(config.GroveDir).FormatNotice(Version); notice != "" {
		fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m\n", notice)
	}

	if err := rootCmd.Execute(); err != nil {
		// If cobra says "unknown command", try to find a matching plugin
		if isUnknownCommandErr(err) {
			if name := extractUnknownCommand(err); name != "" {
				if pluginPath, findErr := plugin.Find(name); findErr == nil {
					args := pluginArgs(name)
					if execErr := plugin.Exec(pluginPath, args); execErr != nil {
						// On Windows, Exec runs a child process — propagate its exit code
						var exitErr *exec.ExitError
						if errors.As(execErr, &exitErr) {
							os.Exit(exitErr.ExitCode())
						}
						fmt.Fprintf(os.Stderr, "\033[1;31merror:\033[0m plugin %s: %s\n", name, execErr)
						os.Exit(1)
					}
					// If Exec used syscall.Exec (Unix), we never reach here.
					os.Exit(0)
				}
			}
		}
		// Print the error ourselves since we silenced cobra
		fmt.Fprintf(os.Stderr, "\033[1;31merror:\033[0m %s\n", err)
		os.Exit(1)
	}
}

// isUnknownCommandErr checks if the error is cobra's "unknown command" error.
func isUnknownCommandErr(err error) bool {
	return strings.Contains(err.Error(), "unknown command")
}

// extractUnknownCommand pulls the command name from cobra's error message.
// Format: `unknown command "foo" for "gw"`
func extractUnknownCommand(err error) string {
	msg := err.Error()
	start := strings.Index(msg, `"`)
	if start < 0 {
		return ""
	}
	end := strings.Index(msg[start+1:], `"`)
	if end < 0 {
		return ""
	}
	return msg[start+1 : start+1+end]
}

// pluginArgs extracts the args after the plugin name from os.Args.
// Skips os.Args[0] (the binary itself) to avoid false matches.
func pluginArgs(name string) []string {
	for i, arg := range os.Args[1:] {
		if arg == name {
			return os.Args[i+2:] // +2 because we sliced from [1:]
		}
	}
	return nil
}

// exitError prints error to stderr and exits.
func exitError(msg string) {
	fmt.Fprintf(os.Stderr, "\033[1;31merror:\033[0m %s\n", msg)
	os.Exit(1)
}

// exitOnPickerErr exits silently on user cancellation, or calls exitError for real errors.
func exitOnPickerErr(err error) {
	if errors.Is(err, picker.ErrCancelled) {
		os.Exit(0)
	}
	exitError(err.Error())
}
