package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/oven"
	"github.com/nicksenap/grove/internal/recipe"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

var ovenJSON bool

type ovenActionOutput struct {
	Action       string   `json:"action"`
	Recipe       string   `json:"recipe,omitempty"`
	RecipeKey    string   `json:"recipe_key,omitempty"`
	Generation   string   `json:"generation,omitempty"`
	SlotID       string   `json:"slot_id,omitempty"`
	Status       string   `json:"status,omitempty"`
	AlreadyReady bool     `json:"already_ready,omitempty"`
	Removed      int      `json:"removed,omitempty"`
	Blocked      []string `json:"blocked,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type ovenStatusOutput struct {
	ID         string `json:"id"`
	Recipe     string `json:"recipe,omitempty"`
	RecipePath string `json:"recipe_path,omitempty"`
	RecipeKey  string `json:"recipe_key"`
	Generation string `json:"generation"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
	Failure    string `json:"failure,omitempty"`
	Workspace  string `json:"workspace,omitempty"`
}

var ovenCmd = &cobra.Command{
	Use:   "oven",
	Short: "Manage opt-in local prepared Recipe workspaces",
}

var ovenBakeCmd = &cobra.Command{
	Use:   "bake RECIPE",
	Short: "Bake one ready slot for the current Recipe generation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runOvenBake(cmd, args[0], false)
	},
}

var ovenReconcileCmd = &cobra.Command{
	Use:   "reconcile RECIPE",
	Short: "Refresh a Recipe generation and keep one ready slot",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runOvenBake(cmd, args[0], true)
	},
}

var ovenStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show local Oven slots",
	Args:  cobra.NoArgs,
	RunE:  runOvenStatus,
}

var ovenCleanCmd = &cobra.Command{
	Use:   "clean [RECIPE]",
	Short: "Remove unclaimed ready and failed slots",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runOvenClean,
}

func init() {
	ovenCmd.PersistentFlags().BoolVarP(&ovenJSON, "json", "j", false, "Output JSON")
	ovenCmd.AddCommand(ovenBakeCmd, ovenReconcileCmd, ovenStatusCmd, ovenCleanCmd)
}

func runOvenBake(cmd *cobra.Command, recipeFile string, reconcile bool) error {
	model, recipePath, err := loadOvenRecipe(recipeFile)
	if err != nil {
		return writeOvenAction(cmd, ovenActionOutput{Action: ovenAction(reconcile), Error: err.Error()}, err)
	}
	cfg := config.RequireConfig()
	service := workspace.NewService()
	if err := service.RecoverOven(); err != nil {
		return writeOvenAction(cmd, ovenActionOutput{Action: ovenAction(reconcile), Recipe: model.Name, Error: err.Error()}, err)
	}
	resolved, err := resolveRecipeRepositories(model, cfg)
	if err != nil {
		return writeOvenAction(cmd, ovenActionOutput{Action: ovenAction(reconcile), Recipe: model.Name, Error: err.Error()}, err)
	}
	runner := oven.LocalRunnerIdentity()
	recipeKey, generation, err := oven.Identity(model, resolved.BaseCommits, runner)
	if err != nil {
		return writeOvenAction(cmd, ovenActionOutput{Action: ovenAction(reconcile), Recipe: model.Name, Error: err.Error()}, err)
	}
	inventory, err := service.Oven.Load()
	if err != nil {
		return writeOvenAction(cmd, ovenActionOutput{Action: ovenAction(reconcile), Recipe: model.Name, Error: err.Error()}, err)
	}
	ready := inventory.ReadyGeneration(recipeKey, generation, runner)
	alreadyReady := ready != nil
	if ready == nil {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		ready, err = service.BakeOvenSlot(workspace.OvenBakeOptions{
			RecipeKey: recipeKey, RecipeName: model.Name, RecipePath: recipePath,
			Generation: generation, Runner: runner, Repos: resolved.Names,
			RepoMap: resolved.RepoMap, Commits: resolved.BaseCommits,
		}, func(worktrees map[string]string) error {
			_, executeErr := (recipe.Executor{Output: cmd.ErrOrStderr()}).Execute(ctx, model, worktrees)
			return executeErr
		})
		if err != nil {
			output := ovenActionOutput{Action: ovenAction(reconcile), Recipe: model.Name, RecipeKey: recipeKey, Generation: generation, Error: err.Error()}
			return writeOvenAction(cmd, output, err)
		}
	}

	output := ovenActionOutput{
		Action: ovenAction(reconcile), Recipe: model.Name, RecipeKey: recipeKey,
		Generation: generation, SlotID: ready.ID, Status: string(ready.Status), AlreadyReady: alreadyReady,
	}
	cleaned, cleanErr := service.PruneOvenRecipe(recipePath, generation, runner, ready.ID)
	output.Removed, output.Blocked = cleaned.Removed, cleaned.Blocked
	if cleanErr != nil {
		output.Error = cleanErr.Error()
		return writeOvenAction(cmd, output, cleanErr)
	}
	if !ovenJSON {
		if alreadyReady {
			console.Successf("Oven slot %s is already ready for %s", shortOvenID(ready.ID), model.Name)
		} else {
			console.Successf("Oven slot %s is ready for %s", shortOvenID(ready.ID), model.Name)
		}
	}
	return writeOvenAction(cmd, output, nil)
}

func loadOvenRecipe(path string) (*recipe.Recipe, string, error) {
	data, err := readRecipeFile(path)
	if err != nil {
		return nil, "", err
	}
	parsed := recipe.Parse(data)
	if parsed.Recipe == nil || len(parsed.Errors) > 0 {
		return nil, "", errors.New(strings.TrimSpace(formatRecipeErrors(parsed.Errors)))
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	return parsed.Recipe, filepath.Clean(absolute), nil
}

func runOvenStatus(cmd *cobra.Command, _ []string) error {
	inventory, err := oven.NewStore(config.GroveDir).Load()
	if err != nil {
		return err
	}
	slots := make([]ovenStatusOutput, 0, len(inventory.Slots))
	for _, slot := range inventory.Slots {
		status := ovenStatusOutput{
			ID: slot.ID, Recipe: slot.RecipeName, RecipePath: slot.RecipePath,
			RecipeKey: slot.RecipeKey, Generation: slot.Generation, Status: string(slot.Status),
			CreatedAt: slot.CreatedAt, UpdatedAt: slot.UpdatedAt, Failure: slot.Failure,
		}
		if slot.Claim != nil {
			status.Workspace = slot.Claim.WorkspaceName
		}
		slots = append(slots, status)
	}
	sort.Slice(slots, func(i, j int) bool {
		if slots[i].Recipe == slots[j].Recipe {
			return slots[i].UpdatedAt > slots[j].UpdatedAt
		}
		return slots[i].Recipe < slots[j].Recipe
	})
	if ovenJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(slots)
	}
	table := console.NewTable(cmd.OutOrStdout(), []string{"Slot", "Recipe", "Generation", "Status", "Workspace", "Failure"})
	for _, slot := range slots {
		table.AddRow([]string{
			shortOvenID(slot.ID), terminalSafe(slot.Recipe), shortOvenID(slot.Generation),
			terminalSafe(slot.Status), terminalSafe(slot.Workspace), terminalSafe(slot.Failure),
		})
	}
	table.Render()
	return nil
}

func runOvenClean(cmd *cobra.Command, args []string) error {
	recipePath := ""
	if len(args) == 1 {
		absolute, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}
		if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
			absolute = resolved
		}
		recipePath = filepath.Clean(absolute)
	}
	service := workspace.NewService()
	if err := service.RecoverOven(); err != nil {
		return err
	}
	result, err := service.CleanOven(recipePath)
	output := ovenActionOutput{Action: "clean", Removed: result.Removed, Blocked: result.Blocked}
	if err != nil {
		output.Error = err.Error()
	}
	if !ovenJSON {
		console.Successf("Removed %d Oven slot(s)", result.Removed)
		if len(result.Blocked) > 0 {
			console.Warningf("Blocked slots: %s", strings.Join(result.Blocked, ", "))
		}
	}
	return writeOvenAction(cmd, output, err)
}

func writeOvenAction(cmd *cobra.Command, output ovenActionOutput, cause error) error {
	if ovenJSON {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(output); err != nil {
			return err
		}
	}
	return cause
}

func ovenAction(reconcile bool) string {
	if reconcile {
		return "reconcile"
	}
	return "bake"
}

func shortOvenID(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
