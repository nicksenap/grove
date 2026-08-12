//go:build !darwin

package workspace

func cloneDirectoryNative(_, _ string) (bool, error) {
	return false, nil
}
