package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/spf13/cobra"
)

var presetCmd = &cobra.Command{
	Use:   "preset",
	Short: "Manage workspace presets",
}

var (
	presetAddRepos  string
	presetListJSON  bool
	presetShowJSON  bool
)

var presetAddCmd = &cobra.Command{
	Use:   "add [NAME]",
	Short: "Create or update a preset",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()

		// Interactive name prompt if not provided
		name := ""
		if len(args) > 0 {
			name = args[0]
		} else if console.IsTerminal(os.Stdin) {
			name = console.Prompt("Preset name")
		}
		if name == "" {
			exitError("preset name required")
		}

		if cfg.Presets == nil {
			cfg.Presets = make(map[string]models.Preset)
		}

		var repoNames []string
		if presetAddRepos != "" {
			repoNames = strings.Split(presetAddRepos, ",")
			for i := range repoNames {
				repoNames[i] = strings.TrimSpace(repoNames[i])
			}
		} else {
			// Interactive: pick repos
			available := discover.FindAllRepos(cfg.RepoDirs)
			if len(available) == 0 {
				exitError("No repos found. Run: gw add-dir <path>")
			}
			choices := make([]string, len(available))
			for i, r := range available {
				choices[i] = r.Name
			}
			selected, err := picker.PickMany("Select repos for preset:", choices)
			if err != nil {
				exitOnPickerErr(err)
			}
			repoNames = selected
		}

		cfg.Presets[name] = models.Preset{Repos: repoNames}
		if err := config.Save(cfg); err != nil {
			exitError(err.Error())
		}
		console.Successf("Preset %s saved: %s", name, strings.Join(repoNames, ", "))
	},
}

var presetListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all presets",
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		if len(cfg.Presets) == 0 {
			if !presetListJSON {
				console.Info("No presets configured")
			} else {
				fmt.Println("{}")
			}
			return
		}

		if presetListJSON {
			data, _ := json.MarshalIndent(cfg.Presets, "", "  ")
			fmt.Println(string(data))
			return
		}

		table := console.NewTable(os.Stdout, []string{"Preset", "Repos"})
		for name, preset := range cfg.Presets {
			table.AddRow([]string{name, fmt.Sprintf("%d", len(preset.Repos))})
		}
		table.Render()
	},
}

var presetShowCmd = &cobra.Command{
	Use:   "show NAME",
	Short: "Show details for a preset",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		preset, ok := cfg.Presets[args[0]]
		if !ok {
			exitError(fmt.Sprintf("Preset %s not found", args[0]))
		}

		if presetShowJSON {
			out := map[string]interface{}{
				"name":  args[0],
				"repos": preset.Repos,
			}
			data, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(data))
			return
		}

		fmt.Fprintf(os.Stderr, "Preset:  %s\n", args[0])
		fmt.Fprintf(os.Stderr, "Repos:   %d\n\n", len(preset.Repos))

		table := console.NewTable(os.Stderr, []string{"Repo"})
		for _, repo := range preset.Repos {
			table.AddRow([]string{repo})
		}
		table.Render()
	},
}

var presetRemoveCmd = &cobra.Command{
	Use:   "remove [NAME]",
	Short: "Remove a preset",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		if len(cfg.Presets) == 0 {
			exitError("No presets to remove")
		}

		name := ""
		if len(args) > 0 {
			name = args[0]
		} else {
			// Interactive: pick preset to remove
			names := make([]string, 0, len(cfg.Presets))
			for n := range cfg.Presets {
				names = append(names, n)
			}
			selected, err := picker.PickOne("Select preset to remove:", names)
			if err != nil {
				exitOnPickerErr(err)
			}
			name = selected
		}

		if _, ok := cfg.Presets[name]; !ok {
			exitError(fmt.Sprintf("Preset %s not found", name))
		}

		delete(cfg.Presets, name)
		if err := config.Save(cfg); err != nil {
			exitError(err.Error())
		}
		console.Successf("Preset %s removed", name)
	},
}

func init() {
	presetAddCmd.Flags().StringVarP(&presetAddRepos, "repos", "r", "", "Comma-separated repo names")
	presetListCmd.Flags().BoolVarP(&presetListJSON, "json", "j", false, "Output as JSON")
	presetShowCmd.Flags().BoolVarP(&presetShowJSON, "json", "j", false, "Output as JSON")
	presetCmd.AddCommand(presetAddCmd, presetListCmd, presetShowCmd, presetRemoveCmd)
}
