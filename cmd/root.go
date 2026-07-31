package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/lifecycle"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/plugin"
	"github.com/nicksenap/grove/internal/update"
	"github.com/spf13/cobra"
)

// Version is set by goreleaser via -ldflags at build time.
var Version = "dev"

var (
	verbose      bool
	outputFormat string
)

var rootCmd = &cobra.Command{
	Use:   "gw",
	Short: "Grove — Git Worktree Workspace Orchestrator",
	Long:  "Manages multi-repo worktree-based workspaces",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// --format is validated here rather than at the flag layer so an unknown
		// value is a structured USAGE error like any other bad invocation.
		if err := machine.SetFormat(outputFormat); err != nil {
			return machine.Wrap(machine.CodeUsage, err, "%s", err.Error())
		}
		if machine.Enabled() {
			// Machine mode owns stdout, so nothing decorative may reach it.
			console.NoColor = true
		}
		logging.Setup(verbose)
		logging.Info("gw %s", cmd.Name())
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable debug logging")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "format", "o", string(machine.FormatText),
		`Output format: "text" (human) or "json" (machine-readable envelope)`)
	rootCmd.PersistentFlags().BoolVarP(&lifecycle.Disabled, "no-hooks", "n", false, "Skip lifecycle hooks")
	rootCmd.Version = Version
	rootCmd.SetVersionTemplate("gw {{.Version}}\n")

	// Silence cobra's default error/usage output so we can handle plugin fallback cleanly
	rootCmd.SilenceErrors = true
	rootCmd.SilenceUsage = true

	// Register all subcommands
	rootCmd.AddCommand(
		initCmd,
		contextCmd,
		announceCmd,
		announcementsCmd,
		planCmd,
		applyCmd,
		createCmd,
		listCmd,
		wsCmd,
		deleteCmd,
		goCmd,
		statusCmd,
		addRepoCmd,
		removeRepoCmd,
		reposCmd,
		renameCmd,
		syncCmd,
		doctorCmd,
		statsCmd,
		shellInitCmd,
		presetCmd,
		addDirCmd,
		removeDirCmd,
		runCmd,
		exploreCmd,
		pluginCmd,
		wizardCmd,
		bugReportCmd,
	)
}

func Execute() {
	// Read --format before Cobra parses, so pre-command output can honor machine
	// mode. Cobra still validates the value in PersistentPreRunE.
	machine.DetectEarly(os.Args[1:])

	// Non-blocking version check. Suppressed in machine mode: an agent asked for
	// one envelope, not release news.
	if !machine.Enabled() {
		if notice := update.NewChecker(config.GroveDir).FormatNotice(Version); notice != "" {
			fmt.Fprintf(os.Stderr, "\033[2m%s\033[0m\n", notice)
		}
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
						fail(machine.Wrap(machine.CodeInternal, execErr, "plugin %s: %s", name, execErr))
					}
					// If Exec used syscall.Exec (Unix), we never reach here.
					os.Exit(0)
				}
			}
		}
		fail(classifyCommandErr(err))
	}
}

// classifyCommandErr maps Cobra's own parse failures onto contract codes. Cobra
// reports them as plain errors, and an agent that mistypes a flag should get
// USAGE (exit 2) rather than an opaque internal failure.
func classifyCommandErr(err error) error {
	var classified *machine.Error
	if errors.As(err, &classified) {
		return err
	}
	msg := err.Error()
	for _, marker := range []string{
		"unknown command", "unknown flag", "unknown shorthand flag",
		"invalid argument", "accepts", "requires at least", "flag needs an argument",
	} {
		if strings.Contains(msg, marker) {
			return machine.Wrap(machine.CodeUsage, err, "%s", msg)
		}
	}
	return err
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

// legacyJSONUsage documents the pre-envelope `--json` flag. It still emits the
// old bare shapes so existing scripts and plugins keep working; `--format json`
// is the versioned contract described in docs/agent-cli.md.
const legacyJSONUsage = "Legacy bare JSON output (deprecated: use --format json)"

// fail terminates the command with a single structured failure: one envelope on
// stdout in machine mode, a colored line on stderr otherwise, and the exit code
// matching the error's class.
func fail(err error) {
	code := machine.EmitError(err)
	if !machine.Enabled() {
		e := machine.AsError(err)
		console.Error(e.Message)
		if e.Fix != "" {
			console.Info("fix: " + e.Fix)
		}
	}
	os.Exit(code)
}

// exitError is the unclassified escape hatch, kept for call sites that only have
// a message. Prefer fail() with a coded machine.Error.
func exitError(msg string) {
	fail(machine.Errorf(machine.CodeInternal, "%s", msg))
}

// exitOnPickerErr exits silently on user cancellation, or fails for real errors.
// Cancellation stays exit 0 so shell integration (gw go, wrappers) treats an
// escaped picker as "never mind", not as a failure.
func exitOnPickerErr(err error) {
	if errors.Is(err, picker.ErrCancelled) {
		os.Exit(0)
	}
	fail(err)
}

// requireArgs rejects interactive fallbacks in machine mode. Machine mode
// promises never to block on input, so a missing argument that a human would be
// prompted for is a USAGE error instead.
func requireArgs(what, example string) {
	if !machine.Enabled() {
		return
	}
	fail(machine.Errorf(machine.CodeUsage, "%s is required in --format json (machine mode never prompts)", what).
		WithFix("Pass it explicitly, e.g. " + example))
}
