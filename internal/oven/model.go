package oven

import (
	"fmt"
	"sort"
)

const InventoryVersion = 1

type SlotStatus string

const (
	StatusBaking       SlotStatus = "baking"
	StatusReady        SlotStatus = "ready"
	StatusClaiming     SlotStatus = "claiming"
	StatusClaimed      SlotStatus = "claimed"
	StatusFailed       SlotStatus = "failed"
	StatusQuarantined  SlotStatus = "quarantined"
	StatusCleanupError SlotStatus = "cleanup_failed"
)

type Inventory struct {
	Version int    `json:"version"`
	Slots   []Slot `json:"slots"`
}

type Slot struct {
	ID           string       `json:"id"`
	RecipeKey    string       `json:"recipe_key"`
	RecipeName   string       `json:"recipe_name,omitempty"`
	RecipePath   string       `json:"recipe_path,omitempty"`
	Generation   string       `json:"generation"`
	Runner       string       `json:"runner"`
	BackingPath  string       `json:"backing_path"`
	Status       SlotStatus   `json:"status"`
	OwnerPID     int          `json:"owner_pid,omitempty"`
	CreatedAt    string       `json:"created_at"`
	UpdatedAt    string       `json:"updated_at"`
	Failure      string       `json:"failure,omitempty"`
	Repositories []Repository `json:"repositories"`
	Claim        *Claim       `json:"claim,omitempty"`
}

type Repository struct {
	Name         string `json:"name"`
	SourceRepo   string `json:"source_repo"`
	WorktreePath string `json:"worktree_path"`
	Commit       string `json:"commit"`
}

type Claim struct {
	Nonce         string            `json:"nonce"`
	WorkspaceName string            `json:"workspace_name"`
	Alias         string            `json:"alias"`
	Branch        string            `json:"branch"`
	StartedAt     string            `json:"started_at"`
	CleanupForce  bool              `json:"cleanup_force,omitempty"`
	Repositories  []ClaimRepository `json:"repositories"`
}

type ClaimRepository struct {
	Name                 string `json:"name"`
	SourceRepo           string `json:"source_repo"`
	PhysicalPath         string `json:"physical_path"`
	AliasPath            string `json:"alias_path"`
	Branch               string `json:"branch"`
	BranchCreated        bool   `json:"branch_created"`
	ExpectedBranchCommit string `json:"expected_branch_commit,omitempty"`
}

func NewInventory() Inventory {
	return Inventory{Version: InventoryVersion, Slots: []Slot{}}
}

func (inventory *Inventory) ReadySlot(recipeKey, runner string) *Slot {
	var matches []*Slot
	for index := range inventory.Slots {
		slot := &inventory.Slots[index]
		if slot.RecipeKey == recipeKey && slot.Runner == runner && slot.Status == StatusReady {
			matches = append(matches, slot)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].UpdatedAt == matches[j].UpdatedAt {
			return matches[i].ID > matches[j].ID
		}
		return matches[i].UpdatedAt > matches[j].UpdatedAt
	})
	if len(matches) == 0 {
		return nil
	}
	return matches[0]
}

func (inventory *Inventory) ReadyGeneration(recipeKey, generation, runner string) *Slot {
	for index := range inventory.Slots {
		slot := &inventory.Slots[index]
		if slot.RecipeKey == recipeKey && slot.Generation == generation && slot.Runner == runner && slot.Status == StatusReady {
			return slot
		}
	}
	return nil
}

func (inventory *Inventory) BlockingSlot(recipeKey, runner string) *Slot {
	for index := range inventory.Slots {
		slot := &inventory.Slots[index]
		if slot.RecipeKey == recipeKey && slot.Runner == runner && slot.Status == StatusQuarantined {
			return slot
		}
	}
	return nil
}

func (inventory *Inventory) ClaimForWorkspace(name string) *Slot {
	for index := range inventory.Slots {
		slot := &inventory.Slots[index]
		if (slot.Status == StatusClaiming || slot.Status == StatusClaimed || slot.Status == StatusCleanupError) &&
			slot.Claim != nil && slot.Claim.WorkspaceName == name {
			return slot
		}
	}
	return nil
}

func (inventory *Inventory) FindSlot(id string) *Slot {
	for index := range inventory.Slots {
		if inventory.Slots[index].ID == id {
			return &inventory.Slots[index]
		}
	}
	return nil
}

func (inventory *Inventory) UpdateSlot(id string, update func(*Slot) error) error {
	slot := inventory.FindSlot(id)
	if slot == nil {
		return fmt.Errorf("oven slot %s not found", id)
	}
	return update(slot)
}

func (inventory *Inventory) RemoveSlot(id string) bool {
	for index := range inventory.Slots {
		if inventory.Slots[index].ID == id {
			inventory.Slots = append(inventory.Slots[:index], inventory.Slots[index+1:]...)
			return true
		}
	}
	return false
}

func validStatus(status SlotStatus) bool {
	switch status {
	case StatusBaking, StatusReady, StatusClaiming, StatusClaimed, StatusFailed, StatusQuarantined, StatusCleanupError:
		return true
	default:
		return false
	}
}
