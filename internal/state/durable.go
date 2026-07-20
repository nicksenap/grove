package state

import (
	"os"
	"path/filepath"
)

// durableSeams holds optional, test-only fault injectors for each durability
// phase. All fields are nil in production.
type durableSeams struct {
	failWrite   func() error
	failSync    func() error
	failRename  func() error
	failDirSync func() error
}

// writeFileDurableWith writes data to path with crash-consistent durability: it
// writes to a uniquely named temp file in the destination directory, fsyncs the
// file, atomically renames it into place, then fsyncs the parent directory so
// the rename itself is durable. The unique temp name avoids collisions between
// concurrent writers and never leaves a shared ".tmp" path behind.
func writeFileDurableWith(path string, data []byte, perm os.FileMode, seams durableSeams) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	// Clean up the temp file on any failure before the rename succeeds.
	defer func() {
		if tmp != "" {
			_ = os.Remove(tmp)
		}
	}()

	if err = f.Chmod(perm); err != nil {
		_ = f.Close()
		return err
	}
	if seams.failWrite != nil {
		if err = seams.failWrite(); err != nil {
			_ = f.Close()
			return err
		}
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if seams.failSync != nil {
		if err = seams.failSync(); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if seams.failRename != nil {
		if err = seams.failRename(); err != nil {
			return err
		}
	}
	if err = os.Rename(tmp, path); err != nil {
		return err
	}
	tmp = "" // renamed successfully; nothing to clean up

	// fsync the parent directory so the rename survives a crash.
	if seams.failDirSync != nil {
		if err = seams.failDirSync(); err != nil {
			return err
		}
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err = d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}

// writeFileDurable is the Store's durable writer, wired to its test-only seams.
func (s *Store) writeFileDurable(path string, data []byte, perm os.FileMode) error {
	return writeFileDurableWith(path, data, perm, durableSeams{
		failWrite:   s.failWrite,
		failSync:    s.failSync,
		failRename:  s.failRename,
		failDirSync: s.failDirSync,
	})
}
