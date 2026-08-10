package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/recipe"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

func TestRunRecipeCreateSuccessJSON(t *testing.T) {
	env := setupRecipeCreateCommand(t, "printf ready > ready.txt")
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	if err := runRecipeCreate(cmd, []string{"cake"}); err != nil {
		t.Fatalf("runRecipeCreate: %v\nstderr: %s", err, stderr.String())
	}
	var output recipeCreateOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, stdout.String())
	}
	if !output.Created || output.Name != "cake" || output.Jobs == nil || len(*output.Jobs) != 1 {
		t.Fatalf("unexpected output: %+v", output)
	}
	if _, err := os.Stat(filepath.Join(env.workspaceDir, "cake", "api", "ready.txt")); err != nil {
		t.Fatalf("Recipe step output missing: %v", err)
	}
	store := state.NewStore(env.groveDir)
	if ws, _ := store.GetWorkspace("cake"); ws == nil {
		t.Fatal("workspace not registered")
	}
}

func TestRunRecipeCreatePostCreateAbortReportsCreatedWorkspace(t *testing.T) {
	env := setupRecipeCreateCommand(t, "true")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hooks = map[string]models.Hook{"post_create": {Command: "exit 9", OnFailure: "abort"}}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err = runRecipeCreate(cmd, []string{"hook-cake"})
	if err == nil {
		t.Fatal("expected post_create failure")
	}
	var output recipeCreateOutput
	if decodeErr := json.Unmarshal(stdout.Bytes(), &output); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if !output.Created || output.Path == "" || output.Error == nil || output.Error.Code != "post_create_failed" {
		t.Fatalf("unexpected output: %+v", output)
	}
	if ws, _ := state.NewStore(env.groveDir).GetWorkspace("hook-cake"); ws == nil {
		t.Fatal("post_create abort incorrectly removed completed workspace")
	}
}

func TestRunRecipeCreateFailureHumanIdentifiesJobAndStep(t *testing.T) {
	setupRecipeCreateCommand(t, "exit 4")
	createJSON = false
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := runRecipeCreate(cmd, []string{"failed-human"})
	if err == nil || !strings.Contains(err.Error(), "Recipe job setup step 1 (Prepare)") {
		t.Fatalf("error is not actionable: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected human stdout: %q", stdout.String())
	}
}

func TestRunRecipeCreateFailureJSONRollsBack(t *testing.T) {
	env := setupRecipeCreateCommand(t, "printf dirty > generated.txt; exit 7")
	createBranch = "feat/failing-recipe"
	var stdout, stderr bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)

	err := runRecipeCreate(cmd, []string{"failed-cake"})
	var runErr *recipeCreateRunError
	if !errors.As(err, &runErr) {
		t.Fatalf("error = %v, want recipeCreateRunError", err)
	}
	var output recipeCreateOutput
	if decodeErr := json.Unmarshal(stdout.Bytes(), &output); decodeErr != nil {
		t.Fatalf("invalid JSON: %v\n%s", decodeErr, stdout.String())
	}
	if output.Created || output.Error == nil || output.Error.Code != recipe.ErrorStepFailed || output.Error.Job != "setup" || output.Error.Step != 1 {
		t.Fatalf("unexpected output: %+v", output)
	}
	store := state.NewStore(env.groveDir)
	if ws, _ := store.GetWorkspace("failed-cake"); ws != nil {
		t.Fatalf("failed workspace remained registered: %+v", ws)
	}
	if _, err := os.Stat(filepath.Join(env.workspaceDir, "failed-cake")); !os.IsNotExist(err) {
		t.Fatalf("failed workspace directory remained: %v", err)
	}
	if gitops.BranchExists(env.repoPath, createBranch) {
		t.Fatal("owned branch remained after rollback")
	}
}

func TestRecipeCreateFailurePreservesTimeoutAndCleanupDetails(t *testing.T) {
	executionErr := &recipe.ExecutionError{Code: recipe.ErrorJobTimeout, Job: "slow", Step: 2, StepName: "Build", Err: errors.New("timed out")}
	failure := recipeCreateFailureFromError(&workspace.PreparationError{Cause: executionErr, CleanupErr: errors.New("cleanup refused")})
	if failure.Code != recipe.ErrorJobTimeout || failure.Job != "slow" || failure.Step != 2 || failure.CleanupError != "cleanup refused" {
		t.Fatalf("unexpected failure: %+v", failure)
	}
}

func TestValidateRecipeCreateOptions(t *testing.T) {
	valid := recipeCreateOptions{Recipe: "recipe.yaml"}
	if err := validateRecipeCreateOptions(valid); err != nil {
		t.Fatalf("valid options: %v", err)
	}
	conflicts := []recipeCreateOptions{
		{Recipe: "recipe.yaml", Repos: "api"},
		{Recipe: "recipe.yaml", Preset: "stack"},
		{Recipe: "recipe.yaml", All: true},
		{Recipe: "recipe.yaml", Replace: true},
		{Recipe: "recipe.yaml", Track: true},
		{Recipe: "recipe.yaml", Force: true},
	}
	for _, options := range conflicts {
		if err := validateRecipeCreateOptions(options); err == nil {
			t.Fatalf("expected conflict for %+v", options)
		}
	}
}

func TestSelectLocalRecipeRepositoryMatchesCanonicalRemote(t *testing.T) {
	candidates := []recipeRepositoryCandidate{
		{Path: "/repos/example", Remote: "git@github.com:Acme/example.git"},
	}
	path, found, err := selectLocalRecipeRepository("https://github.com/Acme/example.git", candidates)
	if err != nil || !found || path != "/repos/example" {
		t.Fatalf("got path=%q found=%v err=%v", path, found, err)
	}
}

func TestFindRecipeRepositoryCandidatesIncludesNestedRepositories(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, "outer")
	nested := filepath.Join(outer, "nested")
	runRecipeGit(t, root, "init", outer)
	runRecipeGit(t, outer, "init", nested)
	runRecipeGit(t, nested, "remote", "add", "origin", "https://github.com/acme/nested.git")

	candidates := findRecipeRepositoryCandidates([]string{root})
	path, found, err := selectLocalRecipeRepository("git@github.com:acme/nested.git", candidates)
	expectedPath, _ := filepath.EvalSymlinks(nested)
	if err != nil || !found || path != expectedPath {
		t.Fatalf("path=%q found=%v err=%v candidates=%+v", path, found, err, candidates)
	}
}

func TestSelectLocalRecipeRepositoryRejectsAmbiguousMatch(t *testing.T) {
	candidates := []recipeRepositoryCandidate{
		{Path: "/repos/one", Remote: "https://github.com/acme/example.git"},
		{Path: "/repos/two", Remote: "git@github.com:acme/example.git"},
	}
	if _, _, err := selectLocalRecipeRepository("https://github.com/acme/example", candidates); err == nil {
		t.Fatal("expected ambiguous remote error")
	}
}

func TestResolveRecipeRepositoriesUsesFetchedRemoteCommit(t *testing.T) {
	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repos")
	os.MkdirAll(reposDir, 0o755)
	bare := filepath.Join(dir, "origin.git")
	repoPath := filepath.Join(reposDir, "example")
	runRecipeGit(t, dir, "init", "--bare", bare)
	runRecipeGit(t, dir, "clone", bare, repoPath)
	runRecipeGit(t, repoPath, "config", "user.email", "test@example.com")
	runRecipeGit(t, repoPath, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("initial"), 0o644)
	runRecipeGit(t, repoPath, "add", ".")
	runRecipeGit(t, repoPath, "commit", "-m", "initial")
	runRecipeGit(t, repoPath, "push", "origin", "HEAD")
	branch := runRecipeGit(t, repoPath, "branch", "--show-current")
	remoteSHA := runRecipeGit(t, repoPath, "rev-parse", "origin/"+branch)
	runRecipeGit(t, repoPath, "remote", "set-url", "origin", "file://"+bare)

	os.WriteFile(filepath.Join(repoPath, "local.txt"), []byte("local"), 0o644)
	runRecipeGit(t, repoPath, "add", ".")
	runRecipeGit(t, repoPath, "commit", "-m", "local only")

	cfg := &models.Config{RepoDirs: []string{reposDir}, WorkspaceDir: filepath.Join(dir, "workspaces")}
	model := &recipe.Recipe{Repositories: map[string]recipe.Repository{
		"api": {URL: "file://" + bare, Ref: branch},
	}}
	resolved, err := resolveRecipeRepositories(model, cfg)
	if err != nil {
		t.Fatal(err)
	}
	expectedPath, _ := filepath.EvalSymlinks(repoPath)
	if resolved.RepoMap["api"] != expectedPath || resolved.BaseCommits["api"] != remoteSHA {
		t.Fatalf("unexpected resolution: %+v", resolved)
	}
}

func TestResolveRecipeRepositoriesRejectsExistingRepoWithoutExpectedOrigin(t *testing.T) {
	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repos")
	os.MkdirAll(reposDir, 0o755)
	bare := filepath.Join(dir, "example.git")
	runRecipeGit(t, dir, "init", "--bare", bare)
	conflict := filepath.Join(reposDir, "example")
	runRecipeGit(t, reposDir, "init", conflict)
	runRecipeGit(t, conflict, "config", "user.email", "test@example.com")
	runRecipeGit(t, conflict, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(conflict, "README.md"), []byte("unrelated"), 0o644)
	runRecipeGit(t, conflict, "add", ".")
	runRecipeGit(t, conflict, "commit", "-m", "unrelated")

	cfg := &models.Config{RepoDirs: []string{reposDir}, WorkspaceDir: filepath.Join(dir, "workspaces")}
	model := &recipe.Recipe{Repositories: map[string]recipe.Repository{
		"api": {URL: "file://" + bare, Ref: "main"},
	}}
	if _, err := resolveRecipeRepositories(model, cfg); err == nil {
		t.Fatal("expected existing repository without matching origin to be rejected")
	}
}

func TestResolveRecipeRepositoriesClonesMissingRemote(t *testing.T) {
	dir := t.TempDir()
	reposDir := filepath.Join(dir, "repos")
	os.MkdirAll(reposDir, 0o755)
	bare := filepath.Join(dir, "example.git")
	seed := filepath.Join(dir, "seed")
	runRecipeGit(t, dir, "init", "--bare", bare)
	runRecipeGit(t, dir, "clone", bare, seed)
	runRecipeGit(t, seed, "config", "user.email", "test@example.com")
	runRecipeGit(t, seed, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(seed, "README.md"), []byte("initial"), 0o644)
	runRecipeGit(t, seed, "add", ".")
	runRecipeGit(t, seed, "commit", "-m", "initial")
	runRecipeGit(t, seed, "push", "origin", "HEAD")
	branch := runRecipeGit(t, seed, "branch", "--show-current")

	cfg := &models.Config{RepoDirs: []string{reposDir}, WorkspaceDir: filepath.Join(dir, "workspaces")}
	model := &recipe.Recipe{Repositories: map[string]recipe.Repository{
		"api": {URL: "file://" + bare, Ref: branch},
	}}
	resolved, err := resolveRecipeRepositories(model, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.RepoMap["api"] == "" || resolved.BaseCommits["api"] == "" {
		t.Fatalf("missing cloned resolution: %+v", resolved)
	}
	if _, err := os.Stat(filepath.Join(resolved.RepoMap["api"], ".git")); err != nil {
		t.Fatalf("clone missing: %v", err)
	}
}

type recipeCreateCommandEnv struct {
	groveDir     string
	workspaceDir string
	repoPath     string
}

func setupRecipeCreateCommand(t *testing.T, command string) recipeCreateCommandEnv {
	t.Helper()
	dir := t.TempDir()
	groveDir := filepath.Join(dir, ".grove")
	reposDir := filepath.Join(dir, "repos")
	workspaceDir := filepath.Join(groveDir, "workspaces")
	os.MkdirAll(reposDir, 0o755)
	os.MkdirAll(workspaceDir, 0o755)
	os.WriteFile(filepath.Join(groveDir, "state.json"), []byte("[]"), 0o644)

	bare := filepath.Join(dir, "origin.git")
	repoPath := filepath.Join(reposDir, "source-name")
	runRecipeGit(t, dir, "init", "--bare", bare)
	runRecipeGit(t, dir, "clone", bare, repoPath)
	runRecipeGit(t, repoPath, "config", "user.email", "test@example.com")
	runRecipeGit(t, repoPath, "config", "user.name", "Test")
	os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("initial"), 0o644)
	runRecipeGit(t, repoPath, "add", ".")
	runRecipeGit(t, repoPath, "commit", "-m", "initial")
	runRecipeGit(t, repoPath, "push", "origin", "HEAD")
	branch := runRecipeGit(t, repoPath, "branch", "--show-current")
	runRecipeGit(t, repoPath, "remote", "set-url", "origin", "file://"+bare)

	recipePath := filepath.Join(dir, "recipe.yaml")
	recipeData := "version: 1\nname: test-recipe\nrepositories:\n  api:\n    url: file://" + bare + "\n    ref: " + branch + "\njobs:\n  setup:\n    repository: api\n    steps:\n      - name: Prepare\n        run: " + strconv.Quote(command) + "\n"
	os.WriteFile(recipePath, []byte(recipeData), 0o644)

	oldGroveDir, oldConfigPath, oldWorkspaceDir := config.GroveDir, config.ConfigPath, config.DefaultWorkspaceDir
	config.GroveDir = groveDir
	config.ConfigPath = filepath.Join(groveDir, "config.toml")
	config.DefaultWorkspaceDir = workspaceDir
	if err := config.Save(&models.Config{RepoDirs: []string{reposDir}, WorkspaceDir: workspaceDir}); err != nil {
		t.Fatal(err)
	}
	oldRecipe, oldBranch, oldJSON := createRecipe, createBranch, createJSON
	oldRepos, oldPreset, oldAll := createRepos, createPreset, createAll
	oldReplace, oldTrack, oldForce := createReplace, createTrack, createForce
	createRecipe, createBranch, createJSON = recipePath, "feat/recipe", true
	createRepos, createPreset, createAll = "", "", false
	createReplace, createTrack, createForce = false, false, false
	t.Cleanup(func() {
		config.GroveDir, config.ConfigPath, config.DefaultWorkspaceDir = oldGroveDir, oldConfigPath, oldWorkspaceDir
		createRecipe, createBranch, createJSON = oldRecipe, oldBranch, oldJSON
		createRepos, createPreset, createAll = oldRepos, oldPreset, oldAll
		createReplace, createTrack, createForce = oldReplace, oldTrack, oldForce
	})
	return recipeCreateCommandEnv{groveDir: groveDir, workspaceDir: workspaceDir, repoPath: repoPath}
}

func runRecipeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
