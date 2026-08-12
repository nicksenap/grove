package workspace

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nicksenap/grove/internal/oven"
)

type unsupportedPreparedFileError struct {
	Path string
}

func (err *unsupportedPreparedFileError) Error() string {
	return fmt.Sprintf("unsupported prepared file type at %s", err.Path)
}

func validatePreparedTrees(slot oven.Slot) error {
	for _, repository := range slot.Repositories {
		if err := validatePreparedTree(repository.WorktreePath); err != nil {
			return fmt.Errorf("%s: %w", repository.Name, err)
		}
	}
	return nil
}

func validatePreparedTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if relative != "." && entry.Name() == ".git" {
			return fmt.Errorf("nested Git administration is not reusable at %s", relative)
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || entry.Type().IsRegular() {
			return nil
		}
		return &unsupportedPreparedFileError{Path: relative}
	})
}

// clonePreparedWorktree preserves the Git administrative file created for the
// destination while materializing all prepared files from the reusable
// template. On copy-on-write filesystems cloneDirectoryNative makes this
// independent materialization effectively constant-time and space-efficient.
func clonePreparedWorktree(template, destination string) error {
	return clonePreparedWorktreeWith(template, destination, cloneDirectoryNative)
}

func clonePreparedWorktreeWith(
	template, destination string,
	cloneNative func(string, string) (bool, error),
) error {
	gitFile := filepath.Join(destination, ".git")
	gitData, err := os.ReadFile(gitFile)
	if err != nil {
		return fmt.Errorf("reading materialized Git administration: %w", err)
	}
	gitInfo, err := os.Lstat(gitFile)
	if err != nil || !gitInfo.Mode().IsRegular() {
		return fmt.Errorf("materialized Git administration is not a regular file")
	}

	checkout := destination + ".checkout"
	if _, err := os.Lstat(checkout); !os.IsNotExist(err) {
		return fmt.Errorf("materialization checkout path already exists")
	}
	if err := os.Rename(destination, checkout); err != nil {
		return fmt.Errorf("preserving materialization checkout: %w", err)
	}
	restored := false
	defer func() {
		if !restored {
			_ = os.RemoveAll(destination)
			_ = os.Rename(checkout, destination)
		}
	}()

	cloned, cloneErr := cloneNative(template, destination)
	if cloned && cloneErr == nil {
		if err := os.Remove(filepath.Join(destination, ".git")); err != nil {
			return fmt.Errorf("replacing cloned Git administration: %w", err)
		}
		if err := os.WriteFile(filepath.Join(destination, ".git"), gitData, gitInfo.Mode().Perm()); err != nil {
			return fmt.Errorf("restoring materialized Git administration: %w", err)
		}
		if err := relocatePreparedPaths(template, destination); err != nil {
			return err
		}
		if err := os.RemoveAll(checkout); err != nil {
			return fmt.Errorf("removing temporary materialization checkout: %w", err)
		}
		restored = true
		return nil
	}
	if cloneErr != nil {
		_ = os.RemoveAll(destination)
	}
	if err := os.Rename(checkout, destination); err != nil {
		return fmt.Errorf("restoring materialization checkout: %w", err)
	}
	restored = true
	if err := overlayPreparedTree(template, destination); err != nil {
		return fmt.Errorf("copying prepared files: %w", err)
	}
	return relocatePreparedPaths(template, destination)
}

func overlayPreparedTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destination, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.Type()&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.RemoveAll(target); err != nil {
				return err
			}
			return os.Symlink(link, target)
		case entry.IsDir():
			if err := os.MkdirAll(target, info.Mode().Perm()); err != nil {
				return err
			}
			return os.Chmod(target, info.Mode().Perm())
		case info.Mode().IsRegular():
			return copyPreparedFile(path, target, info.Mode().Perm())
		default:
			return &unsupportedPreparedFileError{Path: relative}
		}
	})
}

func copyPreparedFile(source, destination string, mode fs.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return err
	}
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// relocatePreparedPaths keeps copied virtual environments and executable
// launchers independent from their immutable template. Oven slot IDs have a
// fixed width, so replacing one slot path with another does not change file
// layout or offsets in generated launchers.
func relocatePreparedPaths(template, destination string) error {
	if len(template) != len(destination) {
		return fmt.Errorf("prepared template and claim paths have different lengths")
	}
	oldPath, newPath := []byte(template), []byte(destination)
	return filepath.WalkDir(destination, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(destination, path)
		if err != nil {
			return err
		}
		if relative == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return relocatePreparedSymlink(path, string(oldPath), string(newPath))
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return &unsupportedPreparedFileError{Path: relative}
		}
		candidate, err := preparedPathCandidate(relative, entry)
		if err != nil || !candidate {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Contains(data, oldPath) {
			return nil
		}
		return os.WriteFile(path, bytes.ReplaceAll(data, oldPath, newPath), 0)
	})
}

func relocatePreparedSymlink(path, oldPath, newPath string) error {
	target, err := os.Readlink(path)
	if err != nil {
		return err
	}
	relocated := strings.ReplaceAll(target, oldPath, newPath)
	if relocated == target {
		return nil
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return os.Symlink(relocated, path)
}

func preparedPathCandidate(relative string, entry fs.DirEntry) (bool, error) {
	components := strings.Split(relative, string(filepath.Separator))
	inVirtualEnvironment := false
	inLauncherDirectory := false
	inDependencyTree := false
	for index, component := range components {
		switch component {
		case ".venv", "venv":
			inVirtualEnvironment = true
			if index+1 < len(components) &&
				(components[index+1] == "bin" || components[index+1] == "Scripts") {
				inLauncherDirectory = true
			}
		case "node_modules":
			inDependencyTree = true
			if index+1 < len(components) && components[index+1] == ".bin" {
				inLauncherDirectory = true
			}
		case "vendor", ".cache":
			inDependencyTree = true
		}
	}
	base := filepath.Base(relative)
	extension := filepath.Ext(base)
	if inLauncherDirectory {
		return true, nil
	}
	if inVirtualEnvironment {
		switch base {
		case "pyvenv.cfg", "direct_url.json":
			return true, nil
		}
		return extension == ".egg-link" || extension == ".pth", nil
	}
	switch base {
	case "CMakeCache.txt", "build.ninja", "rules.ninja":
		return true, nil
	}
	switch extension {
	case ".cmake", ".la", ".ninja", ".pc":
		return true, nil
	}
	if inDependencyTree {
		return false, nil
	}
	info, err := entry.Info()
	if err != nil {
		return false, err
	}
	return info.Mode().Perm()&0o111 != 0, nil
}
