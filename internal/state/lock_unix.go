//go:build !windows

package state

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// errLockTimeout is the sentinel returned when the advisory lock cannot be
// acquired within the deadline. Callers translate it into a CodedError.
var errLockTimeout = errors.New("state lock timeout")

// fileLock is a process-level advisory lock backed by flock(2). The lock is
// released when the file descriptor is closed or the process exits, which is
// what makes stranded locks self-healing after a crash. The lock file itself is
// never unlinked.
type fileLock struct {
	f *os.File
}

// acquireLock takes an exclusive advisory lock on path, polling until the
// deadline. It returns errLockTimeout if the lock cannot be acquired in time.
func acquireLock(ctx context.Context, path string, timeout time.Duration) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &fileLock{f: f}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, err
		}
		if ctx != nil {
			select {
			case <-ctx.Done():
				_ = f.Close()
				return nil, ctx.Err()
			default:
			}
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, errLockTimeout
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// release unlocks and closes the descriptor without removing the lock file.
func (l *fileLock) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}
