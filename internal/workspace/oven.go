package workspace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/oven"
)

var (
	ovenRepositoryName      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	ErrOvenGenerationActive = errors.New("oven generation already has an active slot")
)

type OvenBakeOptions struct {
	RecipeKey  string
	RecipeName string
	RecipePath string
	Generation string
	Runner     string
	Repos      []string
	RepoMap    map[string]string
	Commits    map[string]string

	slotID string
	now    func() time.Time
}

type OvenBakeError struct {
	Cause      error
	CleanupErr error
}

func (err *OvenBakeError) Error() string {
	if err.CleanupErr != nil {
		return fmt.Sprintf("baking Oven slot: %s; cleanup failed: %s", err.Cause, err.CleanupErr)
	}
	return fmt.Sprintf("baking Oven slot: %s", err.Cause)
}

func (err *OvenBakeError) Unwrap() error { return err.Cause }

// BakeOvenSlot creates detached exact-commit worktrees under the private Oven
// root, runs trusted Recipe preparation outside the mutation lock, and exposes
// the slot as ready only after an ownership recheck.
func (service *Service) BakeOvenSlot(options OvenBakeOptions, prepare func(map[string]string) error) (*oven.Slot, error) {
	if service.Oven == nil {
		return nil, fmt.Errorf("oven store is not configured")
	}
	slot, err := service.newBakingSlot(options)
	if err != nil {
		return nil, err
	}
	created, recorded, err := service.createDetachedSlot(slot)
	if err != nil {
		if !recorded {
			return nil, &OvenBakeError{Cause: err}
		}
		cleanupErr := service.failOvenBake(slot.ID, created, err)
		return nil, &OvenBakeError{Cause: err, CleanupErr: cleanupErr}
	}

	worktrees := make(map[string]string, len(slot.Repositories))
	for _, repository := range slot.Repositories {
		worktrees[repository.Name] = repository.WorktreePath
	}
	if err := prepare(worktrees); err != nil {
		cleanupErr := service.failOvenBake(slot.ID, slot.Repositories, err)
		return nil, &OvenBakeError{Cause: err, CleanupErr: cleanupErr}
	}

	var ready *oven.Slot
	err = service.State.WithLock(func() error {
		inventory, err := service.Oven.Load()
		if err != nil {
			return err
		}
		current := inventory.FindSlot(slot.ID)
		if current == nil || current.Status != oven.StatusBaking || !sameOvenSlotIdentity(*current, slot) {
			return fmt.Errorf("oven slot changed during bake; refusing readiness")
		}
		if err := errors.Join(
			service.verifyOvenBackingPath(*current),
			verifyDetachedOvenRepositories(*current, current.Repositories),
			verifyNoOvenCredentialResidue(*current),
		); err != nil {
			current.Status = oven.StatusQuarantined
			current.OwnerPID = 0
			current.Failure = safeOvenFailure(err)
			current.UpdatedAt = ovenTimestamp(options)
			if saveErr := service.Oven.Save(inventory); saveErr != nil {
				return errors.Join(err, saveErr)
			}
			return err
		}
		current.Status = oven.StatusReady
		current.OwnerPID = 0
		current.Failure = ""
		current.UpdatedAt = ovenTimestamp(options)
		if err := service.Oven.Save(inventory); err != nil {
			return err
		}
		copy := *current
		ready = &copy
		return nil
	})
	if err != nil {
		return nil, &OvenBakeError{Cause: err}
	}
	return ready, nil
}

func (service *Service) newBakingSlot(options OvenBakeOptions) (oven.Slot, error) {
	if options.RecipeKey == "" || options.Generation == "" || options.Runner == "" || len(options.Repos) == 0 {
		return oven.Slot{}, fmt.Errorf("recipe identity, generation, runner, and repositories are required")
	}
	id := options.slotID
	if id == "" {
		var err error
		id, err = randomOvenID()
		if err != nil {
			return oven.Slot{}, err
		}
	}
	backingPath := service.Oven.SlotPath(options.Generation, id)
	timestamp := ovenTimestamp(options)
	slot := oven.Slot{
		ID: id, RecipeKey: options.RecipeKey, RecipeName: options.RecipeName,
		RecipePath: options.RecipePath, Generation: options.Generation, Runner: options.Runner,
		BackingPath: backingPath, Status: oven.StatusBaking, OwnerPID: os.Getpid(), CreatedAt: timestamp, UpdatedAt: timestamp,
	}
	seen := make(map[string]bool, len(options.Repos))
	for _, name := range options.Repos {
		if !ovenRepositoryName.MatchString(name) || name == "." || name == ".." || seen[name] {
			return oven.Slot{}, fmt.Errorf("invalid or duplicate Oven repository name %q", name)
		}
		seen[name] = true
		source := options.RepoMap[name]
		commit := options.Commits[name]
		if source == "" || commit == "" {
			return oven.Slot{}, fmt.Errorf("repository %s has no source or exact commit", name)
		}
		slot.Repositories = append(slot.Repositories, oven.Repository{
			Name: name, SourceRepo: canonicalPath(source),
			WorktreePath: filepath.Join(backingPath, name), Commit: commit,
		})
	}
	return slot, nil
}

func (service *Service) createDetachedSlot(slot oven.Slot) ([]oven.Repository, bool, error) {
	created := make([]oven.Repository, 0, len(slot.Repositories))
	recorded := false
	err := service.State.WithLock(func() error {
		inventory, err := service.Oven.Load()
		if err != nil {
			return err
		}
		if inventory.FindSlot(slot.ID) != nil {
			return fmt.Errorf("oven slot %s already exists", slot.ID)
		}
		for _, existing := range inventory.Slots {
			if existing.RecipeKey == slot.RecipeKey && existing.Generation == slot.Generation && existing.Runner == slot.Runner &&
				(existing.Status == oven.StatusBaking || existing.Status == oven.StatusReady) {
				return ErrOvenGenerationActive
			}
		}
		inventory.Slots = append(inventory.Slots, slot)
		if err := service.Oven.Save(inventory); err != nil {
			return err
		}
		recorded = true
		if err := createOvenBackingDirectory(service.Oven, slot); err != nil {
			return err
		}
		for _, repository := range slot.Repositories {
			if err := gitops.WorktreeAddDetached(repository.SourceRepo, repository.WorktreePath, repository.Commit); err != nil {
				return fmt.Errorf("%s: creating detached worktree: %w", repository.Name, err)
			}
			created = append(created, repository)
		}
		return nil
	})
	return created, recorded, err
}

func createOvenBackingDirectory(store *oven.Store, slot oven.Slot) error {
	directories := []string{
		store.GenerationsPath(),
		filepath.Join(store.GenerationsPath(), slot.Generation),
		filepath.Join(store.GenerationsPath(), slot.Generation, "slots"),
	}
	for _, directory := range directories {
		if err := ensurePrivateOvenDirectory(directory); err != nil {
			return err
		}
	}
	if err := os.Mkdir(slot.BackingPath, 0o700); err != nil {
		return fmt.Errorf("creating Oven backing path: %w", err)
	}
	return nil
}

func ensurePrivateOvenDirectory(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := os.Mkdir(path, 0o700); err != nil {
			return fmt.Errorf("creating Oven directory %s: %w", path, err)
		}
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("unsafe Oven directory %s", path)
	}
	return nil
}

func (service *Service) failOvenBake(slotID string, created []oven.Repository, cause error) error {
	return service.State.WithLock(func() error {
		inventory, err := service.Oven.Load()
		if err != nil {
			return err
		}
		slot := inventory.FindSlot(slotID)
		if slot == nil || slot.Status != oven.StatusBaking {
			return fmt.Errorf("oven slot changed after bake failure; refusing cleanup")
		}
		if err := verifyDetachedOvenRepositories(*slot, created); err != nil {
			slot.Status = oven.StatusQuarantined
			slot.OwnerPID = 0
			slot.Failure = safeOvenFailure(errors.Join(cause, err))
			slot.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return service.Oven.Save(inventory)
		}

		var cleanupErrs []error
		for index := len(created) - 1; index >= 0; index-- {
			repository := created[index]
			if err := service.removeWorktree(repository.SourceRepo, repository.WorktreePath, true); err != nil {
				cleanupErrs = append(cleanupErrs, fmt.Errorf("%s: removing failed Oven worktree: %w", repository.Name, err))
			}
		}
		if len(cleanupErrs) == 0 {
			if err := os.Remove(slot.BackingPath); err != nil && !os.IsNotExist(err) {
				cleanupErrs = append(cleanupErrs, err)
			}
		}
		if len(cleanupErrs) > 0 {
			slot.Status = oven.StatusQuarantined
		} else {
			slot.Status = oven.StatusFailed
		}
		slot.OwnerPID = 0
		slot.Failure = safeOvenFailure(cause)
		slot.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := service.Oven.Save(inventory); err != nil {
			cleanupErrs = append(cleanupErrs, err)
		}
		return errors.Join(cleanupErrs...)
	})
}

func verifyDetachedOvenRepositories(slot oven.Slot, repositories []oven.Repository) error {
	for _, repository := range repositories {
		if filepath.Dir(repository.WorktreePath) != slot.BackingPath || filepath.Base(repository.WorktreePath) != repository.Name {
			return fmt.Errorf("%s: worktree path escapes its Oven slot", repository.Name)
		}
		entries, err := gitops.WorktreeList(repository.SourceRepo)
		if err != nil {
			return err
		}
		registered := false
		for _, entry := range entries {
			if canonicalPath(entry.Path) == canonicalPath(repository.WorktreePath) && entry.Branch == "" {
				registered = true
				break
			}
		}
		if !registered {
			return fmt.Errorf("%s: detached worktree registration changed", repository.Name)
		}
		branch, err := gitops.CurrentBranch(repository.WorktreePath)
		if err != nil || branch != "" {
			return fmt.Errorf("%s: prepared worktree is not detached", repository.Name)
		}
		head, err := gitops.HeadCommit(repository.WorktreePath)
		if err != nil || head != repository.Commit {
			return fmt.Errorf("%s: prepared commit changed", repository.Name)
		}
		if status, err := gitops.TrackedStatus(repository.WorktreePath); err != nil || status != "" {
			return fmt.Errorf("%s: prepared tracked files changed", repository.Name)
		}
	}
	return nil
}

func verifyNoOvenCredentialResidue(slot oven.Slot) error {
	for _, repository := range slot.Repositories {
		entries := 0
		err := filepath.WalkDir(repository.WorktreePath, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			entries++
			if entries > 100000 {
				return fmt.Errorf("%s: credential residue scan exceeded its entry limit", repository.Name)
			}
			if entry.IsDir() && path != repository.WorktreePath {
				switch entry.Name() {
				case "node_modules", ".venv", ".cache", "vendor":
					return filepath.SkipDir
				}
			}
			if entry.IsDir() || !forbiddenOvenCredentialPath(path) {
				return nil
			}
			relative, _ := filepath.Rel(repository.WorktreePath, path)
			return fmt.Errorf("%s: credential file %s remains after preparation", repository.Name, relative)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func forbiddenOvenCredentialPath(path string) bool {
	name := filepath.Base(path)
	switch name {
	case ".npmrc", ".pypirc", ".netrc", ".git-credentials", "id_rsa", "id_ed25519":
		return true
	case "credentials":
		return filepath.Base(filepath.Dir(path)) == ".aws"
	case "config.json":
		return filepath.Base(filepath.Dir(path)) == ".docker"
	}
	if name == ".env" || strings.HasPrefix(name, ".env.") {
		return !strings.HasSuffix(name, ".example") && !strings.HasSuffix(name, ".sample") && !strings.HasSuffix(name, ".template")
	}
	return false
}

func sameOvenSlotIdentity(left, right oven.Slot) bool {
	if left.ID != right.ID || left.RecipeKey != right.RecipeKey || left.Generation != right.Generation ||
		left.Runner != right.Runner || left.BackingPath != right.BackingPath || len(left.Repositories) != len(right.Repositories) {
		return false
	}
	for index := range left.Repositories {
		if left.Repositories[index] != right.Repositories[index] {
			return false
		}
	}
	return true
}

func randomOvenID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func ovenTimestamp(options OvenBakeOptions) string {
	now := options.now
	if now == nil {
		now = time.Now
	}
	return now().UTC().Format(time.RFC3339Nano)
}

func safeOvenFailure(err error) string {
	if err == nil {
		return ""
	}
	message := strings.Map(func(char rune) rune {
		if char < 0x20 || char == 0x7f || char >= 0x80 && char <= 0x9f {
			return ' '
		}
		return char
	}, err.Error())
	message = strings.TrimSpace(message)
	if len(message) > 1024 {
		message = message[:1024]
	}
	return message
}
