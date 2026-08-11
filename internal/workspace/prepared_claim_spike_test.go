package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
)

// These tests are the executable spike for issue #77. The helpers deliberately
// remain test-only until the claim strategy is promoted by the Oven work.
type spikePreparedSlot struct {
	ID           string              `json:"id"`
	Path         string              `json:"path"`
	ManifestPath string              `json:"-"`
	Repos        []spikePreparedRepo `json:"repos"`
}

type spikePreparedRepo struct {
	Name       string `json:"name"`
	SourceRepo string `json:"source_repo"`
	Path       string `json:"path"`
	Commit     string `json:"commit"`
}

type spikeLockProbe struct {
	Attempted func()
	Acquired  func()
}

type spikeClaimedOwnership struct {
	SlotID        string
	WorkspaceName string
	Alias         string
	Branch        string
	Repos         []spikePreparedRepo
}

type spikeClaimHooks struct {
	LockProbe       *spikeLockProbe
	AssignBranch    func(spikePreparedRepo, string) error
	PublishAlias    func(string, string) error
	AfterStep       func(string) error
	SaveWorkspace   func(models.Workspace) error
	RemoveWorkspace func(string) error
}

func prepareSpikeSlot(t *testing.T, env *testEnv, id string, repoNames ...string) spikePreparedSlot {
	t.Helper()
	root := filepath.Join(env.groveDir, "oven-spike", "slots", id)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}

	slot := spikePreparedSlot{
		ID:           id,
		Path:         root,
		ManifestPath: root + ".json",
	}
	for _, name := range repoNames {
		source := env.repoMap[name]
		if source == "" {
			t.Fatalf("missing source repo %s", name)
		}
		ignore := filepath.Join(source, ".gitignore")
		if err := os.WriteFile(ignore, []byte(".venv/\nnode_modules/\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		env.run(source, "git", "add", ".gitignore")
		env.run(source, "git", "commit", "-q", "-m", "ignore prepared dependencies")
		commit := env.run(source, "git", "rev-parse", "HEAD")
		worktreePath := filepath.Join(root, name)
		if _, err := spikeGit(source, "worktree", "add", "--detach", worktreePath, commit); err != nil {
			t.Fatalf("preparing detached worktree %s: %v", name, err)
		}
		slot.Repos = append(slot.Repos, spikePreparedRepo{
			Name:       name,
			SourceRepo: source,
			Path:       worktreePath,
			Commit:     commit,
		})
	}
	data, err := json.MarshalIndent(slot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(slot.ManifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		for _, repo := range slot.Repos {
			_ = gitops.WorktreeRemove(repo.SourceRepo, repo.Path, true)
		}
		_ = os.RemoveAll(slot.Path)
		_ = os.Remove(slot.ManifestPath)
	})
	return slot
}

func claimSpikeSlot(env *testEnv, slot spikePreparedSlot, name, branch string, hooks spikeClaimHooks) error {
	return withSpikeLock(env.svc.State, hooks.LockProbe, func() error {
		if err := preflightSpikeSlot(env, slot, name, branch); err != nil {
			return err
		}

		target := filepath.Join(env.wsDir, name)
		ws := models.NewWorkspace(name, target, branch)
		assigned := make([]spikePreparedRepo, 0, len(slot.Repos))
		aliasCreated := false
		rollback := func(cause error) error {
			return rollbackSpikeClaim(env, slot, ws, assigned, target, branch, aliasCreated, hooks, cause)
		}

		assign := hooks.AssignBranch
		if assign == nil {
			assign = func(repo spikePreparedRepo, branch string) error {
				_, err := spikeGit(repo.Path, "switch", "-c", branch, repo.Commit)
				return err
			}
		}
		for _, repo := range slot.Repos {
			if err := preflightSpikeRepo(slot, repo, branch); err != nil {
				return rollback(err)
			}
			if err := assign(repo, branch); err != nil {
				return rollback(fmt.Errorf("%s: assigning branch: %w", repo.Name, err))
			}
			assigned = append(assigned, repo)
			ws.Repos = append(ws.Repos, models.RepoWorktree{
				RepoName:     repo.Name,
				SourceRepo:   repo.SourceRepo,
				WorktreePath: filepath.Join(target, repo.Name),
				Branch:       branch,
			})
			if hooks.AfterStep != nil {
				if err := hooks.AfterStep("branch:" + repo.Name); err != nil {
					return rollback(err)
				}
			}
		}

		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			return rollback(fmt.Errorf("workspace path changed before publication"))
		}
		publish := hooks.PublishAlias
		if publish == nil {
			publish = os.Symlink
		}
		if err := publish(slot.Path, target); err != nil {
			return rollback(fmt.Errorf("publishing workspace alias: %w", err))
		}
		aliasCreated = true
		if hooks.AfterStep != nil {
			if err := hooks.AfterStep("alias"); err != nil {
				return rollback(err)
			}
		}
		if err := preflightClaimedSpikeFilesystem(slot, target, branch, true); err != nil {
			return rollback(err)
		}

		save := hooks.SaveWorkspace
		if save == nil {
			save = env.svc.State.AddWorkspace
		}
		if err := save(ws); err != nil {
			return rollback(err)
		}
		return nil
	})
}

func rollbackSpikeClaim(
	env *testEnv,
	slot spikePreparedSlot,
	ws models.Workspace,
	assigned []spikePreparedRepo,
	target, branch string,
	aliasCreated bool,
	hooks spikeClaimHooks,
	cause error,
) error {
	current, err := env.svc.State.GetWorkspace(ws.Name)
	if err != nil {
		return errors.Join(cause, fmt.Errorf("reading uncertain state; preserving claim artifacts: %w", err))
	}
	if current != nil {
		if !reflect.DeepEqual(*current, ws) {
			return errors.Join(cause, fmt.Errorf("workspace state changed; preserving claim artifacts"))
		}
		remove := hooks.RemoveWorkspace
		if remove == nil {
			remove = env.svc.State.RemoveWorkspace
		}
		if err := remove(ws.Name); err != nil {
			return errors.Join(cause, fmt.Errorf("state removal uncertain; preserving claim artifacts: %w", err))
		}
	}
	if err := preflightSpikeRollback(slot, assigned, target, branch, aliasCreated); err != nil {
		return errors.Join(cause, err)
	}

	var rollbackErrs []error
	if aliasCreated {
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			rollbackErrs = append(rollbackErrs, err)
		}
	}
	for i := len(assigned) - 1; i >= 0; i-- {
		repo := assigned[i]
		if _, err := spikeGit(repo.Path, "switch", "--detach", repo.Commit); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("%s: detaching rollback: %w", repo.Name, err))
			continue
		}
		if err := gitops.DeleteBranch(repo.SourceRepo, branch, true); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("%s: deleting rollback branch: %w", repo.Name, err))
		}
	}
	return errors.Join(cause, errors.Join(rollbackErrs...))
}

func withSpikeLock(store *state.Store, probe *spikeLockProbe, fn func() error) error {
	if probe != nil && probe.Attempted != nil {
		probe.Attempted()
	}
	return store.WithLock(func() error {
		if probe != nil && probe.Acquired != nil {
			probe.Acquired()
		}
		return fn()
	})
}

func preflightSpikeSlot(env *testEnv, slot spikePreparedSlot, name, branch string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return fmt.Errorf("invalid workspace name %q", name)
	}
	if err := validateTrustedSpikeDescriptor(env, slot); err != nil {
		return err
	}
	data, err := os.ReadFile(slot.ManifestPath)
	if err != nil {
		return fmt.Errorf("reading prepared ownership manifest: %w", err)
	}
	var owned spikePreparedSlot
	if err := json.Unmarshal(data, &owned); err != nil || owned.ID != slot.ID || owned.Path != slot.Path ||
		!reflect.DeepEqual(owned.Repos, slot.Repos) {
		return fmt.Errorf("prepared ownership does not match trusted inventory")
	}
	if existing, err := env.svc.State.GetWorkspace(name); err != nil {
		return err
	} else if existing != nil {
		return fmt.Errorf("workspace %s already exists", name)
	}
	target := filepath.Join(env.wsDir, name)
	if _, err := os.Lstat(target); err == nil {
		return fmt.Errorf("workspace path already exists: %s", target)
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, repo := range slot.Repos {
		if err := preflightSpikeRepo(slot, repo, branch); err != nil {
			return err
		}
	}
	return nil
}

func validateTrustedSpikeDescriptor(env *testEnv, slot spikePreparedSlot) error {
	absolute, err := filepath.Abs(slot.Path)
	if err != nil || absolute != filepath.Clean(slot.Path) {
		return fmt.Errorf("backing path is not absolute and clean")
	}
	trustedRoot := filepath.Join(env.groveDir, "oven-spike")
	relative, err := filepath.Rel(trustedRoot, slot.Path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("backing path escapes trusted Oven root")
	}
	current := trustedRoot
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backing path contains an untrusted symlink or is missing")
		}
	}
	allowedSources := make(map[string]string, len(env.repoMap))
	for name, source := range env.repoMap {
		allowedSources[name] = canonicalPath(source)
	}
	seen := make(map[string]bool, len(slot.Repos))
	for _, repo := range slot.Repos {
		if repo.Name == "" || repo.Name == "." || repo.Name == ".." || filepath.Base(repo.Name) != repo.Name || seen[repo.Name] {
			return fmt.Errorf("unsafe or duplicate prepared repository name %q", repo.Name)
		}
		seen[repo.Name] = true
		if canonicalPath(repo.SourceRepo) != allowedSources[repo.Name] {
			return fmt.Errorf("%s: source repository is not trusted", repo.Name)
		}
		if filepath.Dir(repo.Path) != slot.Path || filepath.Base(repo.Path) != repo.Name {
			return fmt.Errorf("%s: worktree path escapes backing root", repo.Name)
		}
	}
	return nil
}

func preflightSpikeRepo(slot spikePreparedSlot, repo spikePreparedRepo, branch string) error {
	if filepath.Dir(repo.Path) != slot.Path || filepath.Base(repo.Path) != repo.Name {
		return fmt.Errorf("%s: worktree path escapes backing root", repo.Name)
	}
	if gitops.BranchExists(repo.SourceRepo, branch) {
		return fmt.Errorf("%s: branch %s already exists", repo.Name, branch)
	}
	entries, err := gitops.WorktreeList(repo.SourceRepo)
	if err != nil {
		return err
	}
	registeredDetached := false
	for _, entry := range entries {
		if canonicalPath(entry.Path) == canonicalPath(repo.Path) && entry.Branch == "" {
			registeredDetached = true
			break
		}
	}
	if !registeredDetached {
		return fmt.Errorf("%s: prepared worktree identity changed", repo.Name)
	}
	currentBranch, err := gitops.CurrentBranch(repo.Path)
	if err != nil || currentBranch != "" {
		return fmt.Errorf("%s: prepared worktree is not detached", repo.Name)
	}
	head, err := spikeGit(repo.Path, "rev-parse", "HEAD")
	if err != nil || head != repo.Commit {
		return fmt.Errorf("%s: prepared commit changed", repo.Name)
	}
	if _, err := spikeGit(repo.Path, "diff", "--quiet"); err != nil {
		return fmt.Errorf("%s: prepared tracked files changed", repo.Name)
	}
	if _, err := spikeGit(repo.Path, "diff", "--cached", "--quiet"); err != nil {
		return fmt.Errorf("%s: prepared index changed", repo.Name)
	}
	return nil
}

func preflightClaimedSpikeFilesystem(slot spikePreparedSlot, alias, branch string, requirePreparedCommit bool) error {
	target, err := os.Readlink(alias)
	if err != nil || canonicalPath(target) != canonicalPath(slot.Path) {
		return fmt.Errorf("claimed alias does not resolve to its owned backing root")
	}
	for _, repo := range slot.Repos {
		branchAtPath, err := gitops.CurrentBranch(filepath.Join(alias, repo.Name))
		if err != nil || branchAtPath != branch {
			return fmt.Errorf("%s: claimed branch identity changed", repo.Name)
		}
		if requirePreparedCommit {
			head, err := spikeGit(filepath.Join(alias, repo.Name), "rev-parse", "HEAD")
			if err != nil || head != repo.Commit {
				return fmt.Errorf("%s: claimed commit identity changed", repo.Name)
			}
		}
		entries, err := gitops.WorktreeList(repo.SourceRepo)
		if err != nil {
			return err
		}
		registered := false
		for _, entry := range entries {
			if canonicalPath(entry.Path) == canonicalPath(repo.Path) && entry.Branch == branch {
				registered = true
				break
			}
		}
		if !registered {
			return fmt.Errorf("%s: claimed worktree registration changed", repo.Name)
		}
	}
	return nil
}

func preflightSpikeRollback(slot spikePreparedSlot, assigned []spikePreparedRepo, alias, branch string, aliasCreated bool) error {
	if aliasCreated {
		target, err := os.Readlink(alias)
		if err != nil || canonicalPath(target) != canonicalPath(slot.Path) {
			return fmt.Errorf("claim alias identity changed; preserving artifacts")
		}
	}
	for _, repo := range assigned {
		currentBranch, err := gitops.CurrentBranch(repo.Path)
		if err != nil || currentBranch != branch {
			return fmt.Errorf("%s: rollback branch identity changed; preserving artifacts", repo.Name)
		}
		head, err := spikeGit(repo.Path, "rev-parse", "HEAD")
		if err != nil || head != repo.Commit {
			return fmt.Errorf("%s: rollback commit identity changed; preserving artifacts", repo.Name)
		}
	}
	return nil
}

func discardSpikeSlot(env *testEnv, slot spikePreparedSlot, probe ...*spikeLockProbe) error {
	var lockProbe *spikeLockProbe
	if len(probe) > 0 {
		lockProbe = probe[0]
	}
	return withSpikeLock(env.svc.State, lockProbe, func() error {
		if err := preflightSpikeSlot(env, slot, "discard-probe", "discard-probe"); err != nil {
			return err
		}
		for _, repo := range slot.Repos {
			if err := preflightSpikeRepo(slot, repo, "discard-probe"); err != nil {
				return err
			}
			if err := gitops.WorktreeRemove(repo.SourceRepo, repo.Path, true); err != nil {
				return err
			}
		}
		if err := os.Remove(slot.Path); err != nil {
			return err
		}
		return os.Remove(slot.ManifestPath)
	})
}

func claimedSpikeOwnership(env *testEnv, slot spikePreparedSlot, name, branch string) spikeClaimedOwnership {
	return spikeClaimedOwnership{
		SlotID:        slot.ID,
		WorkspaceName: name,
		Alias:         filepath.Join(env.wsDir, name),
		Branch:        branch,
		Repos:         append([]spikePreparedRepo(nil), slot.Repos...),
	}
}

func deleteClaimedSpike(env *testEnv, slot spikePreparedSlot, owned spikeClaimedOwnership) error {
	return env.svc.State.WithLock(func() error {
		ws, err := env.svc.State.GetWorkspace(owned.WorkspaceName)
		if err != nil || ws == nil {
			return errors.Join(err, fmt.Errorf("claimed workspace %s not found", owned.WorkspaceName))
		}
		if err := validateTrustedSpikeDescriptor(env, slot); err != nil {
			return err
		}
		if owned.SlotID != slot.ID {
			return fmt.Errorf("claimed slot identity does not match trusted inventory")
		}
		if err := preflightClaimedSpikeState(*ws, owned); err != nil {
			return err
		}
		if err := preflightClaimedSpikeFilesystem(slot, owned.Alias, owned.Branch, false); err != nil {
			return err
		}
		if _, err := env.svc.deleteLocked(owned.WorkspaceName, RemoveOptions{}); err != nil {
			return err
		}
		if err := os.Remove(slot.Path); err != nil {
			return fmt.Errorf("removing empty backing root: %w", err)
		}
		return os.Remove(slot.ManifestPath)
	})
}

func preflightClaimedSpikeState(ws models.Workspace, owned spikeClaimedOwnership) error {
	if owned.SlotID == "" || ws.Name != owned.WorkspaceName || ws.Path != owned.Alias ||
		ws.Branch != owned.Branch || len(ws.Repos) != len(owned.Repos) {
		return fmt.Errorf("claimed workspace state does not match its ownership record")
	}
	expected := make(map[string]spikePreparedRepo, len(owned.Repos))
	for _, repo := range owned.Repos {
		expected[repo.Name] = repo
	}
	seen := make(map[string]bool, len(ws.Repos))
	for _, repo := range ws.Repos {
		owned, ok := expected[repo.RepoName]
		if !ok || seen[repo.RepoName] || repo.SourceRepo != owned.SourceRepo ||
			repo.WorktreePath != filepath.Join(ws.Path, repo.RepoName) || repo.Branch != ws.Branch {
			return fmt.Errorf("claimed workspace repository state does not match its ownership record")
		}
		seen[repo.RepoName] = true
	}
	return nil
}

func spikeGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return strings.TrimSpace(string(out)), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func createPathSensitiveProbes(t *testing.T, repoPath string) {
	t.Helper()
	pythonBin := filepath.Join(repoPath, ".venv", "bin")
	if err := os.MkdirAll(pythonBin, 0o755); err != nil {
		t.Fatal(err)
	}
	pythonShim := filepath.Join(pythonBin, "python")
	if err := os.WriteFile(pythonShim, []byte("#!/bin/sh\nexec /bin/sh \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	pythonPayload := filepath.Join(pythonBin, "prepared-python-tool.payload")
	if err := os.WriteFile(pythonPayload, []byte("echo python-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pythonTool := filepath.Join(pythonBin, "prepared-python-tool")
	launcher := fmt.Sprintf("#!/bin/sh\nexec %q %q\n", pythonShim, pythonPayload)
	if err := os.WriteFile(pythonTool, []byte(launcher), 0o755); err != nil {
		t.Fatal(err)
	}

	packageDir := filepath.Join(repoPath, "node_modules", "probe-package")
	binDir := filepath.Join(repoPath, "node_modules", ".bin")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "tool"), []byte("#!/bin/sh\necho bun-ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../probe-package/tool", filepath.Join(binDir, "prepared-bun-tool")); err != nil {
		t.Fatal(err)
	}
}

func runPreparedProbe(path string) (string, error) {
	out, err := exec.Command(path).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func spikePathSnapshot(path string) (string, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	var data []byte
	if info.Mode().IsRegular() {
		data, err = os.ReadFile(path)
		if err != nil {
			return "", err
		}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return "", err
		}
		data = []byte(target)
	}
	return fmt.Sprintf("%s:%d:%s", info.Mode(), info.Size(), data), nil
}

func TestPreparedClaimSpikeStableBackingPath(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	slot := prepareSpikeSlot(t, env, "slot-stable", "api", "web")
	for _, repo := range slot.Repos {
		createPathSensitiveProbes(t, repo.Path)
	}
	if err := claimSpikeSlot(env, slot, "cake", "feat/cake", spikeClaimHooks{
		AfterStep: assertSpikeStateLast(env, "cake"),
	}); err != nil {
		t.Fatal(err)
	}

	ws := requireSpikeWorkspace(t, env, slot, "cake")
	assertPreparedToolsThroughAlias(t, ws)
	exerciseClaimedGit(t, env, ws)
	if err := deleteClaimedSpike(env, slot, claimedSpikeOwnership(env, slot, "cake", "feat/cake")); err != nil {
		t.Fatalf("ownership-aware normal workspace delete: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(env.wsDir, "cake")); !os.IsNotExist(err) {
		t.Fatalf("workspace alias remained after delete: %v", err)
	}
}

func assertSpikeStateLast(env *testEnv, name string) func(string) error {
	return func(step string) error {
		if step != "alias" {
			return nil
		}
		visible, err := env.svc.State.GetWorkspace(name)
		if err != nil || visible != nil {
			return fmt.Errorf("workspace became visible before final state write")
		}
		return nil
	}
}

func requireSpikeWorkspace(t *testing.T, env *testEnv, slot spikePreparedSlot, name string) *models.Workspace {
	t.Helper()
	ws, err := env.svc.State.GetWorkspace(name)
	if err != nil || ws == nil {
		t.Fatalf("claimed workspace missing: %+v, %v", ws, err)
	}
	if target, err := os.Readlink(ws.Path); err != nil || target != slot.Path {
		t.Fatalf("workspace alias = %q, %v; want %q", target, err, slot.Path)
	}
	return ws
}

func assertPreparedToolsThroughAlias(t *testing.T, ws *models.Workspace) {
	t.Helper()
	for _, repo := range ws.Repos {
		if branch, err := gitops.CurrentBranch(repo.WorktreePath); err != nil || branch != ws.Branch {
			t.Fatalf("%s branch = %q, %v", repo.RepoName, branch, err)
		}
		pythonOut, err := runPreparedProbe(filepath.Join(repo.WorktreePath, ".venv", "bin", "prepared-python-tool"))
		if err != nil || pythonOut != "python-ok" {
			t.Fatalf("%s prepared Python probe = %q, %v", repo.RepoName, pythonOut, err)
		}
		bunOut, err := runPreparedProbe(filepath.Join(repo.WorktreePath, "node_modules", ".bin", "prepared-bun-tool"))
		if err != nil || bunOut != "bun-ok" {
			t.Fatalf("%s prepared Bun probe = %q, %v", repo.RepoName, bunOut, err)
		}
	}
}

func exerciseClaimedGit(t *testing.T, env *testEnv, ws *models.Workspace) {
	t.Helper()
	api := ws.Repos[0].WorktreePath
	if err := os.WriteFile(filepath.Join(api, "claimed.txt"), []byte("normal git works"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := spikeGit(api, "add", "claimed.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := spikeGit(api, "commit", "-m", "claim works"); err != nil {
		t.Fatal(err)
	}
	assertClaimedSpikeStatus(t, env, ws)
}

func assertClaimedSpikeStatus(t *testing.T, env *testEnv, ws *models.Workspace) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	statusErr := env.svc.Status(ws.Name, StatusOptions{JSON: true})
	_ = writer.Close()
	os.Stdout = originalStdout
	data, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if statusErr != nil || readErr != nil {
		t.Fatalf("normal workspace status through alias: %v; reading output: %v", statusErr, readErr)
	}
	var status struct {
		Workspace string `json:"workspace"`
		Repos     []struct {
			Repo   string `json:"repo"`
			Branch string `json:"branch"`
			Status string `json:"status"`
		} `json:"repos"`
	}
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("parsing status JSON: %v\n%s", err, data)
	}
	if status.Workspace != ws.Name || len(status.Repos) != len(ws.Repos) {
		t.Fatalf("status identity mismatch: %+v", status)
	}
	expected := make(map[string]bool, len(ws.Repos))
	for _, repo := range ws.Repos {
		expected[repo.RepoName] = true
	}
	for _, repo := range status.Repos {
		if !expected[repo.Repo] || repo.Branch != ws.Branch || repo.Status != "clean" {
			t.Fatalf("%s status through alias = branch %q, status %q", repo.Repo, repo.Branch, repo.Status)
		}
	}
}

func TestPreparedClaimSpikeOwnershipAwareDeleteRejectsAliasTampering(t *testing.T) {
	for _, tamper := range []string{"dangling-alias", "retargeted-alias", "replaced-worktree", "extra-state-repo"} {
		t.Run(tamper, func(t *testing.T) {
			env := setupTestEnv(t)
			env.createRepo("api")
			slot := prepareSpikeSlot(t, env, "slot-tamper", "api")
			if err := claimSpikeSlot(env, slot, "claimed", "feat/claimed", spikeClaimHooks{}); err != nil {
				t.Fatal(err)
			}
			restore := applySpikeTamper(t, env, slot, tamper)
			assertSpikeDeleteRefused(t, env, slot)
			restore()
			if err := deleteClaimedSpike(env, slot, claimedSpikeOwnership(env, slot, "claimed", "feat/claimed")); err != nil {
				t.Fatalf("delete after restoring ownership: %v", err)
			}
		})
	}
}

func applySpikeTamper(t *testing.T, env *testEnv, slot spikePreparedSlot, tamper string) func() {
	t.Helper()
	alias := filepath.Join(env.wsDir, "claimed")
	switch tamper {
	case "dangling-alias":
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		return func() {
			if err := os.Symlink(slot.Path, alias); err != nil {
				t.Fatal(err)
			}
		}
	case "retargeted-alias":
		return retargetSpikeAlias(t, env, slot, alias)
	case "replaced-worktree":
		return replaceSpikeWorktree(t, slot)
	case "extra-state-repo":
		return addSpikeStateRepo(t, env, slot)
	default:
		t.Fatalf("unknown tamper case %s", tamper)
		return func() {}
	}
}

func retargetSpikeAlias(t *testing.T, env *testEnv, slot spikePreparedSlot, alias string) func() {
	t.Helper()
	replacement := filepath.Join(env.dir, "replacement")
	if err := os.Mkdir(replacement, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(replacement, "sentinel"), []byte("preserve"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(replacement, alias); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := os.Stat(filepath.Join(replacement, "sentinel")); err != nil {
			t.Fatalf("replacement target was mutated: %v", err)
		}
		if err := os.Remove(alias); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(slot.Path, alias); err != nil {
			t.Fatal(err)
		}
	}
}

func replaceSpikeWorktree(t *testing.T, slot spikePreparedSlot) func() {
	t.Helper()
	original := slot.Repos[0].Path
	saved := original + ".saved"
	if err := os.Rename(original, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(original, "sentinel"), []byte("preserve"), 0o644); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := os.Stat(filepath.Join(original, "sentinel")); err != nil {
			t.Fatalf("replacement worktree was mutated: %v", err)
		}
		if _, err := os.Stat(saved); err != nil {
			t.Fatalf("original worktree was mutated: %v", err)
		}
		if err := os.RemoveAll(original); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(saved, original); err != nil {
			t.Fatal(err)
		}
	}
}

func addSpikeStateRepo(t *testing.T, env *testEnv, slot spikePreparedSlot) func() {
	t.Helper()
	source := env.createRepo("extra")
	branch := "feat/claimed"
	if err := gitops.CreateBranch(source, branch, "HEAD"); err != nil {
		t.Fatal(err)
	}
	physicalPath := filepath.Join(slot.Path, "extra")
	if err := gitops.WorktreeAdd(source, physicalPath, branch); err != nil {
		t.Fatal(err)
	}
	ws, err := env.svc.State.GetWorkspace("claimed")
	if err != nil || ws == nil {
		t.Fatalf("reading claimed state: %+v, %v", ws, err)
	}
	ws.Repos = append(ws.Repos, models.RepoWorktree{
		RepoName:     "extra",
		SourceRepo:   source,
		WorktreePath: filepath.Join(ws.Path, "extra"),
		Branch:       branch,
	})
	if err := env.svc.State.UpdateWorkspace(*ws); err != nil {
		t.Fatal(err)
	}
	return func() {
		if _, err := os.Stat(physicalPath); err != nil {
			t.Fatalf("unexpected valid repository was mutated: %v", err)
		}
		current, err := env.svc.State.GetWorkspace("claimed")
		if err != nil || current == nil {
			t.Fatalf("reading tampered state: %+v, %v", current, err)
		}
		current.RemoveRepo("extra")
		if err := env.svc.State.UpdateWorkspace(*current); err != nil {
			t.Fatal(err)
		}
		if err := gitops.WorktreeRemove(source, physicalPath, true); err != nil {
			t.Fatal(err)
		}
		if err := gitops.DeleteBranch(source, branch, true); err != nil {
			t.Fatal(err)
		}
	}
}

func assertSpikeDeleteRefused(t *testing.T, env *testEnv, slot spikePreparedSlot) {
	t.Helper()
	if err := deleteClaimedSpike(env, slot, claimedSpikeOwnership(env, slot, "claimed", "feat/claimed")); err == nil {
		t.Fatal("ownership-aware delete accepted tampered claim")
	}
	if ws, err := env.svc.State.GetWorkspace("claimed"); err != nil || ws == nil {
		t.Fatalf("tampered claim state was removed: %+v, %v", ws, err)
	}
	if _, err := os.Stat(slot.Path); err != nil {
		t.Fatalf("owned backing root was removed: %v", err)
	}
}

func TestPreparedClaimSpikeRelocationBreaksPythonStylePaths(t *testing.T) {
	env := setupTestEnv(t)
	source := env.createRepo("api")
	slot := prepareSpikeSlot(t, env, "slot-move", "api")
	oldRepo := slot.Repos[0].Path
	createPathSensitiveProbes(t, oldRepo)
	if out, err := runPreparedProbe(filepath.Join(oldRepo, ".venv", "bin", "prepared-python-tool")); err != nil || out != "python-ok" {
		t.Fatalf("probe before move = %q, %v", out, err)
	}

	movedRoot := slot.Path + "-moved"
	if err := os.Rename(slot.Path, movedRoot); err != nil {
		t.Fatal(err)
	}
	movedRepo := filepath.Join(movedRoot, "api")
	if err := gitops.WorktreeRepair(source, movedRepo); err != nil {
		t.Fatalf("Git repair after relocation: %v", err)
	}
	if _, err := gitops.RepoStatus(movedRepo); err != nil {
		t.Fatalf("Git operation after repair: %v", err)
	}
	if out, err := runPreparedProbe(filepath.Join(movedRepo, "node_modules", ".bin", "prepared-bun-tool")); err != nil || out != "bun-ok" {
		t.Fatalf("relative Bun-style probe after move = %q, %v", out, err)
	}
	if out, err := runPreparedProbe(filepath.Join(movedRepo, ".venv", "bin", "prepared-python-tool")); err == nil {
		t.Fatalf("absolute Python-style probe unexpectedly survived relocation: %q", out)
	}

	if err := gitops.WorktreeRemove(source, movedRepo, true); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(movedRoot); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(slot.ManifestPath)
}

type spikePreflightSnapshot struct {
	head, branch, target string
	manifest             []byte
	claimBranch          bool
}

func TestPreparedClaimSpikePreflightFailuresDoNotMutate(t *testing.T) {
	cases := map[string]func(*testing.T, *testEnv, spikePreparedSlot){
		"ownership":     mutateSpikeOwnership,
		"commit":        mutateSpikeCommit,
		"tracked-files": mutateSpikeTrackedFiles,
		"target-path":   mutateSpikeTargetPath,
		"branch":        mutateSpikeBranch,
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			env := setupTestEnv(t)
			env.createRepo("api")
			slot := prepareSpikeSlot(t, env, "slot-preflight", "api")
			mutate(t, env, slot)
			before := captureSpikePreflightSnapshot(t, env, slot)
			if err := claimSpikeSlot(env, slot, "claimed", "feat/claimed", spikeClaimHooks{}); err == nil {
				t.Fatal("expected claim preflight failure")
			}
			if ws, err := env.svc.State.GetWorkspace("claimed"); err != nil || ws != nil {
				t.Fatalf("failed preflight became visible: %+v, %v", ws, err)
			}
			after := captureSpikePreflightSnapshot(t, env, slot)
			if !reflect.DeepEqual(after, before) {
				t.Fatal("claim preflight mutated the prepared slot")
			}
		})
	}
}

func mutateSpikeOwnership(t *testing.T, _ *testEnv, slot spikePreparedSlot) {
	data, err := os.ReadFile(slot.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), slot.ID, "different-owner", 1))
	if err := os.WriteFile(slot.ManifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mutateSpikeCommit(t *testing.T, _ *testEnv, slot spikePreparedSlot) {
	if _, err := spikeGit(slot.Repos[0].Path, "switch", "--detach", "HEAD^"); err != nil {
		t.Fatal(err)
	}
}

func mutateSpikeTrackedFiles(t *testing.T, _ *testEnv, slot spikePreparedSlot) {
	if err := os.WriteFile(filepath.Join(slot.Repos[0].Path, "README.md"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateSpikeTargetPath(t *testing.T, env *testEnv, _ spikePreparedSlot) {
	if err := os.WriteFile(filepath.Join(env.wsDir, "claimed"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mutateSpikeBranch(t *testing.T, _ *testEnv, slot spikePreparedSlot) {
	if err := gitops.CreateBranch(slot.Repos[0].SourceRepo, "feat/claimed", slot.Repos[0].Commit); err != nil {
		t.Fatal(err)
	}
}

func captureSpikePreflightSnapshot(t *testing.T, env *testEnv, slot spikePreparedSlot) spikePreflightSnapshot {
	t.Helper()
	head, err := spikeGit(slot.Repos[0].Path, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	branch, err := gitops.CurrentBranch(slot.Repos[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(slot.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	target, err := spikePathSnapshot(filepath.Join(env.wsDir, "claimed"))
	if err != nil {
		t.Fatal(err)
	}
	return spikePreflightSnapshot{
		head:        head,
		branch:      branch,
		target:      target,
		manifest:    manifest,
		claimBranch: gitops.BranchExists(slot.Repos[0].SourceRepo, "feat/claimed"),
	}
}

func TestPreparedClaimSpikeRollsBackEveryFallibleClaimStep(t *testing.T) {
	for _, failStep := range []string{"assign:api", "assign:web", "publish", "publish-race", "state", "state-uncertain"} {
		t.Run(failStep, func(t *testing.T) {
			env := setupTestEnv(t)
			env.createRepo("api")
			env.createRepo("web")
			slot := prepareSpikeSlot(t, env, "slot-failure", "api", "web")
			for _, repo := range slot.Repos {
				createPathSensitiveProbes(t, repo.Path)
			}
			injected := errors.New("injected " + failStep + " failure")
			hooks := spikeFailureHooks(env, failStep, injected)
			if err := claimSpikeSlot(env, slot, "failed", "feat/failed", hooks); !errors.Is(err, injected) {
				t.Fatalf("claim error = %v, want injected failure", err)
			}
			assertRolledBackSpike(t, env, slot, failStep)
			if err := discardSpikeSlot(env, slot); err != nil {
				t.Fatalf("discarding rolled-back slot: %v", err)
			}
		})
	}
}

func spikeFailureHooks(env *testEnv, failStep string, injected error) spikeClaimHooks {
	hooks := spikeClaimHooks{}
	if strings.HasPrefix(failStep, "assign:") {
		failedRepo := strings.TrimPrefix(failStep, "assign:")
		hooks.AssignBranch = func(repo spikePreparedRepo, branch string) error {
			if repo.Name == failedRepo {
				return injected
			}
			_, err := spikeGit(repo.Path, "switch", "-c", branch, repo.Commit)
			return err
		}
	}
	if failStep == "publish" {
		hooks.PublishAlias = func(string, string) error { return injected }
	}
	if failStep == "publish-race" {
		hooks.PublishAlias = func(_, target string) error {
			if err := os.WriteFile(target, []byte("external"), 0o644); err != nil {
				return err
			}
			return injected
		}
	}
	if failStep == "state" {
		hooks.SaveWorkspace = func(models.Workspace) error { return injected }
	}
	if failStep == "state-uncertain" {
		hooks.SaveWorkspace = func(ws models.Workspace) error {
			if err := env.svc.State.AddWorkspace(ws); err != nil {
				return err
			}
			return injected
		}
	}
	return hooks
}

func assertRolledBackSpike(t *testing.T, env *testEnv, slot spikePreparedSlot, failStep string) {
	t.Helper()
	ws, stateErr := env.svc.State.GetWorkspace("failed")
	if stateErr != nil || ws != nil {
		t.Fatalf("failed claim became visible: %+v, %v", ws, stateErr)
	}
	target := filepath.Join(env.wsDir, "failed")
	if failStep == "publish-race" {
		data, err := os.ReadFile(target)
		if err != nil || string(data) != "external" {
			t.Fatalf("external publication race resource was not preserved: %q, %v", data, err)
		}
	} else if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("failed alias remained: %v", err)
	}
	for _, repo := range slot.Repos {
		if branch, err := gitops.CurrentBranch(repo.Path); err != nil || branch != "" {
			t.Fatalf("%s branch after rollback = %q, %v", repo.Name, branch, err)
		}
		if gitops.BranchExists(repo.SourceRepo, "feat/failed") {
			t.Fatalf("%s rollback branch remained", repo.Name)
		}
		if head, err := spikeGit(repo.Path, "rev-parse", "HEAD"); err != nil || head != repo.Commit {
			t.Fatalf("%s HEAD after rollback = %q, %v", repo.Name, head, err)
		}
		if out, err := runPreparedProbe(filepath.Join(repo.Path, ".venv", "bin", "prepared-python-tool")); err != nil || out != "python-ok" {
			t.Fatalf("%s prepared artifacts damaged: %q, %v", repo.Name, out, err)
		}
	}
}

func TestPreparedClaimSpikePreservesArtifactsWhenStateRemovalIsUncertain(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	slot := prepareSpikeSlot(t, env, "slot-state-uncertain", "api")
	saveErr := errors.New("uncertain state write")
	removeErr := errors.New("state removal failed")
	err := claimSpikeSlot(env, slot, "quarantined", "feat/quarantined", spikeClaimHooks{
		SaveWorkspace: func(ws models.Workspace) error {
			if err := env.svc.State.AddWorkspace(ws); err != nil {
				return err
			}
			return saveErr
		},
		RemoveWorkspace: func(string) error { return removeErr },
	})
	if !errors.Is(err, saveErr) || !errors.Is(err, removeErr) {
		t.Fatalf("claim error = %v, want state uncertainty errors", err)
	}
	if ws, stateErr := env.svc.State.GetWorkspace("quarantined"); stateErr != nil || ws == nil {
		t.Fatalf("uncertain state was not preserved: %+v, %v", ws, stateErr)
	}
	if target, linkErr := os.Readlink(filepath.Join(env.wsDir, "quarantined")); linkErr != nil || canonicalPath(target) != canonicalPath(slot.Path) {
		t.Fatalf("claim alias was destructively rolled back: %q, %v", target, linkErr)
	}
	if branch, branchErr := gitops.CurrentBranch(slot.Repos[0].Path); branchErr != nil || branch != "feat/quarantined" {
		t.Fatalf("claim branch was destructively rolled back: %q, %v", branch, branchErr)
	}
	if err := deleteClaimedSpike(env, slot, claimedSpikeOwnership(env, slot, "quarantined", "feat/quarantined")); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedClaimSpikeSerializesClaimAndDiscard(t *testing.T) {
	for _, contender := range []string{"claim", "discard"} {
		t.Run(contender, func(t *testing.T) {
			env := setupTestEnv(t)
			env.createRepo("api")
			slot := prepareSpikeSlot(t, env, "slot-race", "api")
			entered := make(chan struct{})
			release := make(chan struct{})
			var once sync.Once
			claimDone := make(chan error, 1)
			go func() {
				claimDone <- claimSpikeSlot(env, slot, "winner", "feat/winner", spikeClaimHooks{
					AfterStep: func(step string) error {
						if step == "branch:api" {
							once.Do(func() { close(entered) })
							<-release
						}
						return nil
					},
				})
			}()
			<-entered

			contenderEnv := *env
			contenderService := *env.svc
			contenderService.State = state.NewStore(env.groveDir)
			contenderEnv.svc = &contenderService
			attempted := make(chan struct{})
			acquired := make(chan struct{})
			probe := &spikeLockProbe{
				Attempted: func() { close(attempted) },
				Acquired:  func() { close(acquired) },
			}
			contenderDone := make(chan error, 1)
			go func() {
				if contender == "claim" {
					contenderDone <- claimSpikeSlot(&contenderEnv, slot, "loser", "feat/loser", spikeClaimHooks{LockProbe: probe})
					return
				}
				contenderDone <- discardSpikeSlot(&contenderEnv, slot, probe)
			}()
			<-attempted
			select {
			case <-acquired:
				t.Fatalf("%s acquired the cross-store lock before the claim released it", contender)
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			if err := <-claimDone; err != nil {
				t.Fatalf("winning claim: %v", err)
			}
			if err := <-contenderDone; err == nil {
				t.Fatalf("concurrent %s unexpectedly succeeded", contender)
			}
			if winner, _ := env.svc.State.GetWorkspace("winner"); winner == nil {
				t.Fatal("winning claim missing from state")
			}
			if loser, _ := env.svc.State.GetWorkspace("loser"); loser != nil {
				t.Fatalf("losing claim visible: %+v", loser)
			}
			if err := deleteClaimedSpike(env, slot, claimedSpikeOwnership(env, slot, "winner", "feat/winner")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
