package cmd

import (
	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

// completeRepoNames provides shell completion for --repos flag.
func completeRepoNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	repos := discover.UniqueByName(discover.DiscoverReposWithCache(cfg.RepoDirs))
	names := make([]string, len(repos))
	for i, r := range repos {
		names[i] = r.Name
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeAddRepoNames completes discovered repos that are not already in the workspace.
func completeAddRepoNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg, err := config.Load()
	if err != nil || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	ws := resolveWorkspaceForCompletion(args)
	if ws == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	existing := make(map[string]bool, len(ws.Repos))
	for _, r := range ws.Repos {
		existing[r.RepoName] = true
	}

	repos := discover.UniqueByName(discover.DiscoverReposWithCache(cfg.RepoDirs))
	names := make([]string, 0, len(repos))
	for _, r := range repos {
		if existing[r.Name] {
			continue
		}
		names = append(names, r.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeRemoveRepoNames completes repos currently in the target workspace.
func completeRemoveRepoNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	ws := resolveWorkspaceForCompletion(args)
	if ws == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return ws.RepoNames(), cobra.ShellCompDirectiveNoFileComp
}

func resolveWorkspaceForCompletion(args []string) *models.Workspace {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	ws, err := workspace.ResolveWorkspace(name)
	if err != nil {
		return nil
	}
	return ws
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
