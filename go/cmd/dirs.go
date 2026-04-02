package cmd

import (
	"path/filepath"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/spf13/cobra"
)

var addDirCmd = &cobra.Command{
	Use:   "add-dir <PATH>",
	Short: "Add a repo source directory",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		absPath, err := filepath.Abs(args[0])
		if err != nil {
			exitError(err.Error())
		}

		// Check if already configured
		for _, d := range cfg.RepoDirs {
			if d == absPath {
				console.Infof("Directory already configured: %s", absPath)
				return
			}
		}

		cfg.RepoDirs = append(cfg.RepoDirs, absPath)
		if err := config.Save(cfg); err != nil {
			exitError(err.Error())
		}

		repos := discover.FindRepos([]string{absPath})
		console.Successf("Added repo dir: %s (%d repos found)", absPath, len(repos))
	},
}

var removeDirCmd = &cobra.Command{
	Use:   "remove-dir [PATH]",
	Short: "Remove a repo source directory",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cfg := config.RequireConfig()
		if len(cfg.RepoDirs) == 0 {
			exitError("No repo directories configured")
		}
		var absPath string
		if len(args) > 0 {
			absPath, _ = filepath.Abs(args[0])
		} else {
			selected, err := picker.PickOne("Select directory to remove:", cfg.RepoDirs)
			if err != nil {
				exitError(err.Error())
			}
			absPath = selected
		}
		found := false
		filtered := make([]string, 0, len(cfg.RepoDirs))
		for _, d := range cfg.RepoDirs {
			if d == absPath {
				found = true
			} else {
				filtered = append(filtered, d)
			}
		}

		if !found {
			exitError("Directory not configured: " + absPath)
		}

		cfg.RepoDirs = filtered
		if err := config.Save(cfg); err != nil {
			exitError(err.Error())
		}
		console.Successf("Removed repo dir: %s", absPath)
	},
}
