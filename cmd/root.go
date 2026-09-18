package cmd

import (
	"errors"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/lifecycle"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/plugin"
	"github.com/nicksenap/grove/internal/update"
	"github.com/spf13/cobra"
)

// Version is set by goreleaser via -ldflags at build time. When built without
// ldflags (e.g. `go install ...@latest`), it falls back to the module version
// recorded in the binary's build info.
var Version = "dev"

func init() {
	if Version != "dev" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		Version = strings.TrimPrefix(v, "v")
	}
}

var verbose bool

var rootCmd = &cobra.Command{
	Use:   "gw",
	Short: "Grove — Git Worktree Workspace Orchestrator",
	Long:  "Manages multi-repo worktree-based workspaces",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if cmd.Annotations[offlineCommandAnnotation] == "true" {
			return
		}
		if console.IsTerminal(os.Stderr) {
			if notice := update.NewChecker(config.GroveDir).FormatNotice(Version); notice != "" {
				console.Info(notice)
			}
		}
		logging.Setup(verbose)
		logging.Info("gw %s", cmd.Name())
	},
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVarP(&lifecycle.Disabled, "no-hooks", "n", false, "Skip lifecycle hooks")
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
		pruneCmd,
		goCmd,
		statusCmd,
		addRepoCmd,
		removeRepoCmd,
		reposCmd,
		renameCmd,
		syncCmd,
		resetCmd,
		doctorCmd,
		statsCmd,
		shellInitCmd,
		presetCmd,
		recipeCmd,
		addDirCmd,
		removeDirCmd,
		pluginCmd,
		bugReportCmd,
		unlinkTrashCmd,
	)
}

func Execute() {
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
						console.Errorf("plugin %s: %s", name, execErr)
						os.Exit(1)
					}
					// If Exec used syscall.Exec (Unix), we never reach here.
					os.Exit(0)
				}
			}
		}
		// Print the error ourselves since we silenced cobra
		console.Error(err.Error())
		os.Exit(1)
	}
}

// isUnknownCommandErr checks if the error is cobra's "unknown command" error.
func isUnknownCommandErr(err error) bool {
	return strings.HasPrefix(err.Error(), "unknown command ")
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
	console.Error(msg)
	os.Exit(1)
}

// exitOnPickerErr exits silently on user cancellation, or calls exitError for real errors.
func exitOnPickerErr(err error) {
	if errors.Is(err, picker.ErrCancelled) {
		os.Exit(0)
	}
	exitError(err.Error())
}
