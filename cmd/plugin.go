package cmd

import (
	"encoding/json"
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
  gw plugin install <name>     Install a known plugin (see: gw plugin search)
  gw plugin install <repo>     Download from a GitHub release
  Manual: place gw-<name> in ~/.grove/plugins/
  Manual: place gw-<name> anywhere on $PATH`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install <name|repo>",
	Short: "Install a plugin from GitHub",
	Long: `Download and install a plugin from a GitHub repository's latest release.
A bare name is resolved through the built-in registry (gw plugin search).

Examples:
  gw plugin install recipe
  gw plugin install nicksenap/gw-recipe
  gw plugin install github.com/nicksenap/gw-recipe`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		repo, err := plugin.ResolveRepo(args[0])
		if err != nil {
			exitError(err.Error())
		}
		if err := plugin.Install(repo); err != nil {
			exitError(err.Error())
		}
	},
}

var pluginSearchJSON bool

var pluginSearchCmd = &cobra.Command{
	Use:   "search [term]",
	Short: "List known plugins from the built-in registry",
	Long: `Show plugins from the registry shipped with this gw build, optionally
filtered by a case-insensitive term matched against name, repo, and description.
Install one with: gw plugin install <name>`,
	Args: cobra.MaximumNArgs(1),
	Annotations: map[string]string{
		offlineCommandAnnotation: "true",
	},
	Run: func(cmd *cobra.Command, args []string) {
		term := ""
		if len(args) == 1 {
			term = args[0]
		}
		entries, err := plugin.Search(term)
		if err != nil {
			exitError(err.Error())
		}
		if pluginSearchJSON {
			if entries == nil {
				entries = []plugin.RegistryEntry{}
			}
			data, err := json.MarshalIndent(entries, "", "  ")
			if err != nil {
				exitError(fmt.Sprintf("failed to marshal JSON: %s", err))
			}
			fmt.Println(string(data))
			return
		}
		if len(entries) == 0 {
			console.Infof("No plugins match %q", term)
			return
		}
		table := console.NewTable(os.Stdout, []string{"Name", "Repo", "Description"})
		for _, entry := range entries {
			table.AddRow([]string{entry.Name, entry.Repo, entry.Description})
		}
		table.Render()
	},
}

var pluginListJSON bool

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
			if !pluginListJSON {
				console.Info("No plugins installed")
				fmt.Fprintf(os.Stderr, "  Install one with: gw plugin install <owner/repo>\n")
			} else {
				fmt.Println("[]")
			}
			return
		}

		if pluginListJSON {
			data, err := json.MarshalIndent(plugins, "", "  ")
			if err != nil {
				exitError(fmt.Sprintf("failed to marshal JSON: %s", err))
			}
			fmt.Println(string(data))
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
	pluginListCmd.Flags().BoolVarP(&pluginListJSON, "json", "j", false, "Output as JSON")
	pluginSearchCmd.Flags().BoolVarP(&pluginSearchJSON, "json", "j", false, "Output as JSON")
	pluginCmd.AddCommand(pluginInstallCmd, pluginSearchCmd, pluginListCmd, pluginRemoveCmd, pluginUpgradeCmd)
}
