package workspace

import (
	"context"
	"os"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/state"
)

// mutationBackend performs the low-level Git and filesystem mutations that make
// up a workspace lifecycle operation. It is an instance-scoped seam so tests can
// wrap production behavior and inject failures either before or after a mutation
// is applied — the latter is essential for modeling "mutation succeeded, then
// the process died" cases that pure preconditions cannot represent.
//
// Release binaries always use prodBackend, selected by NewService. There is no
// production code path that installs a fault-injecting backend, so failpoints
// cannot be enabled outside tests.
//
// Lifecycle operations migrate onto this backend from Task 3 onward; Task 2
// establishes the seam and its production implementation.
type mutationBackend interface {
	CreateBranch(repo, branch, startPoint string) error
	DeleteBranch(repo, branch string, force bool) error
	WorktreeAdd(repo, path, branch string) error
	WorktreeAddTracking(repo, path, branch string) error
	WorktreeRemove(repo, path string) error
	WorktreeRepair(repo, path string) error
	Mkdir(path string, perm os.FileMode) error
	RemoveAll(path string) error
	Rename(oldPath, newPath string) error
}

// prodBackend is the production mutation backend backed by real git and os
// calls.
type prodBackend struct{}

func (prodBackend) CreateBranch(repo, branch, startPoint string) error {
	return gitops.CreateBranch(repo, branch, startPoint)
}
func (prodBackend) DeleteBranch(repo, branch string, force bool) error {
	return gitops.DeleteBranch(repo, branch, force)
}
func (prodBackend) WorktreeAdd(repo, path, branch string) error {
	return gitops.WorktreeAdd(repo, path, branch)
}
func (prodBackend) WorktreeAddTracking(repo, path, branch string) error {
	return gitops.WorktreeAddTracking(repo, path, branch)
}
func (prodBackend) WorktreeRemove(repo, path string) error {
	return gitops.WorktreeRemove(repo, path)
}
func (prodBackend) WorktreeRepair(repo, path string) error {
	return gitops.WorktreeRepair(repo, path)
}
func (prodBackend) Mkdir(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (prodBackend) RemoveAll(path string) error {
	return os.RemoveAll(path)
}
func (prodBackend) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

// mut returns the Service's mutation backend, defaulting to production.
func (s *Service) mut() mutationBackend {
	if s.backend != nil {
		return s.backend
	}
	return prodBackend{}
}

// withMutation is the single commit boundary lifecycle operations use. The
// callback performs in-memory edits on the locked snapshot but must NOT call
// Commit; withMutation owns the commit so tests can intercept it via an
// instance-scoped commit seam (commitFault) to model either a pre-commit
// failure (state not applied) or an applied-then-error ambiguous commit.
func (s *Service) withMutation(ctx context.Context, fn func(*state.Mutation) error) error {
	return s.State.WithMutation(ctx, func(m *state.Mutation) error {
		if err := fn(m); err != nil {
			return err
		}
		if s.commitFault != nil {
			return s.commitFault(m)
		}
		return m.Commit()
	})
}
