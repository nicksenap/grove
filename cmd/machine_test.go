package cmd

import (
	"errors"
	"testing"

	"github.com/nicksenap/grove/internal/machine"
	"github.com/spf13/cobra"
)

func TestClassifyCommandErrMapsCobraFailuresToUsage(t *testing.T) {
	tests := []string{
		`unknown command "frobnicate" for "gw"`,
		"unknown flag: --frobnicate",
		"unknown shorthand flag: 'z' in -z",
		`invalid argument "maybe" for "-f, --force" flag`,
		"accepts 1 arg(s), received 3",
		"flag needs an argument: --branch",
	}
	for _, msg := range tests {
		if got := machine.CodeFor(classifyCommandErr(errors.New(msg))); got != machine.CodeUsage {
			t.Errorf("%q → %s, want %s", msg, got, machine.CodeUsage)
		}
	}
}

// A real operational failure must not be relabelled as the caller's mistake.
func TestClassifyCommandErrLeavesOtherErrorsAlone(t *testing.T) {
	err := errors.New("could not write state.json: disk full")
	if got := machine.CodeFor(classifyCommandErr(err)); got != machine.CodeInternal {
		t.Errorf("code = %s, want %s", got, machine.CodeInternal)
	}
}

// An already-classified error keeps its code — classification happens closest to
// the cause, and the CLI boundary must not overwrite it.
func TestClassifyCommandErrPreservesClassification(t *testing.T) {
	err := machine.Errorf(machine.CodeWorktreeExists, "api already has a worktree")
	if got := machine.CodeFor(classifyCommandErr(err)); got != machine.CodeWorktreeExists {
		t.Errorf("code = %s, want %s", got, machine.CodeWorktreeExists)
	}
}

// Machine mode must be reachable on every command, since an agent has no way to
// know which subcommands opted in.
func TestFormatFlagIsGlobal(t *testing.T) {
	if rootCmd.PersistentFlags().Lookup("format") == nil {
		t.Fatal("--format must be a persistent (global) flag")
	}
	if sh := rootCmd.PersistentFlags().ShorthandLookup("o"); sh == nil || sh.Name != "format" {
		t.Error("-o should be the shorthand for --format")
	}

	// InheritedFlags resolves the persistent flags a subcommand receives from its
	// parents, which is how --format reaches every command.
	var missing []string
	walk(rootCmd, func(c *cobra.Command) {
		if c.InheritedFlags().Lookup("format") == nil && c.Flags().Lookup("format") == nil {
			missing = append(missing, c.CommandPath())
		}
	})
	if len(missing) > 0 {
		t.Errorf("commands without --format: %v", missing)
	}
}

// --json predates the envelope. It stays available so existing scripts and
// plugins keep working, and must never be silently repurposed.
func TestLegacyJSONFlagStillExists(t *testing.T) {
	for _, path := range []string{"list", "status", "doctor", "repos"} {
		c, _, err := rootCmd.Find([]string{path})
		if err != nil {
			t.Fatalf("finding %q: %v", path, err)
		}
		flag := c.Flags().Lookup("json")
		if flag == nil {
			t.Errorf("gw %s lost its --json flag", path)
			continue
		}
		if flag.Usage != legacyJSONUsage {
			t.Errorf("gw %s --json usage = %q, want it marked deprecated", path, flag.Usage)
		}
	}
}

func walk(c *cobra.Command, fn func(*cobra.Command)) {
	for _, sub := range c.Commands() {
		if sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}
		fn(sub)
		walk(sub, fn)
	}
}
