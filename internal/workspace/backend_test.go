package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// faultBackend wraps a real mutationBackend and can inject a failure either
// before or after a named phase is applied, proving the seam supports both
// precondition failures and "applied then failed" post-mutation failures.
type faultBackend struct {
	inner       mutationBackend
	failBefore  map[string]error
	failAfter   map[string]error
	appliedRepo []string // repos whose WorktreeAdd actually ran
}

func newFaultBackend(inner mutationBackend) *faultBackend {
	return &faultBackend{
		inner:      inner,
		failBefore: map[string]error{},
		failAfter:  map[string]error{},
	}
}

func (f *faultBackend) CreateBranch(repo, branch, start string) error {
	if err := f.failBefore["CreateBranch:"+repo]; err != nil {
		return err
	}
	if err := f.inner.CreateBranch(repo, branch, start); err != nil {
		return err
	}
	return f.failAfter["CreateBranch:"+repo]
}
func (f *faultBackend) DeleteBranch(repo, branch string, force bool) error {
	if err := f.failBefore["DeleteBranch:"+repo]; err != nil {
		return err
	}
	return f.inner.DeleteBranch(repo, branch, force)
}
func (f *faultBackend) WorktreeAdd(repo, path, branch string) error {
	if err := f.failBefore["WorktreeAdd:"+repo]; err != nil {
		return err
	}
	if err := f.inner.WorktreeAdd(repo, path, branch); err != nil {
		return err
	}
	f.appliedRepo = append(f.appliedRepo, repo)
	return f.failAfter["WorktreeAdd:"+repo]
}
func (f *faultBackend) WorktreeAddTracking(repo, path, branch string) error {
	if err := f.failBefore["WorktreeAddTracking:"+repo]; err != nil {
		return err
	}
	return f.inner.WorktreeAddTracking(repo, path, branch)
}
func (f *faultBackend) WorktreeRemove(repo, path string) error {
	if err := f.failBefore["WorktreeRemove:"+repo]; err != nil {
		return err
	}
	return f.inner.WorktreeRemove(repo, path)
}
func (f *faultBackend) WorktreeRepair(repo, path string) error {
	return f.inner.WorktreeRepair(repo, path)
}
func (f *faultBackend) Mkdir(path string, perm os.FileMode) error {
	if err := f.failBefore["Mkdir:"+path]; err != nil {
		return err
	}
	if err := f.inner.Mkdir(path, perm); err != nil {
		return err
	}
	return f.failAfter["Mkdir:"+path]
}
func (f *faultBackend) RemoveAll(path string) error {
	if err := f.failBefore["RemoveAll:"+path]; err != nil {
		return err
	}
	return f.inner.RemoveAll(path)
}
func (f *faultBackend) Rename(o, n string) error { return f.inner.Rename(o, n) }

// TestMutationBackendInstanceScoped proves failpoints are per-Service instance
// and can model both before- and after-mutation failures.
func TestMutationBackendInstanceScoped(t *testing.T) {
	env := setupTestEnv(t)

	// A default service uses the production backend (no faults).
	if _, ok := env.svc.mut().(prodBackend); !ok {
		// setupTestEnv leaves backend nil, so mut() must default to prod.
		t.Fatalf("expected default prod backend, got %T", env.svc.mut())
	}

	// Install a fault-injecting backend on this instance only.
	fb := newFaultBackend(prodBackend{})
	env.svc.backend = fb

	repoPath := env.createRepo("api")
	dst := filepath.Join(t.TempDir(), "wt")
	if err := fb.CreateBranch(repoPath, "feat/x", ""); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	// Before-failure: the mutation is not applied.
	sentinel := errors.New("pre")
	fb.failBefore["WorktreeAdd:"+repoPath] = sentinel
	if err := env.svc.mut().WorktreeAdd(repoPath, dst, "feat/x"); !errors.Is(err, sentinel) {
		t.Fatalf("expected before-failure, got %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("worktree should not exist after a before-failure")
	}

	// After-failure: the mutation IS applied, then an error is returned.
	delete(fb.failBefore, "WorktreeAdd:"+repoPath)
	after := errors.New("post")
	fb.failAfter["WorktreeAdd:"+repoPath] = after
	if err := env.svc.mut().WorktreeAdd(repoPath, dst, "feat/x"); !errors.Is(err, after) {
		t.Fatalf("expected after-failure, got %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("worktree should exist after an after-failure (mutation applied): %v", err)
	}
	if len(fb.appliedRepo) != 1 {
		t.Fatalf("expected exactly one applied WorktreeAdd, got %v", fb.appliedRepo)
	}

	// A fresh service is unaffected — failpoints are instance-scoped.
	other := setupTestEnv(t)
	if _, ok := other.svc.mut().(prodBackend); !ok {
		t.Fatalf("fresh service must not inherit faults, got %T", other.svc.mut())
	}
}
