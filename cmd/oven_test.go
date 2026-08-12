package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/oven"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

func setupOvenCommand(t *testing.T, command string) recipeCreateCommandEnv {
	t.Helper()
	env := setupRecipeCreateCommand(t, command)
	oldOvenJSON := ovenJSON
	ovenJSON = true
	t.Cleanup(func() { ovenJSON = oldOvenJSON })
	return env
}

func ovenTestCommand() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	command := &cobra.Command{}
	command.SetContext(context.Background())
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	return command, &stdout, &stderr
}

func TestOvenBakeAndReconcileKeepOneReadySlot(t *testing.T) {
	env := setupOvenCommand(t, "mkdir -p node_modules && touch node_modules/prepared")
	command, stdout, _ := ovenTestCommand()
	if err := runOvenBake(command, createRecipe, false); err != nil {
		t.Fatal(err)
	}
	var baked ovenActionOutput
	if err := json.Unmarshal(stdout.Bytes(), &baked); err != nil {
		t.Fatal(err)
	}
	if baked.Status != string(oven.StatusReady) || baked.SlotID == "" || baked.AlreadyReady {
		t.Fatalf("bake output = %+v", baked)
	}

	stdout.Reset()
	if err := runOvenBake(command, createRecipe, true); err != nil {
		t.Fatal(err)
	}
	var reconciled ovenActionOutput
	if err := json.Unmarshal(stdout.Bytes(), &reconciled); err != nil {
		t.Fatal(err)
	}
	if !reconciled.AlreadyReady || reconciled.SlotID != baked.SlotID {
		t.Fatalf("reconcile output = %+v", reconciled)
	}
	inventory, err := oven.NewStore(env.groveDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Slots) != 1 || inventory.Slots[0].Status != oven.StatusReady {
		t.Fatalf("inventory = %+v", inventory)
	}
}

func TestOvenReconcileReplacesGenerationOnlyAfterSuccessfulBake(t *testing.T) {
	env := setupOvenCommand(t, "true")
	command, stdout, _ := ovenTestCommand()
	if err := runOvenBake(command, createRecipe, true); err != nil {
		t.Fatal(err)
	}
	inventory, _ := oven.NewStore(env.groveDir).Load()
	oldSlot := inventory.Slots[0]

	if err := os.WriteFile(filepath.Join(env.repoPath, "next.txt"), []byte("next"), 0o644); err != nil {
		t.Fatal(err)
	}
	runRecipeGit(t, env.repoPath, "add", "next.txt")
	runRecipeGit(t, env.repoPath, "commit", "-m", "next")
	runRecipeGit(t, env.repoPath, "push", "origin", "HEAD")
	stdout.Reset()
	if err := runOvenBake(command, createRecipe, true); err != nil {
		t.Fatal(err)
	}
	inventory, _ = oven.NewStore(env.groveDir).Load()
	if len(inventory.Slots) != 1 || inventory.Slots[0].Generation == oldSlot.Generation {
		t.Fatalf("generation was not replaced: %+v", inventory)
	}
	newSlot := inventory.Slots[0]

	data, err := os.ReadFile(createRecipe)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "run: \"true\"", "run: \"exit 9\"", 1))
	if err := os.WriteFile(createRecipe, data, 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := runOvenBake(command, createRecipe, true); err == nil {
		t.Fatal("expected replacement bake failure")
	}
	inventory, _ = oven.NewStore(env.groveDir).Load()
	if ready := inventory.FindSlot(newSlot.ID); ready == nil || ready.Status != oven.StatusReady {
		t.Fatalf("previous ready generation was removed: %+v", inventory)
	}
}

func TestRecipeCreateOvenHitDoesNotFetchAndUsesPreparedArtifacts(t *testing.T) {
	env := setupOvenCommand(t, "mkdir -p node_modules && touch node_modules/prepared")
	command, _, _ := ovenTestCommand()
	if err := runOvenBake(command, createRecipe, false); err != nil {
		t.Fatal(err)
	}
	runRecipeGit(t, env.repoPath, "remote", "set-url", "origin", "/dev/null")
	createOven = true
	var stdout, stderr bytes.Buffer
	createCommand := &cobra.Command{}
	createCommand.SetContext(context.Background())
	createCommand.SetOut(&stdout)
	createCommand.SetErr(&stderr)
	if err := runRecipeCreate(createCommand, []string{"oven-hit"}); err != nil {
		t.Fatal(err)
	}
	var output recipeCreateOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Oven != "hit" || !output.Created {
		t.Fatalf("create output = %+v", output)
	}
	if _, err := os.Stat(filepath.Join(env.workspaceDir, "oven-hit", "api", "node_modules", "prepared")); err != nil {
		t.Fatalf("prepared artifact missing after claim: %v", err)
	}
	if strings.Contains(stderr.String(), "fetch failed") {
		t.Fatalf("Oven hit fetched refs: %s", stderr.String())
	}
	if err := workspace.NewService().DeleteWithOptions("oven-hit", workspace.RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
}

func TestRecipeCreateOvenMissFallsBackToColdCreation(t *testing.T) {
	env := setupOvenCommand(t, "touch cold-marker")
	createOven = true
	var stdout, stderr bytes.Buffer
	command := &cobra.Command{}
	command.SetContext(context.Background())
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	if err := runRecipeCreate(command, []string{"oven-miss"}); err != nil {
		t.Fatal(err)
	}
	var output recipeCreateOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Oven != "miss" || !output.Created {
		t.Fatalf("create output = %+v", output)
	}
	if _, err := os.Stat(filepath.Join(env.workspaceDir, "oven-miss", "api", "cold-marker")); err != nil {
		t.Fatalf("cold fallback did not execute Recipe: %v", err)
	}
	if err := workspace.NewService().DeleteWithOptions("oven-miss", workspace.RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
}

func TestOvenStatusExplainsFailureAndCleanRemovesSafeSlots(t *testing.T) {
	env := setupOvenCommand(t, "exit 7")
	command, _, _ := ovenTestCommand()
	if err := runOvenBake(command, createRecipe, false); err == nil {
		t.Fatal("expected bake failure")
	}
	statusCommand, stdout, _ := ovenTestCommand()
	if err := runOvenStatus(statusCommand, nil); err != nil {
		t.Fatal(err)
	}
	var statuses []ovenStatusOutput
	if err := json.Unmarshal(stdout.Bytes(), &statuses); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 1 || statuses[0].Status != string(oven.StatusFailed) || statuses[0].Failure == "" {
		t.Fatalf("status output = %+v", statuses)
	}
	stdout.Reset()
	if err := runOvenClean(statusCommand, nil); err != nil {
		t.Fatal(err)
	}
	inventory, err := oven.NewStore(env.groveDir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Slots) != 0 {
		t.Fatalf("clean inventory = %+v", inventory)
	}
}

func TestCreateOvenFlagRequiresRecipe(t *testing.T) {
	if err := validateRecipeCreateOptions(recipeCreateOptions{Oven: true}); err == nil {
		t.Fatal("expected --oven without --recipe to fail")
	}
}
