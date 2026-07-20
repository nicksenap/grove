package state

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/models"
)

// --- Subprocess helper ---
//
// These tests re-exec the test binary as helper processes to exercise real
// cross-process locking (a single-process mutex would not catch lost updates
// between separate `gw` invocations).

const (
	envHelperMode = "GW_STATE_HELPER_MODE"
	envHelperDir  = "GW_STATE_HELPER_DIR"
	envHelperName = "GW_STATE_HELPER_NAME"
	envHelperFile = "GW_STATE_HELPER_READY"
)

// TestStateSubprocessHelper is not a real test; it is the entry point used when
// the process is re-executed as a helper. It returns immediately in normal runs.
func TestStateSubprocessHelper(t *testing.T) {
	mode := os.Getenv(envHelperMode)
	if mode == "" {
		return
	}
	store := NewStore(os.Getenv(envHelperDir))
	switch mode {
	case "add":
		name := os.Getenv(envHelperName)
		err := store.WithMutation(context.Background(), func(m *Mutation) error {
			if err := m.Add(models.NewWorkspace(name, "/tmp/"+name, "main")); err != nil {
				return err
			}
			return m.Commit()
		})
		if err != nil {
			if CodeOf(err) == CodeStateConflict {
				os.Exit(4) // deterministic conflict
			}
			os.Exit(3)
		}
		os.Exit(0)
	case "hold":
		// Acquire the lock and hold it (never releasing) until killed, to prove
		// the OS releases advisory locks on process death.
		lock, err := acquireLock(context.Background(), store.lockPath(), 5*time.Second)
		if err != nil {
			os.Exit(3)
		}
		_ = lock
		if ready := os.Getenv(envHelperFile); ready != "" {
			_ = os.WriteFile(ready, []byte("locked"), 0o644)
		}
		time.Sleep(60 * time.Second)
		os.Exit(0)
	case "writeop":
		// Write a recovery record then exit abruptly without cleaning it up, to
		// prove records survive a crashed helper process.
		ops := NewOperationStore(os.Getenv(envHelperDir))
		rec := &OperationRecord{
			Kind:      OpCreate,
			Workspace: os.Getenv(envHelperName),
			Phase:     "provisioning",
			Repos: []RepoOperation{
				{RepoName: "api", Status: RepoDone, BranchOwnership: OwnCreated, WorktreeOwnership: OwnCreated},
			},
		}
		if err := ops.Write(rec); err != nil {
			os.Exit(3)
		}
		if ready := os.Getenv(envHelperFile); ready != "" {
			_ = os.WriteFile(ready, []byte(rec.ID), 0o644)
		}
		os.Exit(0)
	}
}

// runHelper starts the test binary in helper mode.
func runHelper(t *testing.T, mode string, extraEnv ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestStateSubprocessHelper$")
	cmd.Env = append(os.Environ(), envHelperMode+"="+mode)
	cmd.Env = append(cmd.Env, extraEnv...)
	return cmd
}

func TestStoreConcurrentSubprocessMutations(t *testing.T) {
	dir := t.TempDir()
	groveDir := filepath.Join(dir, ".grove")
	if err := os.MkdirAll(groveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewStore(groveDir)
	if err := os.WriteFile(store.Path, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 12
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "ws-" + string(rune('a'+i))
			cmd := runHelper(t, "add",
				envHelperDir+"="+groveDir,
				envHelperName+"="+name,
			)
			errs[i] = cmd.Run()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("helper %d failed: %v", i, err)
		}
	}

	got, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != n {
		t.Fatalf("expected %d workspaces after concurrent adds, got %d: %v", n, len(got), names(got))
	}
	seen := map[string]bool{}
	for _, ws := range got {
		if seen[ws.Name] {
			t.Fatalf("duplicate workspace %q", ws.Name)
		}
		seen[ws.Name] = true
	}
}

func names(ws []models.Workspace) []string {
	out := make([]string, len(ws))
	for i := range ws {
		out[i] = ws[i].Name
	}
	return out
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func TestStoreConcurrentSameNameConflict(t *testing.T) {
	dir := t.TempDir()
	groveDir := filepath.Join(dir, ".grove")
	if err := os.MkdirAll(groveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewStore(groveDir)
	if err := os.WriteFile(store.Path, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}

	const n = 5
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cmd := runHelper(t, "add",
				envHelperDir+"="+groveDir,
				envHelperName+"=dup",
			)
			codes[i] = exitCode(cmd.Run())
		}(i)
	}
	wg.Wait()

	success, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case 0:
			success++
		case 4:
			conflict++
		default:
			t.Fatalf("unexpected helper exit code %d (codes=%v)", c, codes)
		}
	}
	if success != 1 {
		t.Fatalf("expected exactly one winner, got %d (codes=%v)", success, codes)
	}
	if conflict != n-1 {
		t.Fatalf("expected %d conflicts, got %d (codes=%v)", n-1, conflict, codes)
	}

	got, _ := store.Load()
	if len(got) != 1 || got[0].Name != "dup" {
		t.Fatalf("expected single 'dup' workspace, got %v", names(got))
	}
}

func TestMutationCreatesGroveDir(t *testing.T) {
	// The grove dir does not exist yet; the first mutation must create it.
	dir := t.TempDir()
	groveDir := filepath.Join(dir, "nonexistent", ".grove")
	store := NewStore(groveDir)

	if err := store.AddWorkspace(models.NewWorkspace("first", "/tmp/first", "main")); err != nil {
		t.Fatalf("first mutation should create grove dir: %v", err)
	}
	got, _ := store.Load()
	if len(got) != 1 {
		t.Fatalf("expected 1 workspace, got %v", names(got))
	}
}

func TestEscapedMutationCannotCommit(t *testing.T) {
	s := testStore(t)
	var escaped *Mutation
	if err := s.WithMutation(context.Background(), func(m *Mutation) error {
		escaped = m
		return nil
	}); err != nil {
		t.Fatalf("mutation: %v", err)
	}
	// After the callback returns the handle is invalid.
	if err := escaped.Add(models.NewWorkspace("x", "/tmp/x", "main")); CodeOf(err) != CodeStateInactiveHandle {
		t.Fatalf("expected inactive-handle error on Add, got %v", err)
	}
	if err := escaped.Commit(); CodeOf(err) != CodeStateInactiveHandle {
		t.Fatalf("expected inactive-handle error on Commit, got %v", err)
	}
}

func TestNestedMutationRejected(t *testing.T) {
	s := testStore(t)
	err := s.WithMutation(context.Background(), func(m *Mutation) error {
		// Calling a public lock-acquiring mutator from inside must fail fast.
		return s.AddWorkspace(models.NewWorkspace("nested", "/tmp/nested", "main"))
	})
	if CodeOf(err) != CodeStateNested {
		t.Fatalf("expected nested-mutation error, got %v", err)
	}
}

func TestCommitWriteFailurePreservesState(t *testing.T) {
	phases := []struct {
		name string
		set  func(s *Store, fail func() error)
	}{
		{"write", func(s *Store, f func() error) { s.failWrite = f }},
		{"sync", func(s *Store, f func() error) { s.failSync = f }},
		{"rename", func(s *Store, f func() error) { s.failRename = f }},
		{"dirsync", func(s *Store, f func() error) { s.failDirSync = f }},
	}
	for _, ph := range phases {
		t.Run(ph.name, func(t *testing.T) {
			s := testStore(t)
			if err := s.AddWorkspace(models.NewWorkspace("keep", "/tmp/keep", "main")); err != nil {
				t.Fatalf("seed: %v", err)
			}
			before, _ := os.ReadFile(s.Path)

			boom := errors.New("inject " + ph.name)
			ph.set(s, func() error { return boom })
			err := s.AddWorkspace(models.NewWorkspace("ghost", "/tmp/ghost", "main"))
			if !errors.Is(err, boom) {
				t.Fatalf("%s: expected injected error, got %v", ph.name, err)
			}
			ph.set(s, nil)

			after, _ := os.ReadFile(s.Path)
			// rename/dirsync happen after (or during) the swap; for rename the old
			// file must remain intact, for dirsync the write already succeeded so we
			// only require valid JSON. In all cases the file must stay valid.
			var ws []models.Workspace
			if jerr := jsonUnmarshal(after, &ws); jerr != nil {
				t.Fatalf("%s: state file corrupt: %v", ph.name, jerr)
			}
			if ph.name == "write" || ph.name == "sync" || ph.name == "rename" {
				if string(before) != string(after) {
					t.Fatalf("%s: state changed despite failure before rename\nbefore=%s\nafter=%s", ph.name, before, after)
				}
			}

			// No leaked temp files regardless of phase.
			assertNoLeakedTemp(t, s)

			// Lock released: a subsequent clean mutation works.
			if err := s.AddWorkspace(models.NewWorkspace("after", "/tmp/after", "main")); err != nil {
				t.Fatalf("%s: mutation after failure should succeed: %v", ph.name, err)
			}
		})
	}
}

func assertNoLeakedTemp(t *testing.T, s *Store) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(s.dir(), ".*.tmp"))
	if len(matches) != 0 {
		t.Fatalf("leaked temp files: %v", matches)
	}
}

func jsonUnmarshal(data []byte, v interface{}) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

func TestStoreMutationFailurePreservesState(t *testing.T) {
	s := testStore(t)
	if err := s.AddWorkspace(models.NewWorkspace("keep", "/tmp/keep", "main")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}

	// Callback edits then returns an error without committing.
	sentinel := errors.New("boom")
	err = s.WithMutation(context.Background(), func(m *Mutation) error {
		_ = m.Add(models.NewWorkspace("ghost", "/tmp/ghost", "main"))
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	after, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("state changed after failed mutation:\nbefore=%s\nafter=%s", before, after)
	}

	// The lock must have been released — a subsequent mutation succeeds.
	if err := s.AddWorkspace(models.NewWorkspace("second", "/tmp/second", "main")); err != nil {
		t.Fatalf("mutation after failure should succeed (lock released): %v", err)
	}
	got, _ := s.Load()
	if len(got) != 2 {
		t.Fatalf("expected keep+second, got %v", names(got))
	}
	for _, ws := range got {
		if ws.Name == "ghost" {
			t.Fatal("uncommitted ghost workspace leaked into state")
		}
	}
}

func TestStateLockReleasedAfterProcessExit(t *testing.T) {
	dir := t.TempDir()
	groveDir := filepath.Join(dir, ".grove")
	if err := os.MkdirAll(groveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewStore(groveDir)
	ready := filepath.Join(dir, "ready")

	cmd := runHelper(t, "hold",
		envHelperDir+"="+groveDir,
		envHelperFile+"="+ready,
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}

	// Wait until the helper reports it holds the lock.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatal("helper never acquired the lock")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Confirm the lock is genuinely held: a short-timeout acquisition fails.
	if l, err := acquireLock(context.Background(), store.lockPath(), 300*time.Millisecond); err == nil {
		_ = l.release()
		_ = cmd.Process.Kill()
		t.Fatal("expected lock to be held by helper")
	} else if !errors.Is(err, errLockTimeout) {
		_ = cmd.Process.Kill()
		t.Fatalf("expected lock timeout, got %v", err)
	}

	// Kill the helper (simulating a crash) and confirm the OS released the lock.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill helper: %v", err)
	}
	_, _ = cmd.Process.Wait()

	l, err := acquireLock(context.Background(), store.lockPath(), 10*time.Second)
	if err != nil {
		t.Fatalf("lock should be acquirable after holder died: %v", err)
	}
	_ = l.release()
}
