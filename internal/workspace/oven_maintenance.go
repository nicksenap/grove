package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/logging"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/oven"
)

type OvenCleanResult struct {
	Removed int      `json:"removed"`
	Blocked []string `json:"blocked"`
}

// RecoverOven resolves interrupted lifecycle records without making a slot
// ready unless readiness was durably recorded.
func (service *Service) RecoverOven() error {
	if service.Oven == nil {
		return nil
	}
	return service.State.WithLock(func() error {
		inventory, err := service.Oven.Load()
		if err != nil {
			return err
		}
		changed := false
		var removeSlots []string
		for index := range inventory.Slots {
			slot := &inventory.Slots[index]
			switch slot.Status {
			case oven.StatusBaking:
				if processIsAlive(slot.OwnerPID) {
					continue
				}
				if err := service.recoverInterruptedBake(slot); err != nil {
					slot.Status = oven.StatusQuarantined
					slot.Failure = safeOvenFailure(err)
				} else {
					slot.Status = oven.StatusFailed
					slot.Failure = "interrupted bake cleaned before readiness"
				}
				slot.OwnerPID = 0
				slot.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
				changed = true
			case oven.StatusClaiming:
				claimChanged, err := service.recoverInterruptedClaim(slot)
				if err != nil {
					slot.Status = oven.StatusQuarantined
					slot.Failure = safeOvenFailure(err)
					changed = true
				} else if claimChanged {
					changed = true
				}
			case oven.StatusClaimed, oven.StatusCleanupError:
				remove, err := service.recoverDeletedClaim(slot)
				if err != nil {
					if slot.Status == oven.StatusClaimed {
						slot.Status = oven.StatusQuarantined
					}
					slot.Failure = safeOvenFailure(err)
					changed = true
				} else if remove {
					removeSlots = append(removeSlots, slot.ID)
					changed = true
				}
			}
		}
		for _, id := range removeSlots {
			inventory.RemoveSlot(id)
		}
		if !changed {
			return nil
		}
		return service.Oven.Save(inventory)
	})
}

func processIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func (service *Service) recoverInterruptedBake(slot *oven.Slot) error {
	created, err := inspectInterruptedBake(*slot)
	if err != nil {
		return err
	}
	for index := len(created) - 1; index >= 0; index-- {
		repository := created[index]
		if err := service.removeWorktree(repository.SourceRepo, repository.WorktreePath, true); err != nil {
			return fmt.Errorf("%s: removing interrupted bake worktree: %w", repository.Name, err)
		}
	}
	if err := os.Remove(slot.BackingPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing interrupted bake root: %w", err)
	}
	return nil
}

func inspectInterruptedBake(slot oven.Slot) ([]oven.Repository, error) {
	var created []oven.Repository
	for _, repository := range slot.Repositories {
		entries, err := gitops.WorktreeList(repository.SourceRepo)
		if err != nil {
			return nil, err
		}
		registered := false
		for _, entry := range entries {
			if canonicalPath(entry.Path) == canonicalPath(repository.WorktreePath) {
				if entry.Branch != "" {
					return nil, fmt.Errorf("%s: interrupted bake worktree is no longer detached", repository.Name)
				}
				registered = true
				break
			}
		}
		_, statErr := os.Lstat(repository.WorktreePath)
		if !registered && os.IsNotExist(statErr) {
			continue
		}
		if !registered || statErr != nil {
			return nil, fmt.Errorf("%s: interrupted bake path and Git registration disagree", repository.Name)
		}
		head, err := gitops.HeadCommit(repository.WorktreePath)
		if err != nil || head != repository.Commit {
			return nil, fmt.Errorf("%s: interrupted bake commit changed", repository.Name)
		}
		created = append(created, repository)
	}
	return created, nil
}

func (service *Service) recoverOvenCleanup(slot *oven.Slot, workspace *models.Workspace) (bool, error) {
	workspaceByName, err := validateCleanupWorkspace(*slot.Claim, *workspace)
	if err != nil {
		return false, err
	}
	remainingNames, cleanupErrs := service.resumeOvenRepositoryCleanup(*slot, workspaceByName)

	keep := make(map[string]bool, len(remainingNames))
	for _, name := range remainingNames {
		keep[name] = true
	}
	slot.Repositories = filterOvenRepositories(slot.Repositories, keep)
	slot.Claim.Repositories = filterClaimRepositories(slot.Claim.Repositories, keep)
	remainingState := workspace.Repos[:0]
	for _, repository := range workspace.Repos {
		if keep[repository.RepoName] {
			remainingState = append(remainingState, repository)
		}
	}
	workspace.Repos = remainingState
	if err := service.State.UpdateWorkspace(*workspace); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}
	if len(remainingNames) > 0 || len(cleanupErrs) > 0 {
		return false, errors.Join(cleanupErrs...)
	}

	removeMCPConfig(*workspace)
	aliasExists := false
	target, err := os.Readlink(slot.Claim.Alias)
	if err == nil {
		if canonicalPath(target) != canonicalPath(slot.BackingPath) {
			return false, fmt.Errorf("cleanup alias ownership changed")
		}
		aliasExists = true
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("cleanup alias ownership changed")
	}
	if aliasExists {
		if err := os.Remove(slot.Claim.Alias); err != nil {
			return false, err
		}
	}
	if err := service.removeWorkspaceState(workspace.Name); err != nil {
		return false, err
	}
	if err := os.Remove(slot.BackingPath); err != nil && !os.IsNotExist(err) {
		return false, err
	}
	return true, nil
}

func validateCleanupWorkspace(claim oven.Claim, workspace models.Workspace) (map[string]models.RepoWorktree, error) {
	claimByName := make(map[string]bool, len(claim.Repositories))
	for _, repository := range claim.Repositories {
		claimByName[repository.Name] = true
	}
	workspaceByName := make(map[string]models.RepoWorktree, len(workspace.Repos))
	for _, repository := range workspace.Repos {
		workspaceByName[repository.RepoName] = repository
		if !claimByName[repository.RepoName] {
			if _, err := os.Lstat(repository.WorktreePath); !os.IsNotExist(err) {
				return nil, fmt.Errorf("%s: workspace cleanup state has an unclaimed repository", repository.RepoName)
			}
		}
	}
	return workspaceByName, nil
}

func (service *Service) resumeOvenRepositoryCleanup(slot oven.Slot, workspaceByName map[string]models.RepoWorktree) ([]string, []error) {
	var remaining []string
	var cleanupErrs []error
	for _, claimed := range slot.Claim.Repositories {
		if err := service.resumeOvenRepository(slot, claimed, workspaceByName[claimed.Name]); err != nil {
			remaining = append(remaining, claimed.Name)
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	return remaining, cleanupErrs
}

func (service *Service) resumeOvenRepository(slot oven.Slot, claimed oven.ClaimRepository, stateRepository models.RepoWorktree) error {
	_, pathErr := os.Lstat(claimed.PhysicalPath)
	if pathErr == nil {
		if stateRepository.RepoName == "" {
			return fmt.Errorf("%s: claimed worktree remains without workspace repository state", claimed.Name)
		}
		stateRepository.WorktreePath = claimed.PhysicalPath
		if err := preflightRemoval(stateRepository); err != nil && !slot.Claim.CleanupForce {
			return err
		}
		if err := service.removeWorktree(claimed.SourceRepo, claimed.PhysicalPath, slot.Claim.CleanupForce); err != nil {
			return err
		}
	} else if !os.IsNotExist(pathErr) {
		return pathErr
	} else if ovenWorktreeRegistered(claimed.SourceRepo, claimed.PhysicalPath) {
		return fmt.Errorf("%s: missing claimed path remains registered", claimed.Name)
	}
	return service.finishOvenCleanupBranch(claimed, slot.Claim.CleanupForce)
}

func (service *Service) removeWorkspaceState(name string) error {
	if service.RemoveWorkspace != nil {
		return service.RemoveWorkspace(name)
	}
	return service.State.RemoveWorkspace(name)
}

func (service *Service) finishOvenCleanupBranch(claimed oven.ClaimRepository, force bool) error {
	if !claimed.BranchCreated || claimed.ExpectedBranchCommit == "" || !gitops.BranchExists(claimed.SourceRepo, claimed.Branch) {
		return nil
	}
	current, err := gitops.LocalBranchCommit(claimed.SourceRepo, claimed.Branch)
	if err != nil || current != claimed.ExpectedBranchCommit {
		return nil
	}
	if err := gitops.DeleteBranchIfAt(claimed.SourceRepo, claimed.Branch, claimed.ExpectedBranchCommit, force); err != nil {
		logging.Warn("failed to finish Oven branch cleanup for %s: %s", claimed.Name, err)
		return err
	}
	return nil
}

func ovenWorktreeRegistered(sourceRepo, path string) bool {
	entries, err := gitops.WorktreeList(sourceRepo)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if canonicalPath(entry.Path) == canonicalPath(path) {
			return true
		}
	}
	return false
}

func filterOvenRepositories(repositories []oven.Repository, keep map[string]bool) []oven.Repository {
	filtered := repositories[:0]
	for _, repository := range repositories {
		if keep[repository.Name] {
			filtered = append(filtered, repository)
		}
	}
	return filtered
}

func filterClaimRepositories(repositories []oven.ClaimRepository, keep map[string]bool) []oven.ClaimRepository {
	filtered := repositories[:0]
	for _, repository := range repositories {
		if keep[repository.Name] {
			filtered = append(filtered, repository)
		}
	}
	return filtered
}

func (service *Service) recoverDeletedClaim(slot *oven.Slot) (bool, error) {
	if slot.Claim == nil {
		return false, fmt.Errorf("claimed slot has no claim record")
	}
	workspace, err := service.State.GetWorkspace(slot.Claim.WorkspaceName)
	if err != nil {
		return false, err
	}
	if workspace != nil {
		if slot.Status == oven.StatusCleanupError {
			return service.recoverOvenCleanup(slot, workspace)
		}
		return false, service.verifyOvenWorkspaceOwnership(*workspace, *slot)
	}
	if _, err := os.Lstat(slot.Claim.Alias); !os.IsNotExist(err) {
		return false, fmt.Errorf("claimed alias remains without workspace state")
	}
	for _, repository := range slot.Repositories {
		entries, err := gitops.WorktreeList(repository.SourceRepo)
		if err != nil {
			return false, err
		}
		for _, entry := range entries {
			if canonicalPath(entry.Path) == canonicalPath(repository.WorktreePath) {
				return false, fmt.Errorf("%s: claimed worktree remains without workspace state", repository.Name)
			}
		}
		if _, err := os.Lstat(repository.WorktreePath); !os.IsNotExist(err) {
			return false, fmt.Errorf("%s: claimed worktree path remains without workspace state", repository.Name)
		}
	}
	if err := os.Remove(slot.BackingPath); err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("removing orphaned claimed backing root: %w", err)
	}
	return true, nil
}

func (service *Service) recoverInterruptedClaim(slot *oven.Slot) (bool, error) {
	if slot.Claim == nil {
		return false, fmt.Errorf("claiming slot has no claim record")
	}
	workspace, err := service.State.GetWorkspace(slot.Claim.WorkspaceName)
	if err != nil {
		return false, err
	}
	if workspace != nil {
		if err := service.verifyOvenWorkspaceOwnership(*workspace, *slot); err != nil {
			return false, err
		}
		slot.Status = oven.StatusClaimed
		slot.Failure = ""
		slot.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		return true, nil
	}
	if err := service.rollbackInterruptedClaim(*slot); err != nil {
		return false, err
	}
	slot.Status = oven.StatusReady
	slot.Claim = nil
	slot.Failure = ""
	slot.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return true, nil
}

func (service *Service) rollbackInterruptedClaim(slot oven.Slot) error {
	if err := service.verifyOvenBackingPath(slot); err != nil {
		return err
	}
	aliasExists := false
	if target, err := os.Readlink(slot.Claim.Alias); err == nil {
		if canonicalPath(target) != canonicalPath(slot.BackingPath) {
			return fmt.Errorf("interrupted claim alias target changed")
		}
		aliasExists = true
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("interrupted claim alias is not owned: %w", err)
	}

	assigned := make([]oven.ClaimRepository, 0, len(slot.Claim.Repositories))
	for _, claimed := range slot.Claim.Repositories {
		prepared := ovenRepositoryByName(slot, claimed.Name)
		branch, err := gitops.CurrentBranch(claimed.PhysicalPath)
		if err != nil {
			return fmt.Errorf("%s: reading interrupted claim branch: %w", claimed.Name, err)
		}
		head, err := gitops.HeadCommit(claimed.PhysicalPath)
		if err != nil || head != prepared.Commit {
			return fmt.Errorf("%s: interrupted claim commit changed", claimed.Name)
		}
		status, err := gitops.TrackedStatus(claimed.PhysicalPath)
		if err != nil || status != "" {
			return fmt.Errorf("%s: interrupted claim has tracked changes", claimed.Name)
		}
		switch branch {
		case "":
			continue
		case claimed.Branch:
			assigned = append(assigned, claimed)
		default:
			return fmt.Errorf("%s: interrupted claim branch changed", claimed.Name)
		}
	}

	if aliasExists {
		if err := os.Remove(slot.Claim.Alias); err != nil {
			return err
		}
	}
	for index := len(assigned) - 1; index >= 0; index-- {
		claimed := assigned[index]
		commit := ovenRepositoryByName(slot, claimed.Name).Commit
		if err := gitops.DetachWorktree(claimed.PhysicalPath, commit); err != nil {
			return fmt.Errorf("%s: detaching interrupted claim: %w", claimed.Name, err)
		}
		if claimed.BranchCreated {
			if err := gitops.DeleteBranchIfAt(claimed.SourceRepo, claimed.Branch, commit, true); err != nil {
				return fmt.Errorf("%s: deleting interrupted claim branch: %w", claimed.Name, err)
			}
		}
	}
	if err := verifyDetachedOvenRepositories(slot, slot.Repositories); err != nil {
		return fmt.Errorf("interrupted claim recovery did not restore readiness: %w", err)
	}
	return nil
}

// PruneOvenRecipe removes stale ready and failed slots only after a replacement
// generation is ready. Claimed, active, and quarantined slots are retained.
func (service *Service) PruneOvenRecipe(recipePath, _ string, runner, keepSlotID string) (OvenCleanResult, error) {
	return service.cleanOvenSlots(func(slot oven.Slot) bool {
		return slot.RecipePath == recipePath && slot.Runner == runner && slot.ID != keepSlotID &&
			(slot.Status == oven.StatusReady || slot.Status == oven.StatusFailed)
	})
}

// CleanOven removes unclaimed ready and failed slots, optionally restricted to
// one canonical Recipe path. Unsafe lifecycle states remain visible as blocked.
func (service *Service) CleanOven(recipePath string) (OvenCleanResult, error) {
	return service.cleanOvenSlots(func(slot oven.Slot) bool {
		return recipePath == "" || slot.RecipePath == recipePath
	})
}

func (service *Service) cleanOvenSlots(selectSlot func(oven.Slot) bool) (OvenCleanResult, error) {
	result := OvenCleanResult{Blocked: []string{}}
	if service.Oven == nil {
		return result, nil
	}
	err := service.State.WithLock(func() error {
		inventory, err := service.Oven.Load()
		if err != nil {
			return err
		}
		var cleanupErrs []error
		ids := make([]string, 0, len(inventory.Slots))
		for _, slot := range inventory.Slots {
			if selectSlot(slot) {
				ids = append(ids, slot.ID)
			}
		}
		for _, id := range ids {
			slot := inventory.FindSlot(id)
			if slot == nil {
				continue
			}
			switch slot.Status {
			case oven.StatusReady:
				if err := removeReadyOvenSlot(service, *slot); err != nil {
					slot.Status = oven.StatusQuarantined
					slot.Failure = safeOvenFailure(err)
					result.Blocked = append(result.Blocked, slot.ID)
					cleanupErrs = append(cleanupErrs, err)
					continue
				}
				inventory.RemoveSlot(id)
				result.Removed++
			case oven.StatusFailed:
				if err := removeFailedOvenSlot(*slot); err != nil {
					result.Blocked = append(result.Blocked, slot.ID)
					cleanupErrs = append(cleanupErrs, err)
					continue
				}
				inventory.RemoveSlot(id)
				result.Removed++
			default:
				result.Blocked = append(result.Blocked, slot.ID)
			}
		}
		if err := service.Oven.Save(inventory); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
		return errors.Join(cleanupErrs...)
	})
	return result, err
}

func removeReadyOvenSlot(service *Service, slot oven.Slot) error {
	if err := verifyDetachedOvenRepositories(slot, slot.Repositories); err != nil {
		return err
	}
	for index := len(slot.Repositories) - 1; index >= 0; index-- {
		repository := slot.Repositories[index]
		if err := service.removeWorktree(repository.SourceRepo, repository.WorktreePath, true); err != nil {
			return err
		}
	}
	if err := os.Remove(slot.BackingPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(filepath.Dir(slot.BackingPath))
	_ = os.Remove(filepath.Dir(filepath.Dir(slot.BackingPath)))
	return nil
}

func removeFailedOvenSlot(slot oven.Slot) error {
	if _, err := os.Lstat(slot.BackingPath); err == nil {
		return fmt.Errorf("failed slot still has a backing path")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
