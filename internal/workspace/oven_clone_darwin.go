//go:build darwin

package workspace

import "golang.org/x/sys/unix"

func cloneDirectoryNative(source, destination string) (bool, error) {
	if err := unix.Clonefile(source, destination, 0); err != nil {
		return false, err
	}
	return true, nil
}
