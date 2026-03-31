package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/models"
	"github.com/spf13/cobra"
)

var presetCmd = &cobra.Command{
	Use:   "preset",
	Short: "Manage workspace presets",
}

var presetAddRepos string

var presetAddCmd = &cobra.Command{
	Use:   "add [NAME]",
	Short: "Create or update a preset",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			exitError("preset name required")
		}
		if presetAddRepos == "" {
			exitError("--repos is required")
		}

		cfg := config.RequireConfig()
		if cfg.Presets == nil {
			cfg.Presets = make(map[string]models.Preset)
		}

		repos := strings.Split(presetAddRepos, ",")
		for i := range repos {
			repos[i] = strings.TrimSpace(repos[i])
		}

		cfg.Presets[args[0]] = models.Preset{Repos: repos}
		if err := config.Save(cfg); err != nil {
			exitError(err.Error())
		}
		console.Successf("Preset %s saved", args[0])
	},
}

var presetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all presets",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		if len(cfg.Presets) == 0 {
			console.Info("No presets configured")
			return
		}

		table := console.NewTable(os.Stdout, []string{"Preset", "Repos"})
		for name, preset := range cfg.Presets {
			table.AddRow([]string{name, strings.Join(preset.Repos, ", ")})
		}
		table.Render()
	},
}

var presetRemoveCmd = &cobra.Command{
	Use:   "remove [NAME]",
	Short: "Remove a preset",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			exitError("preset name required")
		}

		cfg := config.RequireConfig()
		if _, ok := cfg.Presets[args[0]]; !ok {
			exitError(fmt.Sprintf("Preset %s not found", args[0]))
		}

		delete(cfg.Presets, args[0])
		if err := config.Save(cfg); err != nil {
			exitError(err.Error())
		}
		console.Successf("Preset %s removed", args[0])
	},
}

func init() {
	presetAddCmd.Flags().StringVarP(&presetAddRepos, "repos", "r", "", "Comma-separated repo names")
	presetCmd.AddCommand(presetAddCmd, presetListCmd, presetRemoveCmd)
}
