package workspace

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/oven"
	"github.com/nicksenap/grove/internal/state"
)

const (
	testOvenRecipeKey  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testOvenGeneration = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func testOvenSlotID(label string) string {
	sum := sha256.Sum256([]byte(label))
	return fmt.Sprintf("%x", sum[:16])
}

func ovenBakeOptions(env *testEnv, slotID string, repoNames ...string) OvenBakeOptions {
	commits := make(map[string]string, len(repoNames))
	for _, name := range repoNames {
		commits[name] = env.run(env.repoMap[name], "git", "rev-parse", "HEAD")
	}
	return OvenBakeOptions{
		RecipeKey: testOvenRecipeKey, RecipeName: "stack", RecipePath: "/recipes/stack.yaml",
		Generation: testOvenGeneration, Runner: "test-runner", Repos: repoNames,
		RepoMap: env.repoMap, Commits: commits, slotID: testOvenSlotID(slotID),
		now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	}
}

func configureTestOven(env *testEnv) {
	env.svc.Oven = oven.NewStore(env.groveDir)
}

func TestBakeOvenSlotPreparesDetachedWorktreesOutsideLock(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	configureTestOven(env)
	options := ovenBakeOptions(env, "slot-ready", "api", "web")
	callbackRan := false

	slot, err := env.svc.BakeOvenSlot(options, func(worktrees map[string]string) error {
		callbackRan = true
		otherStore := state.NewStore(env.groveDir)
		if err := otherStore.WithLock(func() error { return nil }); err != nil {
			return err
		}
		for name, path := range worktrees {
			if branch, err := gitops.CurrentBranch(path); err != nil || branch != "" {
				return errors.New(name + " was not detached")
			}
			if err := os.MkdirAll(filepath.Join(path, "node_modules"), 0o755); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !callbackRan || slot.Status != oven.StatusReady {
		t.Fatalf("ready slot = %+v, callback = %v", slot, callbackRan)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	ready := inventory.ReadySlot(options.RecipeKey, options.Runner)
	if ready == nil || ready.ID != slot.ID {
		t.Fatalf("inventory ready slot = %+v", ready)
	}
	for _, repository := range slot.Repositories {
		if branch, err := gitops.CurrentBranch(repository.WorktreePath); err != nil || branch != "" {
			t.Fatalf("%s branch = %q, %v", repository.Name, branch, err)
		}
		if head, err := gitops.HeadCommit(repository.WorktreePath); err != nil || head != repository.Commit {
			t.Fatalf("%s HEAD = %q, %v", repository.Name, head, err)
		}
	}
}

func TestBakeOvenSlotFailureIsNeverReadyAndCleansOwnedWorktrees(t *testing.T) {
	env := setupTestEnv(t)
	source := env.createRepo("api")
	configureTestOven(env)
	options := ovenBakeOptions(env, "slot-failed", "api")
	injected := errors.New("prepare failed")

	_, err := env.svc.BakeOvenSlot(options, func(worktrees map[string]string) error {
		return errors.Join(injected, os.WriteFile(filepath.Join(worktrees["api"], "partial"), []byte("partial"), 0o644))
	})
	var bakeErr *OvenBakeError
	if !errors.As(err, &bakeErr) || !errors.Is(err, injected) {
		t.Fatalf("bake error = %v", err)
	}
	inventory, loadErr := env.svc.Oven.Load()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if len(inventory.Slots) != 1 || inventory.Slots[0].Status != oven.StatusFailed || inventory.ReadySlot(options.RecipeKey, options.Runner) != nil {
		t.Fatalf("failed inventory = %+v", inventory)
	}
	if _, statErr := os.Stat(optionsPath(env, options)); !os.IsNotExist(statErr) {
		t.Fatalf("failed backing path remained: %v", statErr)
	}
	for _, entry := range mustWorktreeList(t, source) {
		if canonicalPath(entry.Path) == canonicalPath(filepath.Join(optionsPath(env, options), "api")) {
			t.Fatalf("failed worktree remained registered: %+v", entry)
		}
	}
}

func TestBakeOvenSlotDuplicateIDDoesNotTouchExistingSlot(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	configureTestOven(env)
	options := ovenBakeOptions(env, "slot-existing", "api")
	first, err := env.svc.BakeOvenSlot(options, func(map[string]string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.BakeOvenSlot(options, func(map[string]string) error { return nil }); err == nil {
		t.Fatal("expected duplicate slot ID failure")
	}
	options.slotID = "slot-other"
	if _, err := env.svc.BakeOvenSlot(options, func(map[string]string) error { return nil }); !errors.Is(err, ErrOvenGenerationActive) {
		t.Fatalf("concurrent generation error = %v", err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Slots) != 1 || inventory.Slots[0].Status != oven.StatusReady {
		t.Fatalf("existing slot changed: %+v", inventory)
	}
	if _, err := os.Stat(first.Repositories[0].WorktreePath); err != nil {
		t.Fatalf("existing worktree was removed: %v", err)
	}
}

func TestBakeOvenSlotCleansEarlierWorktreeAfterPartialProvisionFailure(t *testing.T) {
	env := setupTestEnv(t)
	api := env.createRepo("api")
	env.createRepo("web")
	configureTestOven(env)
	options := ovenBakeOptions(env, "slot-partial", "api", "web")
	options.Commits["web"] = "missing-object"

	if _, err := env.svc.BakeOvenSlot(options, func(map[string]string) error { return nil }); err == nil {
		t.Fatal("expected partial provisioning failure")
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Slots[0].Status != oven.StatusFailed {
		t.Fatalf("partial slot = %+v", inventory.Slots[0])
	}
	for _, entry := range mustWorktreeList(t, api) {
		if canonicalPath(entry.Path) == canonicalPath(filepath.Join(optionsPath(env, options), "api")) {
			t.Fatalf("first partial worktree remained: %+v", entry)
		}
	}
}

func TestBakeOvenSlotNeverReadiesCredentialResidue(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	configureTestOven(env)
	options := ovenBakeOptions(env, "slot-credential", "api")

	_, err := env.svc.BakeOvenSlot(options, func(worktrees map[string]string) error {
		return os.WriteFile(filepath.Join(worktrees["api"], ".npmrc"), []byte("token=secret-value"), 0o600)
	})
	if err == nil {
		t.Fatal("credential-bearing slot became ready")
	}
	inventory, loadErr := env.svc.Oven.Load()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	slot := inventory.FindSlot(options.slotID)
	if slot == nil || slot.Status != oven.StatusQuarantined || inventory.ReadySlot(options.RecipeKey, options.Runner) != nil {
		t.Fatalf("credential slot = %+v", slot)
	}
	if strings.Contains(slot.Failure, "secret-value") {
		t.Fatalf("credential value entered inventory: %q", slot.Failure)
	}
}

func TestBakeOvenSlotQuarantinesChangedWorktreeIdentity(t *testing.T) {
	env := setupTestEnv(t)
	source := env.createRepo("api")
	configureTestOven(env)
	options := ovenBakeOptions(env, "slot-quarantine", "api")

	_, err := env.svc.BakeOvenSlot(options, func(worktrees map[string]string) error {
		_, err := spikeGit(worktrees["api"], "switch", "-c", "unexpected")
		return err
	})
	if err == nil {
		t.Fatal("expected changed identity failure")
	}
	inventory, loadErr := env.svc.Oven.Load()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if inventory.Slots[0].Status != oven.StatusQuarantined || inventory.ReadySlot(options.RecipeKey, options.Runner) != nil {
		t.Fatalf("quarantined inventory = %+v", inventory)
	}
	worktreePath := filepath.Join(optionsPath(env, options), "api")
	if _, statErr := os.Stat(worktreePath); statErr != nil {
		t.Fatalf("quarantined worktree was removed: %v", statErr)
	}
	if err := gitops.WorktreeRemove(source, worktreePath, true); err != nil {
		t.Fatal(err)
	}
	if err := gitops.DeleteBranch(source, "unexpected", true); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverOvenSkipsLiveBakeAndCleansInterruptedBake(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	configureTestOven(env)
	liveOptions := ovenBakeOptions(env, "slot-live", "api")
	liveSlot, err := env.svc.newBakingSlot(liveOptions)
	if err != nil {
		t.Fatal(err)
	}
	if _, recorded, err := env.svc.createDetachedSlot(liveSlot); err != nil || !recorded {
		t.Fatalf("creating live slot: recorded=%v err=%v", recorded, err)
	}
	if err := env.svc.RecoverOven(); err != nil {
		t.Fatal(err)
	}
	inventory, _ := env.svc.Oven.Load()
	if slot := inventory.FindSlot(liveSlot.ID); slot == nil || slot.Status != oven.StatusBaking {
		t.Fatalf("live bake was recovered concurrently: %+v", slot)
	}

	if err := inventory.UpdateSlot(liveSlot.ID, func(slot *oven.Slot) error {
		slot.OwnerPID = 0
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.Oven.Save(inventory); err != nil {
		t.Fatal(err)
	}
	if err := env.svc.RecoverOven(); err != nil {
		t.Fatal(err)
	}
	inventory, _ = env.svc.Oven.Load()
	if slot := inventory.FindSlot(liveSlot.ID); slot == nil || slot.Status != oven.StatusFailed {
		t.Fatalf("interrupted bake was not failed: %+v", slot)
	}
	if _, err := os.Stat(liveSlot.BackingPath); !os.IsNotExist(err) {
		t.Fatalf("interrupted backing remained: %v", err)
	}
}

func TestRecoverOvenResolvesClaimingRecordsConservatively(t *testing.T) {
	t.Run("completed-state", func(t *testing.T) {
		env := setupTestEnv(t)
		env.createRepo("api")
		slot := bakeTestOvenSlot(t, env, "slot-claim-recovery", "api")
		if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "claimed", "feat/claimed")); err != nil {
			t.Fatal(err)
		}
		inventory, _ := env.svc.Oven.Load()
		inventory.FindSlot(slot.ID).Status = oven.StatusClaiming
		if err := env.svc.Oven.Save(inventory); err != nil {
			t.Fatal(err)
		}
		if err := env.svc.RecoverOven(); err != nil {
			t.Fatal(err)
		}
		inventory, _ = env.svc.Oven.Load()
		if inventory.FindSlot(slot.ID).Status != oven.StatusClaimed {
			t.Fatalf("completed claim was not finalized: %+v", inventory.FindSlot(slot.ID))
		}
		if err := env.svc.DeleteWithOptions("claimed", RemoveOptions{Force: true}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unstarted", func(t *testing.T) {
		env := setupTestEnv(t)
		env.createRepo("api")
		slot := bakeTestOvenSlot(t, env, "slot-unstarted-claim", "api")
		inventory, _ := env.svc.Oven.Load()
		current := inventory.FindSlot(slot.ID)
		current.Status = oven.StatusClaiming
		current.Claim = &oven.Claim{
			Nonce: "dddddddddddddddddddddddddddddddd", WorkspaceName: "unused", Alias: filepath.Join(env.wsDir, "unused"), Branch: "feat/unused",
			Repositories: []oven.ClaimRepository{{
				Name: "api", SourceRepo: slot.Repositories[0].SourceRepo,
				PhysicalPath: slot.Repositories[0].WorktreePath,
				AliasPath:    filepath.Join(env.wsDir, "unused", "api"), Branch: "feat/unused", BranchCreated: true,
			}},
		}
		if err := env.svc.Oven.Save(inventory); err != nil {
			t.Fatal(err)
		}
		if err := env.svc.RecoverOven(); err != nil {
			t.Fatal(err)
		}
		inventory, _ = env.svc.Oven.Load()
		current = inventory.FindSlot(slot.ID)
		if current.Status != oven.StatusReady || current.Claim != nil {
			t.Fatalf("unstarted claim was not reset: %+v", current)
		}
	})
}

func TestCleanOvenRemovesReadyButBlocksClaimedAndQuarantined(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	claimed := bakeTestOvenSlot(t, env, "slot-claimed-clean", "api")
	if _, err := env.svc.ClaimOvenSlot(ovenClaimOptions(env, "claimed", "feat/claimed")); err != nil {
		t.Fatal(err)
	}
	ready := bakeTestOvenSlot(t, env, "slot-ready-clean", "api")
	inventory, _ := env.svc.Oven.Load()
	quarantinedID := testOvenSlotID("slot-quarantined")
	quarantined := oven.Slot{
		ID: quarantinedID, RecipeKey: testOvenRecipeKey, RecipePath: "/recipes/stack.yaml",
		Generation: testOvenGeneration, Runner: "test-runner", Status: oven.StatusQuarantined,
		BackingPath: env.svc.Oven.SlotPath(testOvenGeneration, quarantinedID),
	}
	inventory.Slots = append(inventory.Slots, quarantined)
	if err := env.svc.Oven.Save(inventory); err != nil {
		t.Fatal(err)
	}
	result, err := env.svc.CleanOven("")
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 1 || len(result.Blocked) != 2 {
		t.Fatalf("clean result = %+v", result)
	}
	inventory, _ = env.svc.Oven.Load()
	if inventory.FindSlot(ready.ID) != nil || inventory.FindSlot(claimed.ID) == nil || inventory.FindSlot(quarantinedID) == nil {
		t.Fatalf("clean inventory = %+v", inventory)
	}
	if err := env.svc.DeleteWithOptions("claimed", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
}

func optionsPath(env *testEnv, options OvenBakeOptions) string {
	return env.svc.Oven.SlotPath(options.Generation, options.slotID)
}

func mustWorktreeList(t *testing.T, source string) []gitops.WorktreeEntry {
	t.Helper()
	entries, err := gitops.WorktreeList(source)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestConcurrentBakePublishesAtMostOneReadySlot(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	configureTestOven(env)
	first := ovenBakeOptions(env, "slot-concurrent-first", "api")
	second := ovenBakeOptions(env, "slot-concurrent-second", "api")
	started := make(chan struct{})
	release := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		_, err := env.svc.BakeOvenSlot(first, func(map[string]string) error {
			close(started)
			<-release
			return nil
		})
		firstResult <- err
	}()
	<-started
	if _, err := env.svc.BakeOvenSlot(second, nil); !errors.Is(err, ErrOvenGenerationActive) {
		t.Fatalf("concurrent bake error = %v", err)
	}
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	ready := 0
	for _, slot := range inventory.Slots {
		if slot.Status == oven.StatusReady {
			ready++
		}
	}
	if ready != 1 {
		t.Fatalf("ready slots = %d: %+v", ready, inventory.Slots)
	}
}

func TestFailedReplacementBakePreservesReadyGeneration(t *testing.T) {
	env := setupTestEnv(t)
	source := env.createRepo("api")
	ready := bakeTestOvenSlot(t, env, "slot-old-generation", "api")
	if err := os.WriteFile(filepath.Join(source, "next.txt"), []byte("next"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := spikeGit(source, "add", "next.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := spikeGit(source, "commit", "-m", "next"); err != nil {
		t.Fatal(err)
	}
	commit, err := gitops.HeadCommit(source)
	if err != nil {
		t.Fatal(err)
	}
	replacement := ovenBakeOptions(env, "slot-new-generation", "api")
	replacement.Generation = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	replacement.Commits["api"] = commit
	if _, err := env.svc.BakeOvenSlot(replacement, func(map[string]string) error {
		return errors.New("replacement preparation failed")
	}); err == nil {
		t.Fatal("expected replacement bake failure")
	}
	inventory, err := env.svc.Oven.Load()
	if err != nil {
		t.Fatal(err)
	}
	if slot := inventory.FindSlot(ready.ID); slot == nil || slot.Status != oven.StatusReady {
		t.Fatalf("previous ready generation was not preserved: %+v", slot)
	}
	if inventory.ReadySlot(replacement.RecipeKey, replacement.Runner).ID != ready.ID {
		t.Fatal("failed replacement became claimable")
	}
}

func TestBakeOvenSlotAllowsEnvironmentTemplates(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	configureTestOven(env)
	options := ovenBakeOptions(env, "slot-env-template", "api")
	if _, err := env.svc.BakeOvenSlot(options, func(worktrees map[string]string) error {
		return os.WriteFile(filepath.Join(worktrees["api"], ".env.local.example"), []byte("TOKEN="), 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestClaimAndCleanSerializeThroughStateLock(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	bakeTestOvenSlot(t, env, "slot-claim-clean-race", "api")
	options := ovenClaimOptions(env, "serialized", "feat/serialized")
	started := make(chan struct{})
	release := make(chan struct{})
	options.attachBranch = func(repository oven.ClaimRepository, commit string) error {
		close(started)
		<-release
		return gitops.AttachWorktreeBranch(repository.PhysicalPath, repository.Branch, commit, repository.BranchCreated)
	}
	claimResult := make(chan error, 1)
	go func() {
		_, err := env.svc.ClaimOvenSlot(options)
		claimResult <- err
	}()
	<-started
	cleanResult := make(chan OvenCleanResult, 1)
	cleanError := make(chan error, 1)
	go func() {
		result, err := env.svc.CleanOven("")
		cleanResult <- result
		cleanError <- err
	}()
	select {
	case <-cleanResult:
		t.Fatal("clean bypassed the claim mutation lock")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-claimResult; err != nil {
		t.Fatal(err)
	}
	result := <-cleanResult
	if err := <-cleanError; err != nil {
		t.Fatal(err)
	}
	if result.Removed != 0 || len(result.Blocked) != 1 {
		t.Fatalf("clean result after claim = %+v", result)
	}
	if err := env.svc.DeleteWithOptions("serialized", RemoveOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
}

func TestOvenCredentialPathMatrix(t *testing.T) {
	forbidden := []string{
		".netrc", ".pypirc", ".git-credentials", "id_rsa", "id_ed25519", ".env", ".env.local",
		filepath.Join(".aws", "credentials"), filepath.Join(".docker", "config.json"),
	}
	for _, path := range forbidden {
		if !forbiddenOvenCredentialPath(path) {
			t.Errorf("expected %s to be forbidden", path)
		}
	}
	allowed := []string{".env.example", ".env.local.example", ".env.dev.sample", ".env.prod.template"}
	for _, path := range allowed {
		if forbiddenOvenCredentialPath(path) {
			t.Errorf("expected %s to be allowed", path)
		}
	}
}
