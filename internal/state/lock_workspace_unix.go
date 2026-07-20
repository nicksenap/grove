//go:build !windows

package state

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// AcquireWorkspaceLock takes a cross-process advisory lock scoped to a single
// workspace name. It serializes same-name lifecycle operations (e.g. two
// concurrent creates) without holding the global state lock, so provisioning
// for different workspaces still runs concurrently. The returned release
// function must be called when the critical section ends. The lock file is
// never unlinked.
func (s *Store) AcquireWorkspaceLock(ctx context.Context, name string) (func(), error) {
	return s.AcquireResourceLock(ctx, "ws-"+name)
}

// AcquireResourceLock takes a cross-process advisory lock scoped to an arbitrary
// resource key (e.g. a workspace name, or a source-repo+branch pair). Distinct
// keys run concurrently; the same key serializes. The lock file is never
// unlinked.
func (s *Store) AcquireResourceLock(ctx context.Context, key string) (func(), error) {
	dir := filepath.Join(s.dir(), "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lp := filepath.Join(dir, sanitizeIDPart(key)+".lock")
	lock, err := acquireLock(ctx, lp, lockTimeout)
	if err != nil {
		if errors.Is(err, errLockTimeout) {
			return nil, &CodedError{
				Code:      CodeStateLockTimeout,
				Message:   fmt.Sprintf("could not acquire lock for %q within %s", key, lockTimeout),
				Retryable: true,
			}
		}
		return nil, err
	}
	return func() { _ = lock.release() }, nil
}
