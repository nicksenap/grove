package oven

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const (
	testStoreSlotID     = "11111111111111111111111111111111"
	testStoreOtherID    = "22222222222222222222222222222222"
	testStoreRecipeKey  = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testStoreGeneration = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestStoreRoundTripUsesPrivatePermissions(t *testing.T) {
	store := NewStore(t.TempDir())
	inventory := Inventory{Version: InventoryVersion, Slots: []Slot{{
		ID: testStoreSlotID, RecipeKey: testStoreRecipeKey, Generation: testStoreGeneration, Runner: "runner",
		RecipeName: "stack", RecipePath: "/recipes/stack.yaml", BackingPath: store.SlotPath(testStoreGeneration, testStoreSlotID),
		Status: StatusReady, CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
		Repositories: []Repository{{Name: "api", SourceRepo: "/repos/api", WorktreePath: filepath.Join(store.SlotPath(testStoreGeneration, testStoreSlotID), "api"), Commit: "aaaa"}},
	}}}
	if err := store.Save(inventory); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Slots) != 1 || loaded.Slots[0].ID != testStoreSlotID || loaded.Slots[0].Status != StatusReady {
		t.Fatalf("round trip = %+v", loaded)
	}
	if info, err := os.Stat(store.Root); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o700 {
		t.Fatalf("root permissions = %v", info.Mode().Perm())
	}
	if info, err := os.Stat(store.Path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Fatalf("inventory permissions = %v", info.Mode().Perm())
	}
}

func TestStoreMigratesVersionOneInventoryWithReusableClaims(t *testing.T) {
	store := NewStore(t.TempDir())
	template, claim := testStoreTemplateAndClaim(store)
	legacy := Inventory{Version: 1, Slots: []Slot{template, claim}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != InventoryVersion || len(loaded.Slots) != 2 {
		t.Fatalf("migrated inventory = %+v", loaded)
	}
	if err := store.Save(loaded); err != nil {
		t.Fatal(err)
	}
	persisted, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(persisted, &envelope); err != nil || envelope.Version != InventoryVersion {
		t.Fatalf("persisted migration version = %d, %v", envelope.Version, err)
	}
}

func TestStoreMigratesVersionOneLegacyClaimWithoutTemplateReference(t *testing.T) {
	store := NewStore(t.TempDir())
	template, claim := testStoreTemplateAndClaim(store)
	claim.TemplateSlotID = ""
	data, err := json.Marshal(Inventory{Version: 1, Slots: []Slot{template, claim}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(store.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != InventoryVersion || loaded.FindSlot(claim.ID) == nil ||
		loaded.FindSlot(claim.ID).TemplateSlotID != "" {
		t.Fatalf("migrated legacy claim = %+v", loaded)
	}
}

func TestStoreRejectsInvalidInventory(t *testing.T) {
	store := NewStore(t.TempDir())
	valid := Slot{
		ID: testStoreSlotID, RecipeKey: testStoreRecipeKey, Generation: testStoreGeneration, Runner: "runner",
		BackingPath: store.SlotPath(testStoreGeneration, testStoreSlotID), Status: StatusReady,
		Repositories: []Repository{{Name: "api", SourceRepo: "/repos/api", WorktreePath: filepath.Join(store.SlotPath(testStoreGeneration, testStoreSlotID), "api"), Commit: "aaaa"}},
	}
	tests := map[string]Inventory{
		"version":   {Version: InventoryVersion + 1},
		"duplicate": {Version: InventoryVersion, Slots: []Slot{valid, valid}},
		"dangling template": func() Inventory {
			_, claim := testStoreTemplateAndClaim(store)
			return Inventory{Version: InventoryVersion, Slots: []Slot{claim}}
		}(),
		"status": func() Inventory {
			invalid := valid
			invalid.Repositories = append([]Repository(nil), valid.Repositories...)
			invalid.ID = testStoreOtherID
			invalid.BackingPath = store.SlotPath(testStoreGeneration, testStoreOtherID)
			invalid.Repositories[0].WorktreePath = filepath.Join(invalid.BackingPath, "api")
			invalid.Status = SlotStatus("unknown")
			return Inventory{Version: InventoryVersion, Slots: []Slot{invalid}}
		}(),
		"path": func() Inventory {
			invalid := valid
			invalid.ID = testStoreOtherID
			invalid.BackingPath = filepath.Join(t.TempDir(), "outside")
			return Inventory{Version: InventoryVersion, Slots: []Slot{invalid}}
		}(),
	}
	for name, inventory := range tests {
		t.Run(name, func(t *testing.T) {
			if err := store.Save(inventory); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func testStoreTemplateAndClaim(store *Store) (Slot, Slot) {
	templatePath := store.SlotPath(testStoreGeneration, testStoreSlotID)
	template := Slot{
		ID: testStoreSlotID, RecipeKey: testStoreRecipeKey, Generation: testStoreGeneration, Runner: "runner",
		BackingPath: templatePath, Status: StatusReady,
		Repositories: []Repository{{
			Name: "api", SourceRepo: "/repos/api", WorktreePath: filepath.Join(templatePath, "api"), Commit: "aaaa",
		}},
	}
	claimPath := store.SlotPath(testStoreGeneration, testStoreOtherID)
	alias := "/workspaces/cake"
	claim := Slot{
		ID: testStoreOtherID, TemplateSlotID: template.ID,
		RecipeKey: template.RecipeKey, Generation: template.Generation, Runner: template.Runner,
		BackingPath: claimPath, Status: StatusClaimed,
		Repositories: []Repository{{
			Name: "api", SourceRepo: "/repos/api", WorktreePath: filepath.Join(claimPath, "api"), Commit: "aaaa",
		}},
		Claim: &Claim{
			Nonce: "cccccccccccccccccccccccccccccccc", WorkspaceName: "cake",
			Alias: alias, Branch: "feat/cake",
			Repositories: []ClaimRepository{{
				Name: "api", SourceRepo: "/repos/api", PhysicalPath: filepath.Join(claimPath, "api"),
				AliasPath: filepath.Join(alias, "api"), Branch: "feat/cake", BranchCreated: true,
			}},
		},
	}
	return template, claim
}

func TestInventorySelectsNewestReadySlot(t *testing.T) {
	inventory := Inventory{Version: InventoryVersion, Slots: []Slot{
		{ID: "old", RecipeKey: "recipe", Runner: "runner", Status: StatusReady, UpdatedAt: "2026-01-01T00:00:00Z"},
		{ID: "new", RecipeKey: "recipe", Runner: "runner", Status: StatusReady, UpdatedAt: "2026-01-02T00:00:00Z"},
		{ID: "other-runner", RecipeKey: "recipe", Runner: "other", Status: StatusReady, UpdatedAt: "2026-01-03T00:00:00Z"},
		{ID: "failed", RecipeKey: "recipe", Runner: "runner", Status: StatusFailed, UpdatedAt: "2026-01-04T00:00:00Z"},
	}}
	slot := inventory.ReadySlot("recipe", "runner")
	if slot == nil || slot.ID != "new" {
		t.Fatalf("ready slot = %+v", slot)
	}
}

func TestInventorySelectsNewestReadySlotAcrossFractionalTimestamps(t *testing.T) {
	inventory := Inventory{Version: InventoryVersion, Slots: []Slot{
		{ID: "old", RecipeKey: "recipe", Runner: "runner", Status: StatusReady, UpdatedAt: "2026-01-01T00:00:00Z"},
		{ID: "new", RecipeKey: "recipe", Runner: "runner", Status: StatusReady, UpdatedAt: "2026-01-01T00:00:00.1Z"},
	}}
	slot := inventory.ReadySlot("recipe", "runner")
	if slot == nil || slot.ID != "new" {
		t.Fatalf("ready slot = %+v", slot)
	}
}

func TestInventoryClaimQuarantineDoesNotBlockOtherClaims(t *testing.T) {
	inventory := Inventory{Version: InventoryVersion, Slots: []Slot{
		{
			ID: testStoreSlotID, RecipeKey: testStoreRecipeKey, Runner: "runner",
			Status: StatusFailed,
		},
		{
			ID: testStoreOtherID, TemplateSlotID: testStoreSlotID,
			RecipeKey: testStoreRecipeKey, Runner: "runner",
			Status: StatusQuarantined, Failure: "claim-local cleanup failed",
		},
	}}
	if blocked := inventory.BlockingSlot(testStoreRecipeKey, "runner"); blocked != nil {
		t.Fatalf("claim-local quarantine blocked unrelated claims: %+v", blocked)
	}
}

func TestInventoryFindsActiveClaim(t *testing.T) {
	inventory := Inventory{Version: InventoryVersion, Slots: []Slot{
		{ID: "ready", Status: StatusReady},
		{ID: "claiming", Status: StatusClaiming, Claim: &Claim{WorkspaceName: "cake"}},
		{ID: "claimed", Status: StatusClaimed, Claim: &Claim{WorkspaceName: "pie"}},
	}}
	if slot := inventory.ClaimForWorkspace("cake"); slot == nil || slot.ID != "claiming" {
		t.Fatalf("claiming slot = %+v", slot)
	}
	if slot := inventory.ClaimForWorkspace("pie"); slot == nil || slot.ID != "claimed" {
		t.Fatalf("claimed slot = %+v", slot)
	}
	if slot := inventory.ClaimForWorkspace("missing"); slot != nil {
		t.Fatalf("unexpected claim = %+v", slot)
	}
}

func TestStoreUpdateSlot(t *testing.T) {
	inventory := Inventory{Version: InventoryVersion, Slots: []Slot{{ID: "slot", Status: StatusBaking, UpdatedAt: "2026-01-01T00:00:00Z"}}}
	if err := inventory.UpdateSlot("slot", func(slot *Slot) error {
		slot.Status = StatusReady
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if inventory.Slots[0].Status != StatusReady {
		t.Fatalf("slot = %+v", inventory.Slots[0])
	}
	if err := inventory.UpdateSlot("missing", func(*Slot) error { return nil }); err == nil {
		t.Fatal("expected missing slot error")
	}
}

func TestStoreRejectsMissingInventoryAndSymlinkRoot(t *testing.T) {
	t.Run("missing inventory", func(t *testing.T) {
		store := NewStore(t.TempDir())
		if err := os.Mkdir(store.Root, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatal("expected existing root without inventory to fail closed")
		}
	})

	t.Run("symlink root", func(t *testing.T) {
		base := t.TempDir()
		target := filepath.Join(base, "target")
		if err := os.Mkdir(target, 0o700); err != nil {
			t.Fatal(err)
		}
		store := NewStore(filepath.Join(base, "grove"))
		if err := os.Mkdir(filepath.Dir(store.Root), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, store.Root); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatal("expected symlink Oven root to be rejected")
		}
		if err := store.Save(NewInventory()); err == nil {
			t.Fatal("expected save through symlink Oven root to be rejected")
		}
	})
}

func TestStoreRejectsPermissiveInventory(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Save(NewInventory()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(store.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("expected permissive inventory to be rejected")
	}
}
