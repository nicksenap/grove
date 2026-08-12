package oven

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type Store struct {
	Root string
	Path string
}

func NewStore(groveDir string) *Store {
	root := filepath.Join(groveDir, "oven")
	return &Store{Root: root, Path: filepath.Join(root, "inventory.json")}
}

func (store *Store) GenerationsPath() string {
	return filepath.Join(store.Root, "generations")
}

func (store *Store) SlotPath(generation, slotID string) string {
	return filepath.Join(store.GenerationsPath(), generation, "slots", slotID)
}

func (store *Store) Load() (Inventory, error) {
	rootExists, err := store.validateRoot()
	if err != nil {
		return Inventory{}, err
	}
	info, err := os.Lstat(store.Path)
	if os.IsNotExist(err) {
		if rootExists {
			return Inventory{}, fmt.Errorf("oven inventory is missing from an existing oven root")
		}
		return NewInventory(), nil
	}
	if err != nil {
		return Inventory{}, fmt.Errorf("reading oven inventory metadata: %w", err)
	}
	if err := validatePrivateFile(info, store.Path); err != nil {
		return Inventory{}, err
	}
	data, err := os.ReadFile(store.Path)
	if err != nil {
		return Inventory{}, fmt.Errorf("reading oven inventory: %w", err)
	}
	var inventory Inventory
	if err := json.Unmarshal(data, &inventory); err != nil {
		return Inventory{}, fmt.Errorf("parsing oven inventory: %w", err)
	}
	if inventory.Version == 1 {
		inventory.Version = InventoryVersion
	}
	if err := store.validate(inventory); err != nil {
		return Inventory{}, err
	}
	return inventory, nil
}

func (store *Store) Save(inventory Inventory) error {
	if err := store.validate(inventory); err != nil {
		return err
	}
	rootExists, err := store.validateRoot()
	if err != nil {
		return err
	}
	if !rootExists {
		if err := os.Mkdir(store.Root, 0o700); err != nil {
			return fmt.Errorf("creating oven directory: %w", err)
		}
	}
	if info, err := os.Lstat(store.Path); err == nil {
		if err := validatePrivateFile(info, store.Path); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(store.Root, "inventory-*.tmp")
	if err != nil {
		return fmt.Errorf("creating oven inventory temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, store.Path); err != nil {
		return fmt.Errorf("publishing oven inventory: %w", err)
	}
	directory, err := os.Open(store.Root)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (store *Store) validateRoot() (bool, error) {
	info, err := os.Lstat(store.Root)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("oven root must be a real directory")
	}
	if info.Mode().Perm() != 0o700 {
		return false, fmt.Errorf("oven root permissions must be 0700")
	}
	if err := validateOwner(info, store.Root); err != nil {
		return false, err
	}
	return true, nil
}

func validatePrivateFile(info os.FileInfo, path string) error {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("oven inventory must be a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("oven inventory permissions must be 0600")
	}
	if err := validateOwner(info, path); err != nil {
		return err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
		return fmt.Errorf("oven inventory must have one hard link")
	}
	return nil
}

func validateOwner(info os.FileInfo, path string) error {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return fmt.Errorf("%s is not owned by the current user", path)
	}
	return nil
}

func (store *Store) validate(inventory Inventory) error {
	if inventory.Version != InventoryVersion {
		return fmt.Errorf("unsupported oven inventory version %d", inventory.Version)
	}
	seen := make(map[string]bool, len(inventory.Slots))
	for _, slot := range inventory.Slots {
		if err := store.validateSlot(slot, seen); err != nil {
			return err
		}
		seen[slot.ID] = true
	}
	return validateTemplateReferences(inventory)
}

func (store *Store) validateSlot(slot Slot, seen map[string]bool) error {
	if !lowerHex(slot.ID, 32) || seen[slot.ID] {
		return fmt.Errorf("invalid or duplicate oven slot ID %q", slot.ID)
	}
	if err := validateTemplateReference(slot); err != nil {
		return err
	}
	if !lowerHex(slot.RecipeKey, 64) || !lowerHex(slot.Generation, 64) || slot.Runner == "" {
		return fmt.Errorf("oven slot %s has invalid identity", slot.ID)
	}
	if !validStatus(slot.Status) {
		return fmt.Errorf("oven slot %s has invalid status %q", slot.ID, slot.Status)
	}
	expected := filepath.Clean(store.SlotPath(slot.Generation, slot.ID))
	if filepath.Clean(slot.BackingPath) != expected || !pathWithin(store.GenerationsPath(), slot.BackingPath) {
		return fmt.Errorf("oven slot %s backing path is outside its generation", slot.ID)
	}
	if slot.RecipePath != "" && (!filepath.IsAbs(slot.RecipePath) || filepath.Clean(slot.RecipePath) != slot.RecipePath) {
		return fmt.Errorf("oven slot %s Recipe path is not absolute and clean", slot.ID)
	}
	if strings.ContainsRune(slot.Failure, '\x00') {
		return fmt.Errorf("oven slot %s failure contains invalid data", slot.ID)
	}
	if err := validateSlotRepositories(slot); err != nil {
		return err
	}
	return validateSlotLifecycle(slot)
}

func validateTemplateReference(slot Slot) error {
	if slot.TemplateSlotID == "" {
		return nil
	}
	if !lowerHex(slot.TemplateSlotID, 32) || slot.TemplateSlotID == slot.ID {
		return fmt.Errorf("oven slot %s has an invalid template slot ID", slot.ID)
	}
	if slot.Status == StatusBaking || slot.Status == StatusReady || slot.Status == StatusFailed {
		return fmt.Errorf("oven claim slot %s has template-only status %s", slot.ID, slot.Status)
	}
	return nil
}

func validateTemplateReferences(inventory Inventory) error {
	byID := make(map[string]Slot, len(inventory.Slots))
	for _, slot := range inventory.Slots {
		byID[slot.ID] = slot
	}
	for _, slot := range inventory.Slots {
		if slot.TemplateSlotID == "" {
			continue
		}
		template, ok := byID[slot.TemplateSlotID]
		if !ok || template.TemplateSlotID != "" {
			return fmt.Errorf("oven claim slot %s references a missing or non-template slot", slot.ID)
		}
		if slot.RecipeKey != template.RecipeKey || slot.Generation != template.Generation || slot.Runner != template.Runner {
			return fmt.Errorf("oven claim slot %s differs from its template identity", slot.ID)
		}
	}
	return nil
}

func validateSlotRepositories(slot Slot) error {
	if len(slot.Repositories) == 0 && slot.Status != StatusQuarantined && slot.Status != StatusCleanupError {
		return fmt.Errorf("oven slot %s has no repositories", slot.ID)
	}
	seen := make(map[string]bool, len(slot.Repositories))
	for _, repository := range slot.Repositories {
		if !safeName(repository.Name) || seen[repository.Name] || repository.Commit == "" {
			return fmt.Errorf("oven slot %s has an invalid repository", slot.ID)
		}
		seen[repository.Name] = true
		if !filepath.IsAbs(repository.SourceRepo) || filepath.Clean(repository.SourceRepo) != repository.SourceRepo ||
			repository.WorktreePath != filepath.Join(slot.BackingPath, repository.Name) {
			return fmt.Errorf("oven repository %s has an unsafe path", repository.Name)
		}
	}
	return nil
}

func validateSlotLifecycle(slot Slot) error {
	activeClaim := slot.Status == StatusClaiming || slot.Status == StatusClaimed || slot.Status == StatusCleanupError
	if activeClaim && slot.Claim == nil || !activeClaim && slot.Claim != nil && slot.Status != StatusQuarantined {
		return fmt.Errorf("oven slot %s claim lifecycle is inconsistent (status %s, claim present %t)", slot.ID, slot.Status, slot.Claim != nil)
	}
	if slot.Claim == nil {
		return nil
	}
	claim := slot.Claim
	if err := validateClaimShape(slot, *claim); err != nil {
		return err
	}
	return validateClaimRepositories(slot, *claim)
}

func validateClaimShape(slot Slot, claim Claim) error {
	if !lowerHex(claim.Nonce, 32) || !safeName(claim.WorkspaceName) || !filepath.IsAbs(claim.Alias) ||
		filepath.Clean(claim.Alias) != claim.Alias || claim.Branch == "" || len(claim.Repositories) != len(slot.Repositories) {
		return fmt.Errorf("oven slot %s has an invalid claim", slot.ID)
	}
	return nil
}

func validateClaimRepositories(slot Slot, claim Claim) error {
	prepared := make(map[string]Repository, len(slot.Repositories))
	for _, repository := range slot.Repositories {
		prepared[repository.Name] = repository
	}
	seen := make(map[string]bool, len(claim.Repositories))
	for _, repository := range claim.Repositories {
		base, ok := prepared[repository.Name]
		if !ok || seen[repository.Name] || repository.SourceRepo != base.SourceRepo ||
			repository.PhysicalPath != base.WorktreePath || repository.AliasPath != filepath.Join(claim.Alias, repository.Name) ||
			repository.Branch != claim.Branch {
			return fmt.Errorf("oven slot %s has an invalid claimed repository", slot.ID)
		}
		seen[repository.Name] = true
	}
	return nil
}

func lowerHex(value string, length int) bool {
	if len(value) != length {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}

func safeName(value string) bool {
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' ||
			char == '.' || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
