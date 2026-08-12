package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/oven"
)

var ErrOvenOwnership = fmt.Errorf("oven ownership verification failed")

// VerifyWorkspaceOwnership checks external Oven ownership when name belongs to a claimed slot.
func (service *Service) VerifyWorkspaceOwnership(name string) error {
	workspace, err := service.State.GetWorkspace(name)
	if err != nil {
		return err
	}
	if workspace == nil {
		return fmt.Errorf("workspace %s not found", name)
	}
	_, err = service.verifyOvenWorkspaceIfClaimed(*workspace)
	return err
}

// OperationWorkspace returns verified physical paths for operations on an Oven claim.
func (service *Service) OperationWorkspace(name string) (*models.Workspace, error) {
	workspace, err := service.State.GetWorkspace(name)
	if err != nil || workspace == nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("workspace %s not found", name)
	}
	slot, err := service.verifyOvenWorkspaceIfClaimed(*workspace)
	if err != nil {
		return nil, err
	}
	if slot != nil {
		applyOvenPhysicalPaths(workspace, *slot)
		workspace.Path = slot.BackingPath
	}
	return workspace, nil
}

func applyOvenPhysicalPaths(workspace *models.Workspace, slot oven.Slot) {
	for index := range workspace.Repos {
		claimed := ovenClaimRepositoryByName(*slot.Claim, workspace.Repos[index].RepoName)
		workspace.Repos[index].WorktreePath = claimed.PhysicalPath
	}
}

func (service *Service) ovenClaimForWorkspace(name string) (*oven.Slot, error) {
	if service.Oven == nil {
		return nil, nil
	}
	inventory, err := service.Oven.Load()
	if err != nil {
		return nil, err
	}
	slot := inventory.ClaimForWorkspace(name)
	if slot == nil {
		return nil, nil
	}
	copy := *slot
	return &copy, nil
}

func (service *Service) verifyOvenWorkspaceIfClaimed(workspace models.Workspace) (*oven.Slot, error) {
	slot, err := service.ovenClaimForWorkspace(workspace.Name)
	if err != nil {
		return nil, err
	}
	if slot == nil {
		if workspace.Oven != nil {
			return nil, fmt.Errorf("%w: workspace %s references missing Oven slot %s", ErrOvenOwnership, workspace.Name, workspace.Oven.SlotID)
		}
		ovenBacked, err := service.looksOvenBacked(workspace)
		if err != nil {
			return nil, err
		}
		if ovenBacked {
			return nil, fmt.Errorf("%w: workspace %s appears Oven-backed but has no claim record", ErrOvenOwnership, workspace.Name)
		}
		return nil, nil
	}
	if err := service.verifyOvenWorkspaceOwnership(workspace, *slot); err != nil {
		return nil, fmt.Errorf("oven ownership mismatch: %w", err)
	}
	return slot, nil
}

func (service *Service) looksOvenBacked(workspace models.Workspace) (bool, error) {
	if service.Oven == nil {
		return false, nil
	}
	if info, err := os.Lstat(workspace.Path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return true, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	ovenRoot := canonicalPath(service.Oven.Root)
	for _, repository := range workspace.Repos {
		if ovenPathWithin(ovenRoot, canonicalPath(repository.WorktreePath)) {
			return true, nil
		}
	}
	return false, nil
}

func ovenPathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (service *Service) verifyOvenWorkspaceOwnership(workspace models.Workspace, slot oven.Slot) error {
	claim := slot.Claim
	if workspace.Oven == nil || workspace.Oven.SlotID != slot.ID || claim == nil || workspace.Oven.ClaimNonce != claim.Nonce {
		return fmt.Errorf("workspace Oven identity differs from its claim record")
	}
	if slot.Status != oven.StatusClaiming && slot.Status != oven.StatusClaimed && slot.Status != oven.StatusCleanupError {
		return fmt.Errorf("slot is not an active claim")
	}
	if claim.WorkspaceName != workspace.Name || claim.Alias != workspace.Path || claim.Branch != workspace.Branch ||
		len(claim.Repositories) != len(workspace.Repos) || len(slot.Repositories) != len(workspace.Repos) {
		return fmt.Errorf("workspace state differs from its claim record")
	}
	if err := service.verifyOvenBackingPath(slot); err != nil {
		return err
	}
	target, err := os.Readlink(claim.Alias)
	if err != nil || canonicalPath(target) != canonicalPath(slot.BackingPath) {
		return fmt.Errorf("workspace alias does not target its claimed backing path")
	}

	prepared, claimed, err := ovenOwnershipMaps(slot, *claim)
	if err != nil {
		return err
	}
	seen := make(map[string]bool, len(workspace.Repos))
	for _, repository := range workspace.Repos {
		if seen[repository.RepoName] {
			return fmt.Errorf("duplicate workspace repository %s", repository.RepoName)
		}
		seen[repository.RepoName] = true
		if err := verifyOvenWorkspaceRepository(repository, slot, *claim, prepared, claimed); err != nil {
			return err
		}
	}
	return nil
}

func ovenOwnershipMaps(slot oven.Slot, claim oven.Claim) (map[string]oven.Repository, map[string]oven.ClaimRepository, error) {
	prepared := make(map[string]oven.Repository, len(slot.Repositories))
	for _, repository := range slot.Repositories {
		if _, duplicate := prepared[repository.Name]; duplicate {
			return nil, nil, fmt.Errorf("duplicate prepared repository %s", repository.Name)
		}
		prepared[repository.Name] = repository
	}
	claimed := make(map[string]oven.ClaimRepository, len(claim.Repositories))
	for _, repository := range claim.Repositories {
		if _, duplicate := claimed[repository.Name]; duplicate {
			return nil, nil, fmt.Errorf("duplicate claimed repository %s", repository.Name)
		}
		claimed[repository.Name] = repository
	}
	return prepared, claimed, nil
}

func verifyOvenWorkspaceRepository(
	repository models.RepoWorktree,
	slot oven.Slot,
	claim oven.Claim,
	prepared map[string]oven.Repository,
	claimed map[string]oven.ClaimRepository,
) error {
	preparedRepo, preparedOK := prepared[repository.RepoName]
	claimedRepo, claimedOK := claimed[repository.RepoName]
	if !preparedOK || !claimedOK || repository.SourceRepo != claimedRepo.SourceRepo ||
		repository.WorktreePath != claimedRepo.AliasPath || repository.Branch != claimedRepo.Branch ||
		repository.PreserveBranch != !claimedRepo.BranchCreated ||
		preparedRepo.SourceRepo != claimedRepo.SourceRepo || preparedRepo.WorktreePath != claimedRepo.PhysicalPath ||
		filepath.Dir(preparedRepo.WorktreePath) != slot.BackingPath || filepath.Dir(claimedRepo.AliasPath) != claim.Alias {
		return fmt.Errorf("repository %s differs from its claim record", repository.RepoName)
	}
	return verifyClaimedOvenRepository(preparedRepo, claimedRepo)
}

func verifyOvenClaimFilesystem(slot oven.Slot, claim oven.Claim, requirePreparedCommit bool) error {
	target, err := os.Readlink(claim.Alias)
	if err != nil || canonicalPath(target) != canonicalPath(slot.BackingPath) {
		return fmt.Errorf("workspace alias does not target its claimed backing path")
	}
	if len(claim.Repositories) != len(slot.Repositories) {
		return fmt.Errorf("claim repository set changed")
	}
	for _, claimed := range claim.Repositories {
		prepared := ovenRepositoryByName(slot, claimed.Name)
		if prepared.Name == "" || claimed.SourceRepo != prepared.SourceRepo || claimed.PhysicalPath != prepared.WorktreePath ||
			claimed.AliasPath != filepath.Join(claim.Alias, claimed.Name) || claimed.Branch != claim.Branch {
			return fmt.Errorf("%s: claim repository identity changed", claimed.Name)
		}
		if err := verifyClaimedOvenRepository(prepared, claimed); err != nil {
			return err
		}
		if requirePreparedCommit {
			head, err := gitops.HeadCommit(claimed.PhysicalPath)
			if err != nil || head != prepared.Commit {
				return fmt.Errorf("%s: claimed commit changed", claimed.Name)
			}
		}
	}
	return nil
}

func (service *Service) verifyOvenBackingPath(slot oven.Slot) error {
	expected := service.Oven.SlotPath(slot.Generation, slot.ID)
	if filepath.Clean(slot.BackingPath) != filepath.Clean(expected) {
		return fmt.Errorf("backing path is outside its generation")
	}
	relative, err := filepath.Rel(service.Oven.Root, slot.BackingPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("backing path escapes the Oven root")
	}
	current := service.Oven.Root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backing path is missing or contains a symlink")
		}
	}
	return nil
}

func (service *Service) beginOvenCleanup(slotID string, force bool) (*oven.Slot, error) {
	inventory, err := service.Oven.Load()
	if err != nil {
		return nil, err
	}
	slot := inventory.FindSlot(slotID)
	if slot == nil || slot.Claim == nil || (slot.Status != oven.StatusClaimed && slot.Status != oven.StatusCleanupError) {
		return nil, fmt.Errorf("oven claim changed before cleanup")
	}
	if slot.Status == oven.StatusClaimed {
		for index := range slot.Claim.Repositories {
			repository := &slot.Claim.Repositories[index]
			commit, err := gitops.LocalBranchCommit(repository.SourceRepo, repository.Branch)
			if err != nil {
				return nil, fmt.Errorf("%s: reading cleanup branch: %w", repository.Name, err)
			}
			repository.ExpectedBranchCommit = commit
		}
		slot.Status = oven.StatusCleanupError
		slot.Claim.CleanupForce = force
		slot.Failure = "cleanup in progress"
		if err := service.Oven.Save(inventory); err != nil {
			return nil, err
		}
	}
	copy := *slot
	return &copy, nil
}

func (service *Service) updateOvenCleanupProgress(slotID, nonce string, remaining []models.RepoWorktree) (*oven.Slot, error) {
	inventory, err := service.Oven.Load()
	if err != nil {
		return nil, err
	}
	slot := inventory.FindSlot(slotID)
	if slot == nil || slot.Status != oven.StatusCleanupError || slot.Claim == nil || slot.Claim.Nonce != nonce {
		return nil, fmt.Errorf("oven cleanup identity changed")
	}
	keep := make(map[string]bool, len(remaining))
	for _, repository := range remaining {
		keep[repository.RepoName] = true
	}
	prepared := slot.Repositories[:0]
	for _, repository := range slot.Repositories {
		if keep[repository.Name] {
			prepared = append(prepared, repository)
		}
	}
	claimed := slot.Claim.Repositories[:0]
	for _, repository := range slot.Claim.Repositories {
		if keep[repository.Name] {
			claimed = append(claimed, repository)
		}
	}
	slot.Repositories = prepared
	slot.Claim.Repositories = claimed
	slot.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := service.Oven.Save(inventory); err != nil {
		return nil, err
	}
	copy := *slot
	return &copy, nil
}

func ovenClaimRepositoryByName(claim oven.Claim, name string) oven.ClaimRepository {
	for _, repository := range claim.Repositories {
		if repository.Name == name {
			return repository
		}
	}
	return oven.ClaimRepository{}
}

func (service *Service) finalizeDeletedOvenClaim(expected oven.Slot) error {
	inventory, err := service.Oven.Load()
	if err != nil {
		return err
	}
	slot := inventory.FindSlot(expected.ID)
	if slot == nil || slot.Claim == nil || expected.Claim == nil || slot.Claim.Nonce != expected.Claim.Nonce {
		return fmt.Errorf("oven claim changed before backing cleanup")
	}
	if err := service.removeOwnedOvenClaimRoot(*slot); err != nil {
		slot.Status = oven.StatusCleanupError
		slot.Failure = safeOvenFailure(err)
		if saveErr := service.Oven.Save(inventory); saveErr != nil {
			return fmt.Errorf("%w; recording cleanup failure: %v", err, saveErr)
		}
		return err
	}
	inventory.RemoveSlot(slot.ID)
	return service.Oven.Save(inventory)
}

func (service *Service) removeOwnedOvenClaimRoot(slot oven.Slot) error {
	if slot.Claim == nil || slot.Claim.Alias == "" || filepath.Dir(slot.BackingPath) == "." {
		return fmt.Errorf("removing claimed backing root: claim identity is incomplete")
	}
	if err := service.verifyOvenBackingPath(slot); err != nil {
		return fmt.Errorf("removing claimed backing root: %w", err)
	}
	for _, repository := range slot.Repositories {
		if _, err := os.Lstat(repository.WorktreePath); !os.IsNotExist(err) {
			return fmt.Errorf("removing claimed backing root: repository path %s remains", repository.Name)
		}
	}
	if err := os.RemoveAll(slot.BackingPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing claimed backing root: %w", err)
	}
	return nil
}

func verifyClaimedOvenRepository(prepared oven.Repository, claimed oven.ClaimRepository) error {
	if filepath.Dir(prepared.WorktreePath) == "." || filepath.Base(prepared.WorktreePath) != prepared.Name ||
		claimed.PhysicalPath != prepared.WorktreePath || filepath.Base(claimed.AliasPath) != prepared.Name {
		return fmt.Errorf("%s: claimed repository path changed", prepared.Name)
	}
	entries, err := gitops.WorktreeList(prepared.SourceRepo)
	if err != nil {
		return err
	}
	registered := false
	for _, entry := range entries {
		if canonicalPath(entry.Path) == canonicalPath(claimed.PhysicalPath) && entry.Branch == claimed.Branch {
			registered = true
			break
		}
	}
	if !registered {
		return fmt.Errorf("%s: claimed worktree registration changed", prepared.Name)
	}
	branch, err := gitops.CurrentBranch(claimed.AliasPath)
	if err != nil || branch != claimed.Branch {
		return fmt.Errorf("%s: claimed branch changed", prepared.Name)
	}
	return nil
}
