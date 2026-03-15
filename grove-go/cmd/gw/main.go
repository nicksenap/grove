package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/dash"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/git"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/picker"
	"github.com/nicksenap/grove/internal/runner"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/update"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:   "gw",
		Short: "Grove — Git Worktree Workspace Orchestrator",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			verbose, _ := cmd.Flags().GetBool("verbose")
			logging.Setup(verbose)

			// Non-blocking version check
			if newer := update.GetNewerVersion(version); newer != "" {
				console.Warningf("New version available: %s → %s — run: brew upgrade grove", version, newer)
			}
		},
	}
	root.PersistentFlags().Bool("verbose", false, "enable debug logging")

	root.AddCommand(
		versionCmd(),
		initCmd(),
		addDirCmd(),
		removeDirCmd(),
		exploreCmd(),
		createCmd(),
		listCmd(),
		deleteCmd(),
		renameCmd(),
		addRepoCmd(),
		removeRepoCmd(),
		statusCmd(),
		syncCmd(),
		runCmd(),
		goCmd(),
		doctorCmd(),
		shellInitCmd(),
		configCmd(),
		dashCmd(),
		hookCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// --- Version ---

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("gw", version)
		},
	}
}

// --- Init ---

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [dirs...]",
		Short: "Initialize Grove",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.EnsureGroveDir(); err != nil {
				return err
			}

			cfg, _ := config.Load()
			if cfg == nil {
				def := config.DefaultConfig()
				cfg = &def
			}

			for _, dir := range args {
				abs, err := filepath.Abs(dir)
				if err != nil {
					return err
				}
				if !contains(cfg.RepoDirs, abs) {
					cfg.RepoDirs = append(cfg.RepoDirs, abs)
				}
			}

			if err := config.Save(cfg); err != nil {
				return err
			}

			console.Success("Initialized Grove")
			if len(cfg.RepoDirs) > 0 {
				repos := discover.FindAllRepos(cfg.RepoDirs)
				names := make([]string, 0, len(repos))
				for n := range repos {
					names = append(names, n)
				}
				sort.Strings(names)
				if len(names) > 0 {
					console.Infof("Found %d repos: %s", len(names), strings.Join(names, ", "))
				} else {
					console.Info("No git repos found")
				}
			} else {
				console.Info("No repo dirs configured. Add one with: gw add-dir <path>")
			}
			return nil
		},
	}
}

// --- Add/Remove Dir ---

func addDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add-dir <path>",
		Short: "Add a repo source directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}

			abs, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}

			if _, err := os.Stat(abs); os.IsNotExist(err) {
				return fmt.Errorf("directory does not exist: %s", abs)
			}

			if contains(cfg.RepoDirs, abs) {
				console.Warning("directory already configured")
				return nil
			}

			cfg.RepoDirs = append(cfg.RepoDirs, abs)
			if err := config.Save(cfg); err != nil {
				return err
			}

			repos := discover.FindRepos(abs)
			console.Successf("Added repo dir: %s", abs)
			console.Infof("%d repos found", len(repos))
			return nil
		},
	}
}

func removeDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove-dir [path]",
		Short: "Remove a repo source directory",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}

			var dir string
			if len(args) > 0 {
				dir, err = filepath.Abs(args[0])
				if err != nil {
					return err
				}
			} else {
				dir, err = picker.PickOne("Remove directory:", cfg.RepoDirs)
				if err != nil {
					return err
				}
			}

			var filtered []string
			for _, d := range cfg.RepoDirs {
				if d != dir {
					filtered = append(filtered, d)
				}
			}
			cfg.RepoDirs = filtered

			if err := config.Save(cfg); err != nil {
				return err
			}
			console.Successf("Removed repo dir: %s", dir)
			return nil
		},
	}
}

// --- Explore ---

func exploreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "explore",
		Short: "Deep scan for repos in configured directories",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}

			total := 0
			grouped := discover.ExploreRepos(cfg.RepoDirs, 3)
			for dir, repos := range grouped {
				fmt.Println()
				fmt.Println(console.Bold(dir))
				names := make([]string, 0, len(repos))
				for name := range repos {
					names = append(names, name)
				}
				sort.Strings(names)
				for _, name := range names {
					fmt.Printf("  %s  %s\n", name, console.Dim(repos[name]))
				}
				total += len(repos)
			}
			fmt.Println()
			console.Infof("%d repos found", total)
			return nil
		},
	}
}

// --- Create ---

func createCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "new [name]",
		Short:   "Create a new workspace",
		Aliases: []string{"create"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}

			repoFlag, _ := cmd.Flags().GetStringSlice("repos")
			branchFlag, _ := cmd.Flags().GetString("branch")
			presetFlag, _ := cmd.Flags().GetString("preset")
			allFlag, _ := cmd.Flags().GetBool("all")
			copyClaude, _ := cmd.Flags().GetBool("copy-claude-md")
			copyClaude = copyClaude || !cmd.Flags().Changed("copy-claude-md") // default behavior

			allRepos := discover.FindAllRepos(cfg.RepoDirs)
			if len(allRepos) == 0 {
				console.Error("no repos found in configured directories")
				return nil
			}

			// Get branch name first (needed for auto-naming)
			branch := branchFlag
			if branch == "" {
				branch, err = picker.Input("Branch name:")
				if err != nil {
					return err
				}
			}
			branch = strings.TrimSpace(branch)
			if branch == "" {
				return fmt.Errorf("branch name required")
			}

			// Select repos
			var selectedNames []string
			if allFlag {
				for name := range allRepos {
					selectedNames = append(selectedNames, name)
				}
				sort.Strings(selectedNames)
			} else if presetFlag != "" {
				repos, ok := cfg.Presets[presetFlag]
				if !ok {
					return fmt.Errorf("preset %q not found", presetFlag)
				}
				selectedNames = repos
			} else if len(repoFlag) > 0 {
				selectedNames = repoFlag
			} else {
				// Interactive picker with presets
				repoNames := make([]string, 0, len(allRepos))
				for name := range allRepos {
					repoNames = append(repoNames, name)
				}

				if len(cfg.Presets) > 0 {
					// Show presets first, then "Pick manually…"
					presetChoices := make([]string, 0, len(cfg.Presets)+1)
					for name, repos := range cfg.Presets {
						presetChoices = append(presetChoices, fmt.Sprintf("%s  (%s)", name, strings.Join(repos, ", ")))
					}
					sort.Strings(presetChoices)
					presetChoices = append(presetChoices, "Pick manually…")

					choice, err := picker.PickOne("Select repos:", presetChoices)
					if err != nil {
						return err
					}
					if choice == "Pick manually…" {
						selectedNames, err = picker.PickMany("Select repos:", repoNames)
						if err != nil {
							return err
						}
					} else {
						// Extract preset name (before the double space)
						pName := strings.Split(choice, "  ")[0]
						selectedNames = cfg.Presets[pName]
					}
				} else {
					selectedNames, err = picker.PickMany("Select repos:", repoNames)
					if err != nil {
						return err
					}
				}
			}

			if len(selectedNames) == 0 {
				console.Warning("no repos selected")
				return nil
			}

			// Validate repo names
			repoPaths := make(map[string]string)
			for _, name := range selectedNames {
				path, ok := allRepos[name]
				if !ok {
					return fmt.Errorf("repo %q not found", name)
				}
				repoPaths[name] = path
			}

			// Get workspace name
			var name string
			if len(args) > 0 {
				name = args[0]
			} else {
				name = sanitizeName(branch)
			}

			ws, err := workspace.Create(name, repoPaths, branch, cfg)
			if err != nil {
				console.Errorf("failed to create workspace: %v", err)
				return nil
			}

			fmt.Println()
			console.Successf("Workspace %s created at %s", ws.Name, ws.Path)

			// Copy CLAUDE.md if available
			if copyClaude {
				for _, dir := range cfg.RepoDirs {
					src := filepath.Join(dir, "CLAUDE.md")
					if _, err := os.Stat(src); err == nil {
						dst := filepath.Join(ws.Path, "CLAUDE.md")
						if data, err := os.ReadFile(src); err == nil {
							os.WriteFile(dst, data, 0o644)
							console.Success("CLAUDE.md copied")
						}
						break
					}
				}
			}

			// Write GROVE_CD_FILE for shell integration
			if cdFile := os.Getenv("GROVE_CD_FILE"); cdFile != "" {
				os.WriteFile(cdFile, []byte(ws.Path), 0o644)
			}

			// Offer to save preset (only for manual picks, no existing presets)
			if len(selectedNames) > 1 && presetFlag == "" && len(cfg.Presets) == 0 {
				if save, _ := picker.Confirm("Save this selection as a preset?"); save {
					presetName, err := picker.Input("Preset name:")
					if err == nil && presetName != "" {
						cfg.Presets[presetName] = selectedNames
						config.Save(cfg)
						console.Successf("saved preset %q", presetName)
					}
				}
			}

			return nil
		},
	}
	cmd.Flags().StringSliceP("repos", "r", nil, "repos to include")
	cmd.Flags().StringP("branch", "b", "", "branch name")
	cmd.Flags().StringP("preset", "p", "", "use a preset")
	cmd.Flags().Bool("all", false, "include all repos")
	cmd.Flags().Bool("copy-claude-md", false, "copy CLAUDE.md into workspace")
	return cmd
}

// --- List ---

func listCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List workspaces",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			showStatus, _ := cmd.Flags().GetBool("status")

			if showStatus {
				summaries, err := workspace.AllWorkspacesSummary()
				if err != nil {
					return err
				}
				if len(summaries) == 0 {
					console.Info("No workspaces. Create one with: gw new")
					return nil
				}

				t := console.NewTable("Name", "Branch", "Repos", "Status", "Path")
				for _, s := range summaries {
					status := console.Green(s.Status)
					if s.Status != "ok" {
						status = console.Red(s.Status)
					}
					t.AddRow(s.Name, s.Branch, fmt.Sprint(s.Repos), status, s.Path)
				}
				t.Print()
				return nil
			}

			workspaces, err := state.LoadWorkspaces()
			if err != nil {
				return err
			}
			if len(workspaces) == 0 {
				console.Info("No workspaces. Create one with: gw new")
				return nil
			}

			t := console.NewTable("Name", "Branch", "Repos", "Path", "Created")
			for _, ws := range workspaces {
				repoNames := make([]string, len(ws.Repos))
				for i, r := range ws.Repos {
					repoNames[i] = r.RepoName
				}
				created := ""
				if len(ws.CreatedAt) >= 10 {
					created = ws.CreatedAt[:10]
				}
				t.AddRow(ws.Name, ws.Branch, strings.Join(repoNames, ", "), ws.Path, created)
			}
			t.Print()
			return nil
		},
	}
	cmd.Flags().BoolP("status", "s", false, "show workspace status")
	return cmd
}

// --- Delete ---

func deleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "delete [name...]",
		Short:   "Delete workspace(s)",
		Aliases: []string{"rm"},
		RunE: func(cmd *cobra.Command, args []string) error {
			force, _ := cmd.Flags().GetBool("force")

			var names []string
			if len(args) > 0 {
				names = args
			} else {
				workspaces, err := state.LoadWorkspaces()
				if err != nil {
					return err
				}
				if len(workspaces) == 0 {
					console.Info("no workspaces to delete")
					return nil
				}
				wsNames := make([]string, len(workspaces))
				for i, ws := range workspaces {
					wsNames[i] = ws.Name
				}
				names, err = picker.PickMany("Delete workspace(s):", wsNames)
				if err != nil {
					return err
				}
			}

			if len(names) == 0 {
				return nil
			}

			// Validate all names first
			for _, name := range names {
				ws, err := state.GetWorkspace(name)
				if err != nil {
					return err
				}
				if ws == nil {
					return fmt.Errorf("workspace %q not found", name)
				}
			}

			if !force {
				msg := fmt.Sprintf("Delete %d workspace(s) (%s) and all their worktrees?",
					len(names), strings.Join(names, ", "))
				confirmed, err := picker.Confirm(msg)
				if err != nil || !confirmed {
					return nil
				}
			}

			for _, name := range names {
				ok, err := workspace.Delete(name)
				if err != nil {
					console.Errorf("%s: %v", name, err)
					continue
				}
				if ok {
					console.Successf("Workspace %s deleted", name)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolP("force", "f", false, "skip confirmation")
	return cmd
}

// --- Rename ---

func renameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename [name]",
		Short: "Rename a workspace",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}

			var oldName string
			if len(args) > 0 {
				oldName = args[0]
			} else {
				ws, err := resolveWorkspace(nil)
				if err != nil {
					return err
				}
				oldName = ws.Name
			}

			newName, _ := cmd.Flags().GetString("to")
			if newName == "" {
				return fmt.Errorf("--to flag required")
			}

			if err := workspace.Rename(oldName, newName, cfg); err != nil {
				return err
			}
			console.Successf("Renamed %s → %s", oldName, newName)
			return nil
		},
	}
	cmd.Flags().String("to", "", "new name")
	cmd.MarkFlagRequired("to")
	return cmd
}

// --- Add/Remove Repo ---

func addRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add-repo [workspace]",
		Short:   "Add repos to a workspace",
		Aliases: []string{"add"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}

			ws, err := resolveWorkspace(args)
			if err != nil {
				return err
			}

			repoFlag, _ := cmd.Flags().GetStringSlice("repos")
			allRepos := discover.FindAllRepos(cfg.RepoDirs)

			var selectedNames []string
			if len(repoFlag) > 0 {
				selectedNames = repoFlag
			} else {
				existing := make(map[string]bool)
				for _, r := range ws.Repos {
					existing[r.RepoName] = true
				}
				var available []string
				for name := range allRepos {
					if !existing[name] {
						available = append(available, name)
					}
				}
				if len(available) == 0 {
					console.Info("all available repos are already in the workspace")
					return nil
				}
				selectedNames, err = picker.PickMany("Add repos:", available)
				if err != nil {
					return err
				}
			}

			repoPaths := make(map[string]string)
			for _, name := range selectedNames {
				path, ok := allRepos[name]
				if !ok {
					return fmt.Errorf("repo %q not found", name)
				}
				repoPaths[name] = path
			}

			added, err := workspace.AddRepo(ws, repoPaths, cfg)
			if err != nil {
				return err
			}
			console.Successf("Added %d repo(s) to %s", len(added), ws.Name)
			return nil
		},
	}
	cmd.Flags().StringSliceP("repos", "r", nil, "repos to add")
	return cmd
}

func removeRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove-repo [workspace]",
		Short:   "Remove repos from a workspace",
		Aliases: []string{"remove"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, _ := config.Load()

			ws, err := resolveWorkspace(args)
			if err != nil {
				return err
			}

			repoFlag, _ := cmd.Flags().GetStringSlice("repos")
			force, _ := cmd.Flags().GetBool("force")

			var selectedNames []string
			if len(repoFlag) > 0 {
				selectedNames = repoFlag
			} else {
				names := make([]string, len(ws.Repos))
				for i, r := range ws.Repos {
					names[i] = r.RepoName
				}
				selectedNames, err = picker.PickMany("Remove repos:", names)
				if err != nil {
					return err
				}
			}

			if !force {
				msg := fmt.Sprintf("Remove %s from workspace %s?",
					strings.Join(selectedNames, ", "), ws.Name)
				confirmed, err := picker.Confirm(msg)
				if err != nil || !confirmed {
					return nil
				}
			}

			if err := workspace.RemoveRepo(ws, selectedNames, force, cfg); err != nil {
				return err
			}
			console.Successf("Removed %d repo(s) from %s", len(selectedNames), ws.Name)
			return nil
		},
	}
	cmd.Flags().StringSliceP("repos", "r", nil, "repos to remove")
	cmd.Flags().BoolP("force", "f", false, "force removal")
	return cmd
}

// --- Status ---

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status [name]",
		Short: "Show workspace status",
		RunE: func(cmd *cobra.Command, args []string) error {
			allFlag, _ := cmd.Flags().GetBool("all")
			verboseFlag, _ := cmd.Flags().GetBool("verbose-status")
			prFlag, _ := cmd.Flags().GetBool("pr")

			if allFlag {
				console.Warning("--all is deprecated, use: gw list -s")
				summaries, err := workspace.AllWorkspacesSummary()
				if err != nil {
					return err
				}
				t := console.NewTable("Name", "Branch", "Repos", "Status")
				for _, s := range summaries {
					t.AddRow(s.Name, s.Branch, fmt.Sprint(s.Repos), s.Status)
				}
				t.Print()
				return nil
			}

			ws, err := resolveWorkspace(args)
			if err != nil {
				return err
			}

			// Header
			fmt.Printf("Workspace: %s  %s\n\n", console.Bold(ws.Name), console.Dim(ws.Path))

			entries := workspace.Status(ws)

			// Build table
			columns := []string{"Repo", "Branch", "↑↓", "Status"}
			if prFlag {
				columns = append(columns, "PR")
			}
			t := console.NewTable(columns...)

			for _, e := range entries {
				// Format status
				status := console.Green("clean")
				if strings.TrimSpace(e.Status) != "" {
					if strings.HasPrefix(e.Status, "error:") {
						status = console.Red(e.Status)
					} else {
						lines := strings.Count(e.Status, "\n") + 1
						status = console.Yellow(fmt.Sprintf("%d changed", lines))
					}
				}

				// Format drift
				drift := formatDrift(e.Ahead, e.Behind)

				row := []string{e.Repo, e.Branch, drift, status}

				// PR column
				if prFlag {
					pr := formatPR(e.Repo, ws)
					row = append(row, pr)
				}

				t.AddRow(row...)
			}
			t.Print()

			// Verbose: show full git status per repo
			if verboseFlag {
				for _, e := range entries {
					if strings.TrimSpace(e.Status) != "" && !strings.HasPrefix(e.Status, "error:") && e.Status != "" {
						fmt.Println()
						fmt.Println(console.Cyan(e.Repo))
						fmt.Println(e.Status)
					}
				}
			}

			return nil
		},
	}
	cmd.Flags().BoolP("all", "a", false, "show all workspaces (deprecated)")
	cmd.Flags().BoolP("verbose-status", "V", false, "show full git status output")
	cmd.Flags().BoolP("pr", "P", false, "show PR status")
	return cmd
}

func formatDrift(ahead, behind int) string {
	if ahead == 0 && behind == 0 {
		return console.Dim("-")
	}
	return fmt.Sprintf("%s %s", console.Green(fmt.Sprintf("%d↑", ahead)), console.Yellow(fmt.Sprintf("%d↓", behind)))
}

func formatPR(repoName string, ws *models.Workspace) string {
	for _, repo := range ws.Repos {
		if repo.RepoName == repoName {
			pr, err := git.PRStatus(repo.WorktreePath)
			if err != nil {
				return console.Dim("—")
			}
			num := pr["number"]
			st := pr["state"]
			switch st {
			case "MERGED":
				return console.Magenta(fmt.Sprintf("#%s (merged)", num))
			case "CLOSED":
				return console.Dim(fmt.Sprintf("#%s (closed)", num))
			default:
				return fmt.Sprintf("#%s", num)
			}
		}
	}
	return console.Dim("—")
}

// --- Sync ---

func syncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync [name]",
		Short: "Sync workspace repos with their base branches",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveWorkspace(args)
			if err != nil {
				return err
			}

			fmt.Println(console.Bold("Syncing: " + ws.Name))
			fmt.Println()

			results := workspace.Sync(ws)
			for _, r := range results {
				switch {
				case strings.HasPrefix(r.Result, "up to date"):
					fmt.Printf("  %s %s  %s\n", console.Green("✓"), r.Repo, console.Dim(r.Result))
				case strings.HasPrefix(r.Result, "rebased"):
					console.Successf("%s  %s", r.Repo, r.Result)
				case strings.HasPrefix(r.Result, "conflict"):
					console.Errorf("%s  conflict — rebase aborted", r.Repo)
				case strings.HasPrefix(r.Result, "skipped"):
					console.Warningf("%s  %s", r.Repo, r.Result)
				default:
					console.Errorf("%s  %s", r.Repo, r.Result)
				}
			}
			return nil
		},
	}
}

// --- Run ---

func runCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [name]",
		Short: "Run dev processes defined in .grove.toml",
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, err := resolveWorkspace(args)
			if err != nil {
				return err
			}

			runnable := workspace.Runnable(ws)
			if len(runnable) == 0 {
				console.Info("No repos have a run hook in .grove.toml")
				return nil
			}

			// Run pre_run hooks
			for _, r := range runnable {
				cmds := git.RepoHookCommands(r.Repo.SourceRepo, "pre_run")
				for _, c := range cmds {
					runShellCmd(c, r.Repo.WorktreePath)
				}
			}

			// Build entries for runner TUI
			var entries []runner.Entry
			for _, r := range runnable {
				entries = append(entries, runner.Entry{
					RepoName: r.Repo.RepoName,
					Command:  strings.Join(r.Cmds, " && "),
					Cwd:      r.Repo.WorktreePath,
				})
			}

			err = runner.Run(entries)

			// Run post_run hooks
			for _, r := range runnable {
				cmds := git.RepoHookCommands(r.Repo.SourceRepo, "post_run")
				for _, c := range cmds {
					runShellCmd(c, r.Repo.WorktreePath)
				}
			}

			return err
		},
	}
}

func runShellCmd(command, cwd string) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = cwd
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}

// --- Go/CD ---

func goCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cd [name]",
		Short:   "Print workspace path (for shell eval)",
		Aliases: []string{"go"},
		RunE: func(cmd *cobra.Command, args []string) error {
			back, _ := cmd.Flags().GetBool("back")
			del, _ := cmd.Flags().GetBool("delete")

			if back {
				// Navigate to source repo directory
				cwd, _ := os.Getwd()
				ws, _ := state.FindWorkspaceByPath(cwd)
				if ws == nil {
					console.Error("not inside a workspace")
					return nil
				}
				path := resolveBackPath(ws)
				if path != "" {
					fmt.Println(path)
				}
				return nil
			}

			// Build choices for interactive mode
			if len(args) == 0 {
				workspaces, err := state.LoadWorkspaces()
				if err != nil {
					return err
				}
				if len(workspaces) == 0 {
					return fmt.Errorf("no workspaces found")
				}

				// Detect current workspace
				cwd, _ := os.Getwd()
				currentWs, _ := state.FindWorkspaceByPath(cwd)

				choices := make([]string, len(workspaces))
				for i, ws := range workspaces {
					choices[i] = ws.Name
					if currentWs != nil && ws.Name == currentWs.Name {
						choices[i] += "  (current)"
					}
				}

				// Add "back to repos dir" if inside a workspace
				if currentWs != nil {
					choices = append(choices, "← back to repos dir")
				}

				choice, err := picker.PickOne("Go to workspace:", choices)
				if err != nil {
					return err
				}

				if choice == "← back to repos dir" {
					cfg, _ := config.Load()
					if cfg == nil {
						return fmt.Errorf("not configured")
					}
					if len(cfg.RepoDirs) == 1 {
						fmt.Println(cfg.RepoDirs[0])
					} else if len(cfg.RepoDirs) > 1 {
						dir, err := picker.PickOne("Go to:", cfg.RepoDirs)
						if err != nil {
							return err
						}
						fmt.Println(dir)
					}
					return nil
				}

				// Strip "(current)" suffix
				name := strings.TrimSuffix(choice, "  (current)")
				ws, err := state.GetWorkspace(name)
				if err != nil || ws == nil {
					return fmt.Errorf("workspace %q not found", name)
				}

				if del {
					// Spawn detached delete
					spawnDetachedDelete(ws.Name)
				}

				fmt.Println(ws.Path)
				return nil
			}

			ws, err := state.GetWorkspace(args[0])
			if err != nil || ws == nil {
				return fmt.Errorf("workspace %q not found", args[0])
			}

			if del {
				spawnDetachedDelete(ws.Name)
			}

			fmt.Println(ws.Path)
			return nil
		},
	}
	cmd.Flags().BoolP("back", "b", false, "go to source repo directory")
	cmd.Flags().BoolP("delete", "d", false, "delete current workspace after leaving")
	return cmd
}

func resolveBackPath(ws *models.Workspace) string {
	if len(ws.Repos) == 0 {
		return ""
	}
	if len(ws.Repos) == 1 {
		return filepath.Dir(ws.Repos[0].SourceRepo)
	}

	// Check if all repos share a parent
	parents := make(map[string]bool)
	for _, r := range ws.Repos {
		parents[filepath.Dir(r.SourceRepo)] = true
	}
	if len(parents) == 1 {
		for p := range parents {
			return p
		}
	}

	// Multiple parents — use picker
	dirs := make([]string, 0, len(parents))
	for p := range parents {
		dirs = append(dirs, p)
	}
	dir, err := picker.PickOne("Go to:", dirs)
	if err != nil {
		return ""
	}
	return dir
}

func spawnDetachedDelete(name string) {
	self, _ := os.Executable()
	cmd := exec.Command(self, "delete", "--force", name)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Start()
}

// --- Doctor ---

func doctorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose and fix workspace issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}

			fix, _ := cmd.Flags().GetBool("fix")
			issues := workspace.Diagnose(cfg)

			if len(issues) == 0 {
				console.Success("All workspaces healthy")
				return nil
			}

			t := console.NewTable("Workspace", "Repo", "Issue", "Suggested Action")
			for _, issue := range issues {
				ws := issue.Workspace
				if ws == "" {
					ws = console.Dim("—")
				}
				repo := issue.RepoName
				if repo == "" {
					repo = console.Dim("—")
				}
				msg := issue.Message
				if issue.Level == "error" {
					msg = console.Red(msg)
				} else {
					msg = console.Yellow(msg)
				}
				action := issue.Fix
				if action == "" {
					action = console.Dim("manual fix required")
				}
				t.AddRow(ws, repo, msg, action)
			}
			t.Print()

			if fix {
				count := workspace.FixIssues(issues)
				if count > 0 {
					console.Successf("Fixed %d issue(s)", count)
				} else {
					console.Info("No auto-fixable issues found")
				}
			} else {
				fixable := 0
				for _, i := range issues {
					if i.FixFunc != nil {
						fixable++
					}
				}
				if fixable > 0 {
					fmt.Println()
					console.Infof("Found %d issue(s). Run gw doctor --fix to auto-fix", fixable)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("fix", false, "auto-fix issues")
	return cmd
}

// --- Shell Init ---

func shellInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shell-init",
		Short: "Print shell function for workspace navigation",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(`# Grove shell integration — add to .zshrc / .bashrc:
# eval "$(gw shell-init)"

gw() {
    if [ "$1" = "cd" ] || [ "$1" = "go" ]; then
        local dir
        dir="$(command gw "$@")"
        if [ -n "$dir" ] && [ -d "$dir" ]; then
            cd "$dir" || return
        else
            echo "$dir"
        fi
    elif [ "$1" = "new" ] || [ "$1" = "create" ]; then
        local _grove_cd_file
        _grove_cd_file="$(mktemp "${TMPDIR:-/tmp}/.grove_cd.XXXXXX")"
        GROVE_CD_FILE="$_grove_cd_file" command gw "$@"
        if [ -f "$_grove_cd_file" ]; then
            local _grove_target
            _grove_target="$(cat "$_grove_cd_file")"
            rm -f "$_grove_cd_file"
            if [ -n "$_grove_target" ] && [ -d "$_grove_target" ]; then
                cd "$_grove_target" || return
            fi
        fi
    else
        command gw "$@"
    fi
}
`)
		},
	}
}

// --- Config ---

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show or manage config",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show current configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}
			fmt.Printf("Config: %s\n", config.ConfigPath())
			fmt.Printf("Workspace dir: %s\n", cfg.WorkspaceDir)
			fmt.Printf("Claude memory sync: %v\n", cfg.ClaudeMemSync)
			fmt.Println("Repo dirs:")
			for _, d := range cfg.RepoDirs {
				fmt.Printf("  %s\n", d)
			}
			if len(cfg.Presets) > 0 {
				fmt.Println("Presets:")
				for name, repos := range cfg.Presets {
					fmt.Printf("  %s: %s\n", name, strings.Join(repos, ", "))
				}
			}
			return nil
		},
	})

	// Preset subcommands
	preset := &cobra.Command{
		Use:   "preset",
		Short: "Manage presets",
	}

	preset.AddCommand(&cobra.Command{
		Use:   "add [name]",
		Short: "Create or update a preset",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}

			var name string
			if len(args) > 0 {
				name = args[0]
			} else {
				name, err = picker.Input("Preset name:")
				if err != nil {
					return err
				}
			}

			if err := config.ValidatePresetName(name); err != nil {
				return err
			}

			repoFlag, _ := cmd.Flags().GetStringSlice("repos")
			allRepos := discover.FindAllRepos(cfg.RepoDirs)

			var selectedNames []string
			if len(repoFlag) > 0 {
				selectedNames = repoFlag
			} else {
				repoNames := make([]string, 0, len(allRepos))
				for n := range allRepos {
					repoNames = append(repoNames, n)
				}
				selectedNames, err = picker.PickMany("Select repos for preset:", repoNames)
				if err != nil {
					return err
				}
			}

			cfg.Presets[name] = selectedNames
			if err := config.Save(cfg); err != nil {
				return err
			}
			console.Successf("Preset %s saved: %s", name, strings.Join(selectedNames, ", "))
			return nil
		},
	})

	preset.AddCommand(&cobra.Command{
		Use:     "list",
		Short:   "List presets",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}
			if len(cfg.Presets) == 0 {
				console.Info("no presets configured")
				return nil
			}
			t := console.NewTable("Preset", "Repos")
			for name, repos := range cfg.Presets {
				t.AddRow(name, strings.Join(repos, ", "))
			}
			t.Print()
			return nil
		},
	})

	preset.AddCommand(&cobra.Command{
		Use:     "remove [name]",
		Short:   "Remove a preset",
		Aliases: []string{"rm"},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Require()
			if err != nil {
				return err
			}
			var name string
			if len(args) > 0 {
				name = args[0]
			} else {
				names := make([]string, 0, len(cfg.Presets))
				for n := range cfg.Presets {
					names = append(names, n)
				}
				name, err = picker.PickOne("Remove preset:", names)
				if err != nil {
					return err
				}
			}
			delete(cfg.Presets, name)
			if err := config.Save(cfg); err != nil {
				return err
			}
			console.Successf("Preset %s removed", name)
			return nil
		},
	})

	cmd.AddCommand(preset)
	return cmd
}

// --- Dashboard ---

func dashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dash",
		Short: "Agent dashboard and hooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			return dash.Dash(version)
		},
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install Claude Code hooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: implement hook installer
			console.Info("Hook installer not yet implemented in Go version")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "uninstall",
		Short: "Remove Claude Code hooks",
		RunE: func(cmd *cobra.Command, args []string) error {
			console.Info("Hook uninstaller not yet implemented in Go version")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "One-line agent summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			console.Info("Dashboard status not yet implemented in Go version")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List active agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			console.Info("Agent list not yet implemented in Go version")
			return nil
		},
	})

	return cmd
}

// --- Hook (internal) ---

func hookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "_hook",
		Short:  "Internal hook handler",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: implement hook handler
			// Reads JSON from stdin, updates ~/.grove/status/<session_id>.json
			return nil
		},
	}
	cmd.Flags().String("event", "", "event type")
	return cmd
}

// --- Helpers ---

func resolveWorkspace(args []string) (*models.Workspace, error) {
	if len(args) > 0 {
		ws, err := state.GetWorkspace(args[0])
		if err != nil {
			return nil, err
		}
		if ws == nil {
			return nil, fmt.Errorf("workspace %q not found", args[0])
		}
		return ws, nil
	}

	// Try to detect from CWD
	cwd, _ := os.Getwd()
	ws, err := state.FindWorkspaceByPath(cwd)
	if err != nil {
		return nil, err
	}
	if ws != nil {
		return ws, nil
	}

	// Interactive picker
	workspaces, err := state.LoadWorkspaces()
	if err != nil {
		return nil, err
	}
	if len(workspaces) == 0 {
		return nil, fmt.Errorf("no workspaces found")
	}
	names := make([]string, len(workspaces))
	for i, ws := range workspaces {
		names[i] = ws.Name
	}
	name, err := picker.PickOne("Select workspace:", names)
	if err != nil {
		return nil, err
	}
	return state.GetWorkspace(name)
}

var sanitizeRegexp = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizeName(branch string) string {
	s := sanitizeRegexp.ReplaceAllString(branch, "-")
	s = strings.Trim(s, "-")
	return s
}

func contains(s []string, v string) bool {
	for _, item := range s {
		if item == v {
			return true
		}
	}
	return false
}
