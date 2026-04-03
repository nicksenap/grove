package cmd

import (
	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/state"
	"github.com/spf13/cobra"
)

// completeRepoNames provides shell completion for --repos flag.
func completeRepoNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	repos := discover.FindAllRepos(cfg.RepoDirs)
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completePresetNames provides shell completion for --preset flag.
func completePresetNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	names := make([]string, 0, len(cfg.Presets))
	for name := range cfg.Presets {
		names = append(names, name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeWorkspaceNames provides shell completion for workspace name arguments.
func completeWorkspaceNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	workspaces, err := state.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	names := make([]string, len(workspaces))
	for i, ws := range workspaces {
		names[i] = ws.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
