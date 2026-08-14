package oven

import (
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
