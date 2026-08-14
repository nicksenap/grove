package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/oven"
)

var (
	ErrOvenMiss    = errors.New("no ready Oven slot")
	ErrOvenBlocked = errors.New("oven claim blocked by quarantined state")
)

type OvenClaimOptions struct {
	RecipeKey string
	Runner    string
	Name      string
	Branch    string
	Config    *models.Config
	Source    *models.WorkspaceSource

	nonce         string
	now           func() time.Time
	attachBranch  func(oven.ClaimRepository, string) error
	clonePrepared func(string, string) error
	publishAlias  func(string, string) error
	saveWorkspace func(models.Workspace) error
}

type OvenClaimResult struct {
	Workspace models.Workspace
	SlotID    string
	Warning   error
}

func (service *Service) ClaimOvenSlot(options OvenClaimOptions) (*OvenClaimResult, error) {
	if service.Oven == nil {
		return nil, ErrOvenMiss
	}
	if options.Config == nil || options.RecipeKey == "" || options.Runner == "" || options.Name == "" || options.Branch == "" {
		return nil, fmt.Errorf("oven claim requires Recipe identity, runner, workspace name, branch, and config")
	}
	if options.Name == "." || options.Name == ".." || filepath.Base(options.Name) != options.Name {
		return nil, fmt.Errorf("invalid Oven workspace name %q", options.Name)
	}

	var result *OvenClaimResult
	err := service.State.WithLock(func() error {
		var err error
		result, err = service.claimOvenSlotLocked(options)
		return err
	})
	if err != nil {
		return nil, err
	}
	service.finishCreate(result.Workspace)
	return result, nil
}

func (service *Service) claimOvenSlotLocked(options OvenClaimOptions) (*OvenClaimResult, error) {
	if err := service.preflightOvenClaimDestination(options); err != nil {
		return nil, err
	}
	inventory, err := service.Oven.Load()
	if err != nil {
		return nil, err
	}
	template := inventory.ReadySlot(options.RecipeKey, options.Runner)
	if template == nil {
		if blocked := inventory.BlockingSlot(options.RecipeKey, options.Runner); blocked != nil {
			return nil, fmt.Errorf("%w: slot %s: %s", ErrOvenBlocked, blocked.ID, blocked.Failure)
		}
		return nil, ErrOvenMiss
	}
	if err := service.preflightReadyOvenTemplate(options, *template); err != nil {
		template.Status = oven.StatusQuarantined
		template.Failure = safeOvenFailure(err)
		template.UpdatedAt = ovenClaimTimestamp(options)
		if saveErr := service.Oven.Save(inventory); saveErr != nil {
			return nil, errors.Join(err, saveErr)
		}
		return nil, fmt.Errorf("%w: ready slot %s was quarantined: %v", ErrOvenBlocked, template.ID, err)
	}
	if err := service.preflightOvenClaimRepositories(options, *template); err != nil {
		return nil, err
	}
	claimSlot, err := service.materializeOvenClaimLocked(options, &inventory, *template)
	if err != nil {
		return nil, err
	}
	return service.executeOvenClaimLocked(options, inventory, claimSlot)
}

func (service *Service) executeOvenClaimLocked(options OvenClaimOptions, inventory oven.Inventory, slot *oven.Slot) (*OvenClaimResult, error) {
	claim := *slot.Claim

	workspace, assigned, err := service.attachOvenClaimRepositories(options, *slot, claim)
	if err != nil {
		return nil, service.rollbackOvenClaim(slot.ID, claim.Nonce, assigned, false, err)
	}
	if err := service.publishOvenClaimAlias(options, *slot, claim); err != nil {
		return nil, service.rollbackOvenClaim(slot.ID, claim.Nonce, assigned, false, err)
	}
	if err := verifyOvenClaimFilesystem(*slot, claim, true); err != nil {
		return nil, service.rollbackOvenClaim(slot.ID, claim.Nonce, assigned, true, err)
	}

	result, err := service.saveClaimedOvenWorkspace(options, workspace, slot.ID)
	if err != nil {
		return nil, service.rollbackOvenClaim(slot.ID, claim.Nonce, assigned, true, err)
	}
	writeMCPConfig(workspace)
	slot.Status = oven.StatusClaimed
	slot.UpdatedAt = ovenClaimTimestamp(options)
	if err := service.Oven.Save(inventory); err != nil {
		result.Warning = errors.Join(result.Warning, fmt.Errorf("finalizing oven claim inventory: %w", err))
	}
	return result, nil
}

func (service *Service) materializeOvenClaimLocked(options OvenClaimOptions, inventory *oven.Inventory, template oven.Slot) (*oven.Slot, error) {
	id, err := randomOvenID()
	if err != nil {
		return nil, err
	}
	backingPath := service.Oven.SlotPath(template.Generation, id)
	claimSlot := oven.Slot{
		ID: id, TemplateSlotID: template.ID, RecipeKey: template.RecipeKey, RecipeName: template.RecipeName,
		RecipePath: template.RecipePath, Generation: template.Generation, Runner: template.Runner,
		BackingPath: backingPath, Status: oven.StatusClaiming,
		CreatedAt: ovenClaimTimestamp(options), UpdatedAt: ovenClaimTimestamp(options),
	}
	for _, repository := range template.Repositories {
		claimSlot.Repositories = append(claimSlot.Repositories, oven.Repository{
			Name: repository.Name, SourceRepo: repository.SourceRepo,
			WorktreePath: filepath.Join(backingPath, repository.Name), Commit: repository.Commit,
		})
	}
	claim, err := service.newOvenClaim(options, claimSlot)
	if err != nil {
		return nil, err
	}
	claimSlot.Claim = &claim
	inventory.Slots = append(inventory.Slots, claimSlot)
	if err := service.Oven.Save(*inventory); err != nil {
		return nil, err
	}
	if err := createOvenBackingDirectory(service.Oven, claimSlot); err != nil {
		return nil, service.failOvenMaterialization(claimSlot.ID, err)
	}
	created := make([]oven.Repository, 0, len(claimSlot.Repositories))
	clonePrepared := options.clonePrepared
	if clonePrepared == nil {
		clonePrepared = clonePreparedWorktree
	}
	for _, repository := range claimSlot.Repositories {
		templateRepository := ovenRepositoryByName(template, repository.Name)
		if templateRepository.Name == "" {
			return nil, service.failOvenMaterialization(claimSlot.ID, fmt.Errorf("%s: reusable template repository is missing", repository.Name))
		}
		if err := gitops.WorktreeAddDetached(repository.SourceRepo, repository.WorktreePath, repository.Commit); err != nil {
			return nil, service.failOvenMaterialization(claimSlot.ID, fmt.Errorf("%s: creating claim worktree: %w", repository.Name, err))
		}
		created = append(created, repository)
		if err := clonePrepared(templateRepository.WorktreePath, repository.WorktreePath); err != nil {
			return nil, service.failOvenMaterialization(claimSlot.ID, fmt.Errorf("%s: materializing prepared worktree: %w", repository.Name, err))
		}
	}
	if err := errors.Join(
		service.verifyOvenBackingPath(claimSlot),
		verifyDetachedOvenRepositories(claimSlot, created),
		verifyNoOvenCredentialResidue(claimSlot),
	); err != nil {
		return nil, service.failOvenMaterialization(claimSlot.ID, err)
	}
	return inventory.FindSlot(claimSlot.ID), nil
}

func (service *Service) failOvenMaterialization(slotID string, cause error) error {
	inventory, loadErr := service.Oven.Load()
	if loadErr != nil {
		return errors.Join(cause, loadErr)
	}
	slot := inventory.FindSlot(slotID)
	if slot == nil || slot.Status != oven.StatusClaiming || slot.Claim == nil {
		return errors.Join(cause, fmt.Errorf("oven materialization identity changed; preserving artifacts"))
	}
	var cleanupErrs []error
	for index := len(slot.Repositories) - 1; index >= 0; index-- {
		repository := slot.Repositories[index]
		pathExists, registered, err := inspectInterruptedClaimPath(oven.ClaimRepository{
			SourceRepo: repository.SourceRepo, PhysicalPath: repository.WorktreePath,
		})
		if err != nil {
			cleanupErrs = append(cleanupErrs, err)
			continue
		}
		if pathExists != registered {
			cleanupErrs = append(cleanupErrs, fmt.Errorf("%s: materialized path and Git registration disagree", repository.Name))
			continue
		}
		if pathExists {
			if removeErr := service.removeWorktree(repository.SourceRepo, repository.WorktreePath, true); removeErr != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("%s: removing materialized worktree: %w", repository.Name, removeErr))
			}
		}
	}
	if len(cleanupErrs) == 0 {
		if err := service.removeOwnedOvenClaimRoot(*slot); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	if len(cleanupErrs) == 0 {
		inventory.RemoveSlot(slot.ID)
	} else {
		slot.Status = oven.StatusQuarantined
		slot.Failure = safeOvenFailure(errors.Join(cause, errors.Join(cleanupErrs...)))
	}
	if err := service.Oven.Save(inventory); err != nil {
		cleanupErrs = append(cleanupErrs, err)
	}
	return errors.Join(cause, errors.Join(cleanupErrs...))
}

func (service *Service) attachOvenClaimRepositories(options OvenClaimOptions, slot oven.Slot, claim oven.Claim) (models.Workspace, []oven.ClaimRepository, error) {
	workspace := models.NewWorkspace(options.Name, claim.Alias, options.Branch)
	workspace.Source = options.Source
	workspace.Oven = &models.OvenOwnership{SlotID: slot.ID, ClaimNonce: claim.Nonce}
	assigned := make([]oven.ClaimRepository, 0, len(claim.Repositories))
	attach := options.attachBranch
	if attach == nil {
		attach = func(repository oven.ClaimRepository, commit string) error {
			return gitops.AttachWorktreeBranch(repository.PhysicalPath, repository.Branch, commit, repository.BranchCreated)
		}
	}
	for _, repository := range claim.Repositories {
		prepared := ovenRepositoryByName(slot, repository.Name)
		if err := preflightReadyOvenRepository(slot, prepared, options.Branch); err != nil {
			return workspace, assigned, err
		}
		if err := attach(repository, prepared.Commit); err != nil {
			return workspace, assigned, fmt.Errorf("%s: attaching claim branch: %w", repository.Name, err)
		}
		assigned = append(assigned, repository)
		workspace.Repos = append(workspace.Repos, models.RepoWorktree{
			RepoName: repository.Name, SourceRepo: repository.SourceRepo,
			WorktreePath: repository.AliasPath, Branch: options.Branch,
			PreserveBranch: !repository.BranchCreated,
		})
	}
	return workspace, assigned, nil
}

func (service *Service) publishOvenClaimAlias(options OvenClaimOptions, slot oven.Slot, claim oven.Claim) error {
	if _, err := os.Lstat(claim.Alias); !os.IsNotExist(err) {
		return fmt.Errorf("workspace path changed before oven claim publication")
	}
	publish := options.publishAlias
	if publish == nil {
		publish = os.Symlink
	}
	if err := publish(slot.BackingPath, claim.Alias); err != nil {
		return fmt.Errorf("publishing oven workspace alias: %w", err)
	}
	return nil
}

func (service *Service) saveClaimedOvenWorkspace(options OvenClaimOptions, workspace models.Workspace, slotID string) (*OvenClaimResult, error) {
	save := options.saveWorkspace
	if save == nil {
		save = service.State.AddWorkspace
	}
	if err := save(workspace); err != nil {
		current, readErr := service.State.GetWorkspace(workspace.Name)
		if readErr != nil || current == nil || !reflect.DeepEqual(*current, workspace) {
			return nil, errors.Join(err, readErr)
		}
		return &OvenClaimResult{Workspace: workspace, SlotID: slotID, Warning: err}, nil
	}
	return &OvenClaimResult{Workspace: workspace, SlotID: slotID}, nil
}

func (service *Service) preflightOvenClaimDestination(options OvenClaimOptions) error {
	if existing, err := service.State.GetWorkspace(options.Name); err != nil {
		return err
	} else if existing != nil {
		return fmt.Errorf("workspace %s already exists", options.Name)
	}
	alias := filepath.Join(options.Config.WorkspaceDir, options.Name)
	if _, err := os.Lstat(alias); err == nil {
		return fmt.Errorf("workspace path already exists: %s", alias)
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (service *Service) preflightReadyOvenTemplate(options OvenClaimOptions, slot oven.Slot) error {
	if slot.TemplateSlotID != "" || slot.Status != oven.StatusReady || slot.Claim != nil || slot.RecipeKey != options.RecipeKey || slot.Runner != options.Runner {
		return fmt.Errorf("oven slot is not a reusable template for this Recipe and runner")
	}
	if err := service.verifyOvenBackingPath(slot); err != nil {
		return err
	}
	return errors.Join(
		verifyDetachedOvenRepositories(slot, slot.Repositories),
		validatePreparedTrees(slot),
	)
}

func (service *Service) preflightOvenClaimRepositories(options OvenClaimOptions, template oven.Slot) error {
	for _, repository := range template.Repositories {
		if !sourceRepoAllowed(options.Config, repository.SourceRepo) {
			return fmt.Errorf("%s: source repository is outside configured repository directories", repository.Name)
		}
		if err := preflightReadyOvenRepository(template, repository, options.Branch); err != nil {
			return err
		}
	}
	return nil
}

func sourceRepoAllowed(config *models.Config, sourceRepo string) bool {
	canonicalSource := canonicalPath(sourceRepo)
	for _, root := range config.RepoDirs {
		canonicalRoot := canonicalPath(root)
		relative, err := filepath.Rel(canonicalRoot, canonicalSource)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func preflightReadyOvenRepository(slot oven.Slot, repository oven.Repository, branch string) error {
	if repository.Name == "" {
		return fmt.Errorf("oven repository is missing")
	}
	hasWorktree, err := gitops.WorktreeHasBranch(repository.SourceRepo, branch)
	if err != nil {
		return err
	}
	if hasWorktree {
		return fmt.Errorf("%s: branch %s already has a worktree", repository.Name, branch)
	}
	if gitops.BranchExists(repository.SourceRepo, branch) {
		commit, err := gitops.LocalBranchCommit(repository.SourceRepo, branch)
		if err != nil || commit != repository.Commit {
			return fmt.Errorf("%s: branch %s exists at a different commit", repository.Name, branch)
		}
	}
	return nil
}

func (service *Service) newOvenClaim(options OvenClaimOptions, slot oven.Slot) (oven.Claim, error) {
	nonce := options.nonce
	if nonce == "" {
		var err error
		nonce, err = randomOvenID()
		if err != nil {
			return oven.Claim{}, err
		}
	}
	alias := filepath.Join(options.Config.WorkspaceDir, options.Name)
	claim := oven.Claim{
		Nonce: nonce, WorkspaceName: options.Name, Alias: alias,
		Branch: options.Branch, StartedAt: ovenClaimTimestamp(options),
	}
	for _, repository := range slot.Repositories {
		claim.Repositories = append(claim.Repositories, oven.ClaimRepository{
			Name: repository.Name, SourceRepo: repository.SourceRepo,
			PhysicalPath: repository.WorktreePath, AliasPath: filepath.Join(alias, repository.Name),
			Branch: options.Branch, BranchCreated: !gitops.BranchExists(repository.SourceRepo, options.Branch),
		})
	}
	return claim, nil
}

func (service *Service) rollbackOvenClaim(slotID, nonce string, assigned []oven.ClaimRepository, aliasCreated bool, cause error) error {
	inventory, err := service.Oven.Load()
	if err != nil {
		return errors.Join(cause, err)
	}
	slot := inventory.FindSlot(slotID)
	if slot == nil || slot.Status != oven.StatusClaiming || slot.Claim == nil || slot.Claim.Nonce != nonce {
		return errors.Join(cause, fmt.Errorf("oven claim identity changed; preserving artifacts"))
	}
	if err := preflightOvenClaimRollback(*slot, *slot.Claim, assigned, aliasCreated); err != nil {
		slot.Status = oven.StatusQuarantined
		slot.Failure = safeOvenFailure(errors.Join(cause, err))
		if saveErr := service.Oven.Save(inventory); saveErr != nil {
			return errors.Join(cause, err, saveErr)
		}
		return errors.Join(cause, err)
	}

	var rollbackErrs []error
	if aliasCreated {
		if err := os.Remove(slot.Claim.Alias); err != nil && !os.IsNotExist(err) {
			rollbackErrs = append(rollbackErrs, err)
		}
	}
	for index := len(assigned) - 1; index >= 0; index-- {
		repository := assigned[index]
		commit := ovenRepositoryByName(*slot, repository.Name).Commit
		if err := gitops.DetachWorktree(repository.PhysicalPath, commit); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("%s: detaching rollback: %w", repository.Name, err))
			continue
		}
		if repository.BranchCreated {
			if err := gitops.DeleteBranchIfAt(repository.SourceRepo, repository.Branch, commit, true); err != nil {
				rollbackErrs = append(rollbackErrs, fmt.Errorf("%s: deleting rollback branch: %w", repository.Name, err))
			}
		}
	}
	if len(rollbackErrs) > 0 {
		slot.Status = oven.StatusQuarantined
		slot.Failure = safeOvenFailure(errors.Join(cause, errors.Join(rollbackErrs...)))
	} else if slot.TemplateSlotID != "" {
		if err := service.removeRolledBackOvenClaim(*slot); err != nil {
			rollbackErrs = append(rollbackErrs, err)
			slot.Status = oven.StatusQuarantined
			slot.Failure = safeOvenFailure(errors.Join(cause, err))
		} else {
			inventory.RemoveSlot(slot.ID)
		}
	} else {
		slot.Status = oven.StatusReady
		slot.Claim = nil
		slot.Failure = ""
	}
	if err := service.Oven.Save(inventory); err != nil {
		rollbackErrs = append(rollbackErrs, err)
	}
	return errors.Join(cause, errors.Join(rollbackErrs...))
}

func (service *Service) removeRolledBackOvenClaim(slot oven.Slot) error {
	if err := verifyDetachedOvenRepositories(slot, slot.Repositories); err != nil {
		return fmt.Errorf("verifying rolled-back materialization: %w", err)
	}
	for index := len(slot.Repositories) - 1; index >= 0; index-- {
		repository := slot.Repositories[index]
		if err := service.removeWorktree(repository.SourceRepo, repository.WorktreePath, true); err != nil {
			return fmt.Errorf("%s: removing rolled-back materialization: %w", repository.Name, err)
		}
	}
	return service.removeOwnedOvenClaimRoot(slot)
}

func preflightOvenClaimRollback(slot oven.Slot, claim oven.Claim, assigned []oven.ClaimRepository, aliasCreated bool) error {
	if aliasCreated {
		target, err := os.Readlink(claim.Alias)
		if err != nil || canonicalPath(target) != canonicalPath(slot.BackingPath) {
			return fmt.Errorf("oven claim alias changed; preserving artifacts")
		}
	}
	for _, repository := range assigned {
		branch, err := gitops.CurrentBranch(repository.PhysicalPath)
		if err != nil || branch != claim.Branch {
			return fmt.Errorf("%s: claim branch changed; preserving artifacts", repository.Name)
		}
		head, err := gitops.HeadCommit(repository.PhysicalPath)
		if err != nil || head != ovenRepositoryByName(slot, repository.Name).Commit {
			return fmt.Errorf("%s: claim commit changed; preserving artifacts", repository.Name)
		}
	}
	return nil
}

func ovenRepositoryByName(slot oven.Slot, name string) oven.Repository {
	for _, repository := range slot.Repositories {
		if repository.Name == name {
			return repository
		}
	}
	return oven.Repository{}
}

func ovenClaimTimestamp(options OvenClaimOptions) string {
	now := options.now
	if now == nil {
		now = time.Now
	}
	return now().UTC().Format(time.RFC3339Nano)
}
