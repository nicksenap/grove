package workspace

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type preparedCloneFixture struct {
	template       string
	destination    string
	launcher       string
	destinationGit []byte
}

func TestClonePreparedWorktreeFallsBackAfterPartialNativeClone(t *testing.T) {
	root := t.TempDir()
	template := filepath.Join(root, "aaaaaaaa")
	destination := filepath.Join(root, "bbbbbbbb")
	for _, path := range []string{template, destination} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(template, ".git"), []byte("template git"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(template, "prepared"), []byte("ready"), 0o755); err != nil {
		t.Fatal(err)
	}
	destinationGit := []byte("gitdir: destination-admin")
	if err := os.WriteFile(filepath.Join(destination, ".git"), destinationGit, 0o644); err != nil {
		t.Fatal(err)
	}

	err := clonePreparedWorktreeWith(template, destination, func(_, target string) (bool, error) {
		if err := os.Mkdir(target, 0o755); err != nil {
			return false, err
		}
		if err := os.WriteFile(filepath.Join(target, "partial"), []byte("partial"), 0o644); err != nil {
			return false, err
		}
		return false, errors.New("native clone failed")
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "partial")); !os.IsNotExist(err) {
		t.Fatalf("partial native clone remained: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "prepared")); err != nil || string(data) != "ready" {
		t.Fatalf("fallback prepared file = %q, %v", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, ".git")); err != nil || !bytes.Equal(data, destinationGit) {
		t.Fatalf("fallback Git administration = %q, %v", data, err)
	}
}

func TestClonePreparedWorktreePreservesGitAdministrationAndRelocatesPaths(t *testing.T) {
	fixture := newPreparedCloneFixture(t)
	if err := clonePreparedWorktreeWith(fixture.template, fixture.destination, func(string, string) (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	assertPreparedClone(t, fixture)
}

func newPreparedCloneFixture(t *testing.T) preparedCloneFixture {
	t.Helper()
	root := t.TempDir()
	template := filepath.Join(root, "aaaaaaaa")
	destination := filepath.Join(root, "bbbbbbbb")
	for _, path := range []string{
		filepath.Join(template, ".venv", "bin"),
		filepath.Join(template, ".venv", "lib"),
		destination,
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(template, ".git"), []byte("template git"), 0o644); err != nil {
		t.Fatal(err)
	}
	destinationGit := []byte("gitdir: destination-admin")
	if err := os.WriteFile(filepath.Join(destination, ".git"), destinationGit, 0o644); err != nil {
		t.Fatal(err)
	}
	launcher := filepath.Join(template, ".venv", "bin", "tool")
	if err := os.WriteFile(launcher, []byte("#!"+template+"/.venv/bin/python\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pathFile := filepath.Join(template, ".venv", "lib", "editable.pth")
	if err := os.WriteFile(pathFile, []byte(template+"/src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	activation := filepath.Join(template, ".venv", "bin", "activate")
	if err := os.WriteFile(activation, []byte("VIRTUAL_ENV="+template+"/.venv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(template, "CMakeCache.txt"),
		[]byte("BUILD_ROOT="+template+"/build\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(template, ".venv", "bin", "tool"), filepath.Join(template, "tool")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(".venv", "bin", "tool"), filepath.Join(template, "relative-tool")); err != nil {
		t.Fatal(err)
	}
	return preparedCloneFixture{
		template: template, destination: destination, launcher: launcher, destinationGit: destinationGit,
	}
}

func assertPreparedClone(t *testing.T, fixture preparedCloneFixture) {
	t.Helper()
	gitData, err := os.ReadFile(filepath.Join(fixture.destination, ".git"))
	if err != nil || !bytes.Equal(gitData, fixture.destinationGit) {
		t.Fatalf("destination Git administration = %q, %v", gitData, err)
	}
	for _, path := range []string{
		filepath.Join(fixture.destination, ".venv", "bin", "tool"),
		filepath.Join(fixture.destination, ".venv", "bin", "activate"),
		filepath.Join(fixture.destination, ".venv", "lib", "editable.pth"),
		filepath.Join(fixture.destination, "CMakeCache.txt"),
	} {
		data, err := os.ReadFile(path)
		if err != nil || bytes.Contains(data, []byte(fixture.template)) ||
			!bytes.Contains(data, []byte(fixture.destination)) {
			t.Fatalf("relocated file %s = %q, %v", path, data, err)
		}
	}
	target, err := os.Readlink(filepath.Join(fixture.destination, "tool"))
	if err != nil || target != filepath.Join(fixture.destination, ".venv", "bin", "tool") {
		t.Fatalf("relocated symlink = %q, %v", target, err)
	}
	relativeTarget, err := os.Readlink(filepath.Join(fixture.destination, "relative-tool"))
	if err != nil || relativeTarget != filepath.Join(".venv", "bin", "tool") {
		t.Fatalf("relative symlink = %q, %v", relativeTarget, err)
	}
	info, err := os.Stat(filepath.Join(fixture.destination, ".venv", "bin", "tool"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("launcher mode = %v", info.Mode().Perm())
	}
	templateData, err := os.ReadFile(fixture.launcher)
	if err != nil || !bytes.Contains(templateData, []byte(fixture.template)) {
		t.Fatalf("template launcher changed = %q, %v", templateData, err)
	}
}
