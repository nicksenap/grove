package cmd

import (
	"fmt"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/hook"
	"github.com/spf13/cobra"
)

var claudeHookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Manage Claude Code hooks",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var claudeHookInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Register Grove hooks in Claude Code settings",
	Run: func(cmd *cobra.Command, args []string) {
		gwPath, err := hook.ResolveGW()
		if err != nil {
			exitError(err.Error())
		}

		inst := hook.NewInstaller()

		fmt.Fprintf(cmd.ErrOrStderr(), "This will register Grove hooks in ~/.claude/settings.json\n\n")
		fmt.Fprintf(cmd.ErrOrStderr(), "  Events: %d hook event types\n", len(hook.HookEvents))
		fmt.Fprintf(cmd.ErrOrStderr(), "  Binary: %s\n\n", gwPath)

		// Backup
		backupPath, err := inst.Backup()
		if err != nil {
			exitError("backup failed: " + err.Error())
		}
		if backupPath != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "  Backup: %s\n\n", backupPath)
		}

		if !console.Confirm("Proceed?", true) {
			console.Info("Cancelled")
			return
		}

		count, err := inst.Install(gwPath)
		if err != nil {
			exitError(err.Error())
		}
		console.Successf("Installed %d hook(s) in ~/.claude/settings.json", count)
	},
}

var claudeHookUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove Grove hooks from Claude Code settings",
	Run: func(cmd *cobra.Command, args []string) {
		inst := hook.NewInstaller()

		if !inst.IsInstalled() {
			console.Info("Grove hooks are not installed")
			return
		}

		if !console.Confirm("Remove Grove hooks from ~/.claude/settings.json?", false) {
			console.Info("Cancelled")
			return
		}

		removed, err := inst.Uninstall()
		if err != nil {
			exitError(err.Error())
		}
		console.Successf("Removed %d hook(s)", removed)
	},
}

var claudeHookStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if Grove hooks are installed",
	Run: func(cmd *cobra.Command, args []string) {
		inst := hook.NewInstaller()
		if inst.IsInstalled() {
			console.Success("Grove hooks are installed")
		} else {
			console.Info("Grove hooks are not installed. Run: gw hook install")
		}
	},
}

func init() {
	claudeHookCmd.AddCommand(claudeHookInstallCmd)
	claudeHookCmd.AddCommand(claudeHookUninstallCmd)
	claudeHookCmd.AddCommand(claudeHookStatusCmd)
}
