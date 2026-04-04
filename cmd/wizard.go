package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/plugin"
	"github.com/spf13/cobra"
)

var wizardCmd = &cobra.Command{
	Use:   "wizard",
	Short: "Interactive setup for plugins and hooks",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.Load()
		if err != nil || cfg == nil {
			exitError("Grove not initialized. Run: gw init <repo-dir>")
		}

		fmt.Fprintln(os.Stderr, "Grove plugin wizard")
		fmt.Fprintln(os.Stderr, "")

		changed := false

		// --- Claude Code ---
		home, _ := os.UserHomeDir()
		claudeExists := false
		if _, err := os.Stat(filepath.Join(home, ".claude")); err == nil {
			claudeExists = true
		}

		if claudeExists {
			_, claudeErr := plugin.Find("claude")
			claudeInstalled := claudeErr == nil

			if !claudeInstalled {
				if console.Confirm("Claude Code detected. Install gw-claude plugin?", true) {
					if err := plugin.Install("nicksenap/gw-claude"); err != nil {
						console.Warningf("install failed: %s", err)
					} else {
						claudeInstalled = true
					}
				}
			} else {
				console.Infof("gw-claude plugin already installed")
			}

			if claudeInstalled {
				// Offer to configure hooks
				if _, ok := cfg.Hooks["post_create"]; !ok {
					if console.Confirm("Configure Claude memory sync hooks?", true) {
						if cfg.Hooks == nil {
							cfg.Hooks = make(map[string]string)
						}
						cfg.Hooks["post_create"] = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
						cfg.Hooks["pre_delete"] = "gw claude sync harvest {path}"
						changed = true
						console.Success("Added post_create and pre_delete hooks")
					}
				} else {
					console.Infof("Claude hooks already configured")
				}

				// Offer to register Claude Code event hooks
				if console.Confirm("Register Claude Code session tracking hooks?", true) {
					pluginPath, findErr := plugin.Find("claude")
					if findErr != nil {
						console.Warningf("cannot find gw-claude: %s", findErr)
					} else {
						hookCmd := exec.Command(pluginPath, "hook", "install")
						hookCmd.Stdout = os.Stdout
						hookCmd.Stderr = os.Stderr
						if err := hookCmd.Run(); err != nil {
							console.Warningf("hook install failed: %s", err)
						}
					}
				}
			}
		}

		fmt.Fprintln(os.Stderr, "")

		// --- Zellij ---
		zellijSession := os.Getenv("ZELLIJ_SESSION_NAME") != ""

		if zellijSession {
			_, zellijErr := plugin.Find("zellij")
			zellijInstalled := zellijErr == nil

			if !zellijInstalled {
				if console.Confirm("Zellij detected. Install gw-zellij plugin?", true) {
					if err := plugin.Install("nicksenap/gw-zellij"); err != nil {
						console.Warningf("install failed: %s", err)
					} else {
						zellijInstalled = true
					}
				}
			} else {
				console.Infof("gw-zellij plugin already installed")
			}

			if zellijInstalled {
				if _, ok := cfg.Hooks["on_close"]; !ok {
					if console.Confirm("Configure on_close hook for Zellij?", true) {
						if cfg.Hooks == nil {
							cfg.Hooks = make(map[string]string)
						}
						cfg.Hooks["on_close"] = "gw zellij close-pane"
						changed = true
						console.Success("Added on_close hook")
					}
				} else {
					console.Infof("on_close hook already configured")
				}
			}
		}

		// Save config if hooks were added
		if changed {
			if err := config.Save(cfg); err != nil {
				exitError("saving config: " + err.Error())
			}
			fmt.Fprintln(os.Stderr, "")
			console.Success("Config updated")
		}

		fmt.Fprintln(os.Stderr, "")
		console.Success("Done! Run 'gw create' to get started.")
	},
}
