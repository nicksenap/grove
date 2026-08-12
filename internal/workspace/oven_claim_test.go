package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/oven"
)

func bakeTestOvenSlot(t *testing.T, env *testEnv, slotID string, repoNames ...string) *oven.Slot {
	t.Helper()
	configureTestOven(env)
	options := ovenBakeOptions(env, slotID, repoNames...)
	slot, err := env.svc.BakeOvenSlot(options, func(worktrees map[string]string) error {
		for _, path := range worktrees {
			if err := os.MkdirAll(filepath.Join(path, "node_modules"), 0o755); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return slot
}

func ovenClaimOptions(env *testEnv, name, branch string) OvenClaimOptions {
	return OvenClaimOptions{
		RecipeKey: testOvenRecipeKey, Runner: "test-runner", Name: name, Branch: branch, Config: env.cfg,
		nonce: "cccccccccccccccccccccccccccccccc", now: func() time.Time { return time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC) },
	}
}

func TestClaimOvenSlotCreatesNormalWorkspaceAndCleansBackingOnDelete(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	template := bakeTestOvenSlot(t, env, "slot-claim", "api", "web")

	result, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "cake", "feat/cake"))
	if err != nil {
		t.Fatal(err)
	}
	if result.SlotID == template.ID || result.Warning != nil {
		t.Fatalf("claim result = %+v", result)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	claimSlot := inventory.FindSlot(result.SlotID)
	if claimSlot == nil || claimSlot.TemplateSlotID != template.ID {
		t.Fatalf("materialized claim slot = %+v", claimSlot)
	}
	assertClaimedOvenWorkspace(t, env, *claimSlot)
	if err := env.svc.Delete("cake"); err != nil {
		t.Fatal(err)
	}
	assertDeletedOvenBacking(t, env, *claimSlot)
	inventory, err = env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	if ready := inventory.FindSlot(template.ID); ready == nil || ready.Status != oven.StatusReady {
		t.Fatalf("reusable template after claim delete = %+v", ready)
	}
}

func TestClaimOvenSlotMaterializesMultipleIndependentWorkspacesFromOneBake(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	template := bakeTestOvenSlot(t, env, "slot-reusable", "api")
	prepared := filepath.Join(template.Repositories[0].WorktreePath, "node_modules", "prepared.txt")
	if err := os.WriteFile(prepared, []byte("prepared once"), 0o644); err != nil {
		t.Fatal(err)
	}

	first, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "first", "feat/first"))
	if err != nil {
		t.Fatal(err)
	}
	secondOptions := ovenClaimOptions(env, "second", "feat/second")
	secondOptions.nonce = "dddddddddddddddddddddddddddddddd"
	second, err := env.svc.ClaimOvenSlot(secondOptions)
	if err != nil {
		t.Fatal(err)
	}
	if first.SlotID == second.SlotID || first.SlotID == template.ID || second.SlotID == template.ID {
		t.Fatalf("template/claim IDs are not distinct: template=%s first=%s second=%s", template.ID, first.SlotID, second.SlotID)
	}
	for _, workspaceName := range []string{"first", "second"} {
		workspace, err := env.svc.State.GetWorkspace(workspaceName)
		if err != nil || workspace == nil {
			t.Fatalf("workspace %s = %+v, %v", workspaceName, workspace, err)
		}
		data, err := os.ReadFile(filepath.Join(workspace.Repos[0].WorktreePath, "node_modules", "prepared.txt"))
		if err != nil || string(data) != "prepared once" {
			t.Fatalf("workspace %s prepared data = %q, %v", workspaceName, data, err)
		}
	}
	firstWorkspace, _ := env.svc.State.GetWorkspace("first")
	if err := os.WriteFile(filepath.Join(firstWorkspace.Repos[0].WorktreePath, "node_modules", "prepared.txt"), []byte("first changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	secondWorkspace, _ := env.svc.State.GetWorkspace("second")
	secondData, err := os.ReadFile(filepath.Join(secondWorkspace.Repos[0].WorktreePath, "node_modules", "prepared.txt"))
	if err != nil || string(secondData) != "prepared once" {
		t.Fatalf("second workspace shared mutable prepared data = %q, %v", secondData, err)
	}
	if err := env.svc.DeleteWithOptions("first", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.Status("second", StatusOptions{JSON: true}); err != nil {
		t.Fatalf("deleting first claim damaged second: %v", err)
	}
	thirdOptions := ovenClaimOptions(env, "third", "feat/third")
	thirdOptions.nonce = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if _, err := env.svc.ClaimOvenSlot(thirdOptions); err != nil {
		t.Fatalf("template was not reusable after claim deletion: %v", err)
	}
	templateData, err := os.ReadFile(prepared)
	if err != nil || string(templateData) != "prepared once" {
		t.Fatalf("template was mutated through claim = %q, %v", templateData, err)
	}
}

func TestClaimOvenSlotSerializesConcurrentMaterializationsFromOneTemplate(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	template := bakeTestOvenSlot(t, env, "slot-concurrent-materialization", "api")
	options := []OvenClaimOptions{
		ovenClaimOptions(env, "first", "feat/first"),
		ovenClaimOptions(env, "second", "feat/second"),
	}
	options[1].nonce = "dddddddddddddddddddddddddddddddd"

	var wait sync.WaitGroup
	errs := make([]error, len(options))
	for index := range options {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, errs[index] = env.svc.ClaimOvenSlot(options[index])
		}(index)
	}
	wait.Wait()
	for index, err := range errs {
		if err != nil {
			t.Fatalf("claim %d: %v", index, err)
		}
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	if ready := inventory.FindSlot(template.ID); ready == nil || ready.Status != oven.StatusReady {
		t.Fatalf("concurrent claims consumed template: %+v", ready)
	}
	for _, name := range []string{"first", "second"} {
		if claim := inventory.ClaimForWorkspace(name); claim == nil || claim.TemplateSlotID != template.ID {
			t.Fatalf("concurrent claim %s = %+v", name, claim)
		}
	}
}

func TestClaimOvenSlotCleansFailedMaterializationAndKeepsTemplateReady(t *testing.T) {
	env := setupTestEnv(t)
	source := env.createRepo("api")
	template := bakeTestOvenSlot(t, env, "slot-materialization-failure", "api")
	injected := errors.New("injected prepared clone failure")
	options := ovenClaimOptions(env, "failed-copy", "feat/failed-copy")
	options.clonePrepared = func(_, _ string) error { return injected }

	if _, err := env.svc.ClaimOvenSlot(options); !errors.Is(err, injected) {
		t.Fatalf("claim error = %v", err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	ready := inventory.FindSlot(template.ID)
	if len(inventory.Slots) != 1 || ready == nil || ready.Status != oven.StatusReady {
		t.Fatalf("failed materialization changed template inventory: %+v", inventory)
	}
	if workspace, _ := env.svc.State.GetWorkspace("failed-copy"); workspace != nil {
		t.Fatalf("failed materialization published workspace: %+v", workspace)
	}
	for _, entry := range mustWorktreeList(t, source) {
		if canonicalPath(entry.Path) != canonicalPath(source) &&
			canonicalPath(entry.Path) != canonicalPath(template.Repositories[0].WorktreePath) {
			t.Fatalf("failed materialization worktree remained: %+v", entry)
		}
	}
}

func assertClaimedOvenWorkspace(t *testing.T, env *testEnv, slot oven.Slot) {
	t.Helper()
	workspace, err := env.svc.State.GetWorkspace("cake")
	if err != nil || workspace == nil {
		t.Fatalf("workspace = %+v, %v", workspace, err)
	}
	if target, err := os.Readlink(workspace.Path); err != nil || canonicalPath(target) != canonicalPath(slot.BackingPath) {
		t.Fatalf("workspace alias = %q, %v", target, err)
	}
	for _, repository := range workspace.Repos {
		if branch, err := gitops.CurrentBranch(repository.WorktreePath); err != nil || branch != "feat/cake" {
			t.Fatalf("%s branch = %q, %v", repository.RepoName, branch, err)
		}
		if _, err := os.Stat(filepath.Join(repository.WorktreePath, "node_modules")); err != nil {
			t.Fatalf("%s prepared dependency missing: %v", repository.RepoName, err)
		}
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	claimed := inventory.ClaimForWorkspace("cake")
	if claimed == nil || claimed.Status != oven.StatusClaimed || claimed.Claim.Nonce != "cccccccccccccccccccccccccccccccc" {
		t.Fatalf("claimed inventory = %+v", claimed)
	}
	if err := env.svc.Status("cake", StatusOptions{JSON: true}); err != nil {
		t.Fatalf("status through oven alias: %v", err)
	}
}

func assertDeletedOvenBacking(t *testing.T, env *testEnv, slot oven.Slot) {
	t.Helper()
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	if inventory.FindSlot(slot.ID) != nil {
		t.Fatalf("deleted claim remained in inventory: %+v", inventory)
	}
	if _, err := os.Stat(slot.BackingPath); !os.IsNotExist(err) {
		t.Fatalf("backing path remained after delete: %v", err)
	}
}

func TestClaimOvenSlotPreservesPreexistingExactBranch(t *testing.T) {
	env := setupTestEnv(t)
	source := env.createRepo("api")
	slot := bakeTestOvenSlot(t, env, "slot-existing-branch", "api")
	if err := gitops.CreateBranch(source, "feat/existing", slot.Repositories[0].Commit); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "existing", "feat/existing")); err != nil {
		t.Fatal(err)
	}
	workspace, _ := env.svc.State.GetWorkspace("existing")
	if workspace == nil || !workspace.Repos[0].PreserveBranch {
		t.Fatalf("pre-existing branch ownership not persisted: %+v", workspace)
	}
	if err := env.svc.DeleteWithOptions("existing", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if !gitops.BranchExists(source, "feat/existing") {
		t.Fatal("pre-existing branch was deleted")
	}
}

func TestClaimOvenSlotMissDoesNotMutateWorkspaceState(t *testing.T) {
	env := setupTestEnv(t)
	configureTestOven(env)
	_, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "miss", "feat/miss"))
	if !errors.Is(err, ErrOvenMiss) {
		t.Fatalf("claim error = %v, want Oven miss", err)
	}
	if workspace, _ := env.svc.State.GetWorkspace("miss"); workspace != nil {
		t.Fatalf("miss created workspace: %+v", workspace)
	}
}

func TestClaimOvenSlotNameCollisionDoesNotQuarantineReadySlot(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	slot := bakeTestOvenSlot(t, env, "slot-name-collision", "api")
	existing := models.NewWorkspace("existing", filepath.Join(env.wsDir, "existing"), "feat/existing")
	if err := env.svc.State.AddWorkspace(existing); err != nil {
		t.Fatal(err)
	}

	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "existing", "feat/existing")); err == nil {
		t.Fatal("claim with existing workspace name succeeded")
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	if current := inventory.FindSlot(slot.ID); current == nil || current.Status != oven.StatusReady {
		t.Fatalf("name collision changed ready slot: %+v", current)
	}
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "available", "feat/available")); err != nil {
		t.Fatalf("claim after name collision: %v", err)
	}
}

func TestClaimOvenSlotRollsBackSecondBranchFailure(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	template := bakeTestOvenSlot(t, env, "slot-attach-failure", "api", "web")
	injected := errors.New("attach failed")
	options := ovenClaimOptions(env, "failed", "feat/failed")
	options.attachBranch = func(repository oven.ClaimRepository, commit string) error {
		if repository.Name == "web" {
			return injected
		}
		return gitops.AttachWorktreeBranch(repository.PhysicalPath, repository.Branch, commit, repository.BranchCreated)
	}

	if _, err := env.svc.ClaimOvenSlot(options); !errors.Is(err, injected) {
		t.Fatalf("claim error = %v", err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	ready := inventory.FindSlot(template.ID)
	if len(inventory.Slots) != 1 || ready == nil || ready.Status != oven.StatusReady || ready.Claim != nil {
		t.Fatalf("rolled-back inventory = %+v", inventory)
	}
	for _, repository := range template.Repositories {
		if branch, err := gitops.CurrentBranch(repository.WorktreePath); err != nil || branch != "" {
			t.Fatalf("%s branch after rollback = %q, %v", repository.Name, branch, err)
		}
		if gitops.BranchExists(repository.SourceRepo, "feat/failed") {
			t.Fatalf("%s rollback branch remained", repository.Name)
		}
	}
	if workspace, _ := env.svc.State.GetWorkspace("failed"); workspace != nil {
		t.Fatalf("failed claim entered state: %+v", workspace)
	}
}

func TestClaimOvenSlotPreservesExternalAliasRace(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	slot := bakeTestOvenSlot(t, env, "slot-alias-race", "api")
	injected := errors.New("alias occupied")
	options := ovenClaimOptions(env, "raced", "feat/raced")
	options.publishAlias = func(_, target string) error {
		if err := os.WriteFile(target, []byte("external"), 0o644); err != nil {
			return err
		}
		return injected
	}
	if _, err := env.svc.ClaimOvenSlot(options); !errors.Is(err, injected) {
		t.Fatalf("claim error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(env.wsDir, "raced"))
	if err != nil || string(data) != "external" {
		t.Fatalf("external target changed: %q, %v", data, err)
	}
	inventory, _ := env.svc.Oven.Load()
	if current := inventory.FindSlot(slot.ID); len(inventory.Slots) != 1 || current == nil || current.Status != oven.StatusReady {
		t.Fatalf("inventory after alias race = %+v", inventory)
	}
}

func TestClaimOvenSlotTreatsCommittedStateWriteAsSuccess(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	bakeTestOvenSlot(t, env, "slot-uncertain", "api")
	injected := errors.New("uncertain state result")
	options := ovenClaimOptions(env, "uncertain", "feat/uncertain")
	options.saveWorkspace = func(workspace models.Workspace) error {
		if err := env.svc.State.AddWorkspace(workspace); err != nil {
			return err
		}
		return injected
	}
	result, err := env.svc.ClaimOvenSlot(options)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(result.Warning, injected) {
		t.Fatalf("claim warning = %v", result.Warning)
	}
	if workspace, _ := env.svc.State.GetWorkspace("uncertain"); workspace == nil {
		t.Fatal("committed state was rolled back")
	}
	if err := env.svc.DeleteWithOptions("uncertain", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
}

func TestOvenOwnershipBlocksTamperedStatusDeleteAndShapeChanges(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	bakeTestOvenSlot(t, env, "slot-tamper", "api")
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "claimed", "feat/claimed")); err != nil {
		t.Fatal(err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	slot := inventory.ClaimForWorkspace("claimed")
	alias := filepath.Join(env.wsDir, "claimed")
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(env.dir, "replacement")
	if err := os.Mkdir(replacement, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(replacement, alias); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.Status("claimed", StatusOptions{}); err == nil {
		t.Fatal("status accepted retargeted alias")
	}
	if err := env.svc.DeleteWithOptions("claimed", RemoveOptions{Force: true}); err == nil {
		t.Fatal("force delete accepted retargeted alias")
	}
	issues, fixed, err := env.svc.Doctor(true)
	if err != nil || fixed != 0 || len(issues) == 0 {
		t.Fatalf("Doctor changed tampered Oven state: issues=%+v fixed=%d err=%v", issues, fixed, err)
	}
	if workspace, _ := env.svc.State.GetWorkspace("claimed"); workspace == nil {
		t.Fatal("tampered workspace state was removed")
	}
	if _, err := os.Stat(slot.BackingPath); err != nil {
		t.Fatalf("owned backing was removed: %v", err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(slot.BackingPath, alias); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.Rename("claimed", "renamed"); err == nil {
		t.Fatal("renamed Oven-backed workspace")
	}
	if err := env.svc.AddRepos("claimed", []string{"api"}, env.repoMap); err == nil {
		t.Fatal("added repository to Oven-backed workspace")
	}
	if err := env.svc.RemoveRepos("claimed", []string{"api"}); err == nil {
		t.Fatal("removed repository from Oven-backed workspace")
	}
	if err := env.svc.DeleteWithOptions("claimed", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
}

func TestOvenDeleteRemovesWorkspaceRootMetadataAfterRepositories(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	bakeTestOvenSlot(t, env, "slot-root-metadata", "api")
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "root-metadata", "feat/root-metadata")); err != nil {
		t.Fatal(err)
	}
	workspace, err := env.svc.State.GetWorkspace("root-metadata")
	if err != nil || workspace == nil {
		t.Fatalf("workspace = %+v, %v", workspace, err)
	}
	if err := os.MkdirAll(filepath.Join(workspace.Path, ".pi"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Path, ".pi", "metadata.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.DeleteWithOptions("root-metadata", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil || inventory.ClaimForWorkspace("root-metadata") != nil {
		t.Fatalf("claim survived root metadata cleanup: %+v, %v", inventory, err)
	}
}

func TestRemoveOwnedOvenClaimRootRejectsBackingOutsideSlotIdentity(t *testing.T) {
	env := setupTestEnv(t)
	configureTestOven(env)
	outside := filepath.Join(env.dir, "outside")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(outside, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	slot := oven.Slot{
		ID:          testOvenSlotID("unsafe-cleanup"),
		Generation:  testOvenGeneration,
		BackingPath: outside,
		Claim: &oven.Claim{
			Alias: filepath.Join(env.wsDir, "unsafe-cleanup"),
		},
	}

	if err := env.svc.removeOwnedOvenClaimRoot(slot); err == nil {
		t.Fatal("cleanup accepted a backing path outside the slot identity")
	}
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Fatalf("unsafe cleanup changed outside path: %q, %v", data, err)
	}
}

func TestOvenDeletePersistsPartialCleanupAndRetries(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	bakeTestOvenSlot(t, env, "slot-partial-delete", "api", "web")
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "partial", "feat/partial")); err != nil {
		t.Fatal(err)
	}

	realRemove := env.svc.RemoveWorktree
	failed := false
	env.svc.RemoveWorktree = func(repo, path string, force bool) error {
		if filepath.Base(path) == "web" && !failed {
			failed = true
			return errors.New("injected worktree removal failure")
		}
		return gitops.WorktreeRemove(repo, path, force)
	}
	if err := env.svc.DeleteWithOptions("partial", RemoveOptions{Force: true}); err == nil {
		t.Fatal("expected partial deletion failure")
	}
	workspace, err := env.svc.State.GetWorkspace("partial")
	if err != nil || workspace == nil || len(workspace.Repos) != 1 || workspace.Repos[0].RepoName != "web" {
		t.Fatalf("partial workspace state = %+v, %v", workspace, err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	slot := inventory.ClaimForWorkspace("partial")
	if slot == nil || slot.Status != oven.StatusCleanupError || len(slot.Repositories) != 1 || slot.Repositories[0].Name != "web" {
		t.Fatalf("partial Oven state = %+v", slot)
	}

	env.svc.RemoveWorktree = realRemove
	if err := env.svc.DeleteWithOptions("partial", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	inventory, err = env.svc.Oven.Load()
	if err != nil || len(inventory.Slots) != 1 || inventory.Slots[0].Status != oven.StatusReady {
		t.Fatalf("final inventory = %+v, %v", inventory, err)
	}
}

func TestRecoverOvenResumesPersistedCleanupIntent(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	bakeTestOvenSlot(t, env, "slot-crashed-delete", "api", "web")
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "crashed", "feat/crashed")); err != nil {
		t.Fatal(err)
	}

	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	claimed := inventory.ClaimForWorkspace("crashed")
	if claimed == nil {
		t.Fatal("missing claim")
	}
	cleanup, err := env.svc.beginOvenCleanup(claimed.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	api := ovenClaimRepositoryByName(*cleanup.Claim, "api")
	if err := gitops.WorktreeRemove(api.SourceRepo, api.PhysicalPath, true); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.RecoverOven(); err != nil {
		t.Fatal(err)
	}
	if workspace, _ := env.svc.State.GetWorkspace("crashed"); workspace != nil {
		t.Fatalf("workspace survived cleanup recovery: %+v", workspace)
	}
	inventory, err = env.svc.Oven.Load()
	if err != nil || len(inventory.Slots) != 1 || inventory.Slots[0].Status != oven.StatusReady {
		t.Fatalf("inventory after cleanup recovery = %+v, %v", inventory, err)
	}
}

func TestOvenOwnershipFailsClosedWhenInventoryDisappears(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	slot := bakeTestOvenSlot(t, env, "slot-missing-inventory", "api")
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "missing", "feat/missing")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(env.svc.Oven.Path); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.VerifyWorkspaceOwnership("missing"); err == nil {
		t.Fatal("missing inventory disabled ownership checks")
	}
	if err := env.svc.DeleteWithOptions("missing", RemoveOptions{Force: true}); err == nil {
		t.Fatal("force delete bypassed missing inventory")
	}
	if workspace, _ := env.svc.State.GetWorkspace("missing"); workspace == nil {
		t.Fatal("workspace state was removed")
	}
	if _, err := os.Stat(slot.BackingPath); err != nil {
		t.Fatalf("backing path was removed: %v", err)
	}
}

func TestOvenOwnershipIgnoresOtherOvenWorktreesForNormalWorkspace(t *testing.T) {
	env := setupTestEnv(t)
	sourceRepo := env.createRepo("api")
	if err := env.svc.Create("normal", "feat/normal", []string{"api"}, env.repoMap, env.cfg); err != nil {
		t.Fatal(err)
	}
	env.svc.Oven = oven.NewStore(canonicalPath(env.groveDir))
	if err := env.svc.Oven.Save(oven.Inventory{Version: oven.InventoryVersion}); err != nil {
		t.Fatal(err)
	}
	commit, err := gitops.HeadCommit(sourceRepo)
	if err != nil {
		t.Fatal(err)
	}
	if err := gitops.WorktreeAddDetached(sourceRepo, filepath.Join(env.svc.Oven.Root, "unrelated"), commit); err != nil {
		t.Fatal(err)
	}

	if err := env.svc.VerifyWorkspaceOwnership("normal"); err != nil {
		t.Fatalf("normal workspace was mistaken for an Oven claim: %v", err)
	}
	if err := env.svc.DeleteWithOptions("normal", RemoveOptions{Force: true}); err != nil {
		t.Fatalf("normal workspace deletion was blocked: %v", err)
	}
}

func TestTamperedReadySlotBlocksInsteadOfFallingBack(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	slot := bakeTestOvenSlot(t, env, "slot-blocked-claim", "api")
	if err := os.WriteFile(filepath.Join(slot.Repositories[0].WorktreePath, "README.md"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "blocked", "feat/blocked"))
	if !errors.Is(err, ErrOvenBlocked) || errors.Is(err, ErrOvenMiss) {
		t.Fatalf("tampered claim error = %v", err)
	}
	_, err = env.svc.ClaimOvenSlot(ovenClaimOptions(env, "blocked", "feat/blocked"))
	if !errors.Is(err, ErrOvenBlocked) {
		t.Fatalf("quarantined claim error = %v", err)
	}
	if workspace, _ := env.svc.State.GetWorkspace("blocked"); workspace != nil {
		t.Fatalf("blocked claim created workspace: %+v", workspace)
	}
}

func TestRecoverOvenRollsBackPartiallyAttachedClaim(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	slot := bakeTestOvenSlot(t, env, "slot-partial-claim", "api", "web")
	options := ovenClaimOptions(env, "partial-claim", "feat/partial-claim")
	claim, err := env.svc.newOvenClaim(options, *slot)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	current := inventory.FindSlot(slot.ID)
	current.Status = oven.StatusClaiming
	current.Claim = &claim
	if err := env.svc.Oven.Save(inventory); err != nil {
		t.Fatal(err)
	}
	first := claim.Repositories[0]
	commit := ovenRepositoryByName(*slot, first.Name).Commit
	if err := gitops.AttachWorktreeBranch(first.PhysicalPath, first.Branch, commit, first.BranchCreated); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(slot.BackingPath, claim.Alias); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.RecoverOven(); err != nil {
		t.Fatal(err)
	}
	inventory, err = env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	current = inventory.FindSlot(slot.ID)
	if current == nil || current.Status != oven.StatusReady || current.Claim != nil {
		t.Fatalf("recovered slot = %+v", current)
	}
	if _, err := os.Lstat(claim.Alias); !os.IsNotExist(err) {
		t.Fatalf("interrupted alias survived: %v", err)
	}
	for _, repository := range current.Repositories {
		if branch, err := gitops.CurrentBranch(repository.WorktreePath); err != nil || branch != "" {
			t.Fatalf("%s branch after recovery = %q, %v", repository.Name, branch, err)
		}
	}
}

func TestRecoverOvenRemovesPartiallyMaterializedReusableClaim(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	template := bakeTestOvenSlot(t, env, "slot-partial-materialization", "api", "web")
	claimID := testOvenSlotID("claim-partial-materialization")
	claimSlot := oven.Slot{
		ID: claimID, TemplateSlotID: template.ID,
		RecipeKey: template.RecipeKey, RecipeName: template.RecipeName, RecipePath: template.RecipePath,
		Generation: template.Generation, Runner: template.Runner,
		BackingPath: env.svc.Oven.SlotPath(template.Generation, claimID),
		Status:      oven.StatusClaiming, CreatedAt: template.CreatedAt, UpdatedAt: template.UpdatedAt,
	}
	for _, repository := range template.Repositories {
		claimSlot.Repositories = append(claimSlot.Repositories, oven.Repository{
			Name: repository.Name, SourceRepo: repository.SourceRepo,
			WorktreePath: filepath.Join(claimSlot.BackingPath, repository.Name), Commit: repository.Commit,
		})
	}
	options := ovenClaimOptions(env, "partial-materialization", "feat/partial-materialization")
	claim, err := env.svc.newOvenClaim(options, claimSlot)
	if err != nil {
		t.Fatal(err)
	}
	claimSlot.Claim = &claim
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	inventory.Slots = append(inventory.Slots, claimSlot)
	if err := env.svc.Oven.Save(inventory); err != nil {
		t.Fatal(err)
	}
	if err := createOvenBackingDirectory(env.svc.Oven, claimSlot); err != nil {
		t.Fatal(err)
	}
	first := claimSlot.Repositories[0]
	if err := gitops.WorktreeAddDetached(first.SourceRepo, first.WorktreePath, first.Commit); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(first.WorktreePath, first.WorktreePath+".checkout"); err != nil {
		t.Fatal(err)
	}

	if err := env.svc.RecoverOven(); err != nil {
		t.Fatal(err)
	}
	inventory, err = env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Slots) != 1 || inventory.FindSlot(template.ID) == nil || inventory.FindSlot(claimID) != nil {
		t.Fatalf("recovered inventory = %+v", inventory)
	}
	if _, err := os.Lstat(claimSlot.BackingPath); !os.IsNotExist(err) {
		t.Fatalf("partial materialization backing remained: %v", err)
	}
}

func TestRecoverOvenRemovesReusableClaimBeforeBackingCreation(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	template := bakeTestOvenSlot(t, env, "slot-pre-backing-crash", "api")
	claimID := testOvenSlotID("claim-pre-backing-crash")
	claimSlot := oven.Slot{
		ID: claimID, TemplateSlotID: template.ID,
		RecipeKey: template.RecipeKey, RecipeName: template.RecipeName, RecipePath: template.RecipePath,
		Generation: template.Generation, Runner: template.Runner,
		BackingPath: env.svc.Oven.SlotPath(template.Generation, claimID),
		Status:      oven.StatusClaiming, CreatedAt: template.CreatedAt, UpdatedAt: template.UpdatedAt,
		Repositories: []oven.Repository{{
			Name: "api", SourceRepo: template.Repositories[0].SourceRepo,
			WorktreePath: filepath.Join(env.svc.Oven.SlotPath(template.Generation, claimID), "api"),
			Commit:       template.Repositories[0].Commit,
		}},
	}
	options := ovenClaimOptions(env, "pre-backing-crash", "feat/pre-backing-crash")
	claim, err := env.svc.newOvenClaim(options, claimSlot)
	if err != nil {
		t.Fatal(err)
	}
	claimSlot.Claim = &claim
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	inventory.Slots = append(inventory.Slots, claimSlot)
	if err := env.svc.Oven.Save(inventory); err != nil {
		t.Fatal(err)
	}

	if err := env.svc.RecoverOven(); err != nil {
		t.Fatal(err)
	}
	inventory, err = env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Slots) != 1 || inventory.FindSlot(template.ID) == nil || inventory.FindSlot(claimID) != nil {
		t.Fatalf("recovered pre-backing inventory = %+v", inventory)
	}
}

func TestOvenOwnershipRejectsClaimNonceTampering(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	bakeTestOvenSlot(t, env, "slot-nonce-tamper", "api")
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "nonce", "feat/nonce")); err != nil {
		t.Fatal(err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	slot := inventory.ClaimForWorkspace("nonce")
	original := slot.Claim.Nonce
	slot.Claim.Nonce = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	if err := env.svc.Oven.Save(inventory); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.Status("nonce", StatusOptions{}); err == nil {
		t.Fatal("status accepted a changed claim nonce")
	}
	if err := env.svc.DeleteWithOptions("nonce", RemoveOptions{Force: true}); err == nil {
		t.Fatal("delete accepted a changed claim nonce")
	}
	inventory, _ = env.svc.Oven.Load()
	inventory.ClaimForWorkspace("nonce").Claim.Nonce = original
	if err := env.svc.Oven.Save(inventory); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.DeleteWithOptions("nonce", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
}

func TestClaimOvenRejectsSourceOutsideConfiguredRepoDirs(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	bakeTestOvenSlot(t, env, "slot-source-allowlist", "api")
	env.cfg.RepoDirs = []string{filepath.Join(env.dir, "other-repositories")}
	_, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "blocked-source", "feat/blocked-source"))
	if err == nil || errors.Is(err, ErrOvenBlocked) {
		t.Fatalf("claim error = %v", err)
	}
	if workspace, _ := env.svc.State.GetWorkspace("blocked-source"); workspace != nil {
		t.Fatalf("blocked source created workspace: %+v", workspace)
	}
	inventory, loadErr := env.svc.Oven.Load()
	if loadErr != nil || inventory.ReadySlot(testOvenRecipeKey, "test-runner") == nil {
		t.Fatalf("claim-local policy failure poisoned template: %+v, %v", inventory, loadErr)
	}
}

func TestClaimOvenRejectsSymlinkedBackingComponent(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	slot := bakeTestOvenSlot(t, env, "slot-symlink-component", "api")
	generationRoot := filepath.Dir(filepath.Dir(slot.BackingPath))
	relocated := generationRoot + "-relocated"
	if err := os.Rename(generationRoot, relocated); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relocated, generationRoot); err != nil {
		t.Fatal(err)
	}
	_, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "blocked-symlink", "feat/blocked-symlink"))
	if !errors.Is(err, ErrOvenBlocked) {
		t.Fatalf("claim error = %v", err)
	}
	if workspace, _ := env.svc.State.GetWorkspace("blocked-symlink"); workspace != nil {
		t.Fatalf("symlinked backing created workspace: %+v", workspace)
	}
}

func TestRecoverOvenRetriesAfterWorkspaceStateRemovalFailure(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	bakeTestOvenSlot(t, env, "slot-state-remove-failure", "api")
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "state-failure", "feat/state-failure")); err != nil {
		t.Fatal(err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	claimed := inventory.ClaimForWorkspace("state-failure")
	if _, err := env.svc.beginOvenCleanup(claimed.ID, true); err != nil {
		t.Fatal(err)
	}
	failed := false
	env.svc.RemoveWorkspace = func(string) error {
		if !failed {
			failed = true
			return errors.New("injected state removal failure")
		}
		return env.svc.State.RemoveWorkspace("state-failure")
	}
	if err := env.svc.RecoverOven(); err != nil {
		t.Fatal(err)
	}
	workspace, err := env.svc.State.GetWorkspace("state-failure")
	if err != nil || workspace == nil {
		t.Fatalf("state was unexpectedly removed: %+v, %v", workspace, err)
	}
	if _, err := os.Lstat(workspace.Path); !os.IsNotExist(err) {
		t.Fatalf("alias should have been removed before injected state error: %v", err)
	}
	if err := env.svc.RecoverOven(); err != nil {
		t.Fatal(err)
	}
	if workspace, _ = env.svc.State.GetWorkspace("state-failure"); workspace != nil {
		t.Fatalf("state survived retry: %+v", workspace)
	}
	inventory, err = env.svc.Oven.Load()
	if err != nil || len(inventory.Slots) != 1 || inventory.Slots[0].Status != oven.StatusReady {
		t.Fatalf("inventory after retry = %+v, %v", inventory, err)
	}
}

func TestDoctorReportsClaimWithoutWorkspaceState(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	bakeTestOvenSlot(t, env, "slot-orphaned-claim", "api")
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "orphaned", "feat/orphaned")); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.State.RemoveWorkspace("orphaned"); err != nil {
		t.Fatal(err)
	}
	issues, fixed, err := env.svc.Doctor(true)
	if err != nil || fixed != 0 || len(issues) == 0 || !strings.Contains(issues[0].Issue, "has no workspace state") {
		t.Fatalf("Doctor result = issues %+v, fixed %d, err %v", issues, fixed, err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil || inventory.ClaimForWorkspace("orphaned") == nil {
		t.Fatalf("Doctor mutated orphaned claim: %+v, %v", inventory, err)
	}
}
