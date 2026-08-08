package state

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// WithLock serializes a state-changing operation across Grove processes.
// The separate lock file is stable while state.json is atomically replaced.
func (s *Store) WithLock(fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	path := filepath.Join(filepath.Dir(s.Path), "state.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("opening state lock: %w", err)
	}
	defer file.Close()

	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		return fmt.Errorf("locking state: %w", err)
	}
	defer unix.Flock(int(file.Fd()), unix.LOCK_UN) //nolint:errcheck -- close also releases the lock

	return fn()
}
