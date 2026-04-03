package cmd

import (
	"fmt"
	"os"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/plugin"
	"github.com/spf13/cobra"
)

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage gw plugins",
	Long: `Install, list, and remove external plugins that extend gw with new commands.

Plugins are executables named gw-<name>. Any unknown command "gw foo" will
look for a "gw-foo" plugin and run it.

Install methods:
  gw plugin install <repo>     Download from a GitHub release
  Manual: place gw-<name> in ~/.grove/plugins/
  Manual: place gw-<name> anywhere on $PATH`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install <repo>",
	Short: "Install a plugin from GitHub",
	Long: `Download and install a plugin from a GitHub repository's latest release.

Examples:
  gw plugin install nicksenap/gw-dash
  gw plugin install github.com/nicksenap/gw-dash`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := plugin.Install(args[0]); err != nil {
			exitError(err.Error())
		}
	},
}

var pluginListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List installed plugins",
	Run: func(cmd *cobra.Command, args []string) {
		plugins, err := plugin.List()
		if err != nil {
			exitError(err.Error())
		}

		if len(plugins) == 0 {
			console.Info("No plugins installed")
			fmt.Fprintf(os.Stderr, "  Install one with: gw plugin install <owner/repo>\n")
			return
		}

		table := console.NewTable(os.Stdout, []string{"Plugin", "Path"})
		for _, p := range plugins {
			table.AddRow([]string{p.Name, p.Path})
		}
		table.Render()
	},
}

var pluginRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Aliases: []string{"rm", "uninstall"},
	Short:   "Remove an installed plugin",
	Args:    cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := plugin.Remove(args[0]); err != nil {
			exitError(err.Error())
		}
		console.Successf("Removed plugin %s", args[0])
	},
}

var pluginUpgradeCmd = &cobra.Command{
	Use:   "upgrade [name]",
	Short: "Upgrade installed plugin(s) to the latest release",
	Long: `Re-fetch the latest release for a plugin. Without arguments, upgrades all
plugins that were installed via "gw plugin install".`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 1 {
			if err := plugin.Upgrade(args[0]); err != nil {
				exitError(err.Error())
			}
			return
		}

		upgraded, err := plugin.UpgradeAll()
		if err != nil {
			exitError(err.Error())
		}
		if len(upgraded) == 0 {
			console.Info("No plugins to upgrade")
		}
	},
}

func init() {
	pluginCmd.AddCommand(pluginInstallCmd, pluginListCmd, pluginRemoveCmd, pluginUpgradeCmd)
}
