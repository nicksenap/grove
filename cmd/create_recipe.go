package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/lifecycle"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/recipe"
	"github.com/nicksenap/grove/internal/workspace"
	"github.com/spf13/cobra"
)

const (
	createErrorRecipeInvalid    = "recipe_invalid"
	createErrorResolutionFailed = "repository_resolution_failed"
	createErrorProvisionFailed  = "workspace_provision_failed"
	createErrorPostCreateFailed = "post_create_failed"
)

type recipeCreateOutput struct {
	Created bool                     `json:"created"`
	Name    string                   `json:"name"`
	Path    string                   `json:"path,omitempty"`
	Recipe  string                   `json:"recipe,omitempty"`
	Jobs    *[]recipe.JobResult      `json:"jobs,omitempty"`
	Error   *recipeCreateErrorOutput `json:"error,omitempty"`
}

type recipeCreateErrorOutput struct {
	Code         string `json:"code"`
	Job          string `json:"job,omitempty"`
	Step         int    `json:"step,omitempty"`
	StepName     string `json:"step_name,omitempty"`
	Message      string `json:"message"`
	CleanupError string `json:"cleanup_error,omitempty"`
}

type recipeCreateRunError struct {
	output recipeCreateErrorOutput
	cause  error
}

func (e *recipeCreateRunError) Error() string {
	if e.output.Job == "" {
		return e.output.Message
	}
	step := ""
	if e.output.Step > 0 {
		step = fmt.Sprintf(" step %d", e.output.Step)
		if e.output.StepName != "" {
			step += " (" + e.output.StepName + ")"
		}
	}
	return fmt.Sprintf("Recipe job %s%s: %s", e.output.Job, step, e.output.Message)
}
func (e *recipeCreateRunError) Unwrap() error { return e.cause }

func runRecipeCreate(cmd *cobra.Command, args []string) error {
	options := recipeCreateOptions{
		Recipe: createRecipe, Repos: createRepos, Preset: createPreset,
		All: createAll, Replace: createReplace, Track: createTrack, Force: createForce,
	}
	if err := validateRecipeCreateOptions(options); err != nil {
		return writeRecipeCreateFailure(cmd, "", "", false, recipeCreateErrorOutput{Code: createErrorRecipeInvalid, Message: err.Error()}, err)
	}

	name, branch, err := recipeCreateIdentity(args)
	if err != nil {
		return writeRecipeCreateFailure(cmd, name, "", false, recipeCreateErrorOutput{Code: createErrorRecipeInvalid, Message: err.Error()}, err)
	}
	data, err := readRecipeFile(createRecipe)
	if err != nil {
		return writeRecipeCreateFailure(cmd, name, "", false, recipeCreateErrorOutput{Code: createErrorRecipeInvalid, Message: err.Error()}, err)
	}
	parsed := recipe.Parse(data)
	if parsed.Recipe == nil || len(parsed.Errors) > 0 {
		err := errors.New(strings.TrimSpace(formatRecipeErrors(parsed.Errors)))
		return writeRecipeCreateFailure(cmd, name, "", false, recipeCreateErrorOutput{Code: createErrorRecipeInvalid, Message: err.Error()}, err)
	}

	cfg := config.RequireConfig()
	resolved, err := resolveRecipeRepositories(parsed.Recipe, cfg)
	if err != nil {
		return writeRecipeCreateFailure(cmd, name, parsed.Recipe.Name, false, recipeCreateErrorOutput{Code: createErrorResolutionFailed, Message: err.Error()}, err)
	}

	var source *models.WorkspaceSource
	if createSourceURL != "" || createSourceProvide != "" {
		source = &models.WorkspaceSource{Provider: createSourceProvide, URL: createSourceURL, Ref: createSourceRef, Title: createSourceTitle}
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var report recipe.Report
	svc := workspace.NewService()
	err = svc.CreateWithPreparation(name, workspace.PreparationOpts{
		CreateOpts: workspace.CreateOpts{
			Branch: branch, Repos: resolved.Names, RepoMap: resolved.RepoMap, Cfg: cfg, Source: source,
		},
		BaseCommits: resolved.BaseCommits,
	}, func(ws models.Workspace) error {
		worktrees := make(map[string]string, len(ws.Repos))
		for _, repoWorktree := range ws.Repos {
			worktrees[repoWorktree.RepoName] = repoWorktree.WorktreePath
		}
		var executeErr error
		report, executeErr = (recipe.Executor{Output: cmd.ErrOrStderr()}).Execute(ctx, parsed.Recipe, worktrees)
		return executeErr
	})
	wsPath := filepath.Join(cfg.WorkspaceDir, name)
	if err != nil {
		failure := recipeCreateFailureFromError(err)
		return writeRecipeCreateFailure(cmd, name, parsed.Recipe.Name, false, failure, err)
	}

	vars := lifecycle.Vars{Name: name, Path: wsPath, Branch: branch}
	if source != nil {
		vars.SourceURL, vars.SourceRef, vars.SourceTitle = source.URL, source.Ref, source.Title
	}
	if hookErr := lifecycle.Run("post_create", vars); hookErr != nil && !errors.Is(hookErr, lifecycle.ErrNoHook) {
		if lifecycle.ShouldAbort(hookErr) {
			failure := recipeCreateErrorOutput{Code: createErrorPostCreateFailed, Message: hookErr.Error()}
			if createJSON {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(recipeCreateOutput{
					Created: true, Name: name, Path: wsPath, Recipe: parsed.Recipe.Name, Jobs: &report.Jobs, Error: &failure,
				}); err != nil {
					return err
				}
			}
			return &recipeCreateRunError{output: failure, cause: hookErr}
		}
		console.Warning(hookErr.Error())
	}

	if createJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(recipeCreateOutput{
			Created: true, Name: name, Path: wsPath, Recipe: parsed.Recipe.Name, Jobs: &report.Jobs,
		})
	}
	return nil
}

func recipeCreateIdentity(args []string) (string, string, error) {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	branch := createBranch
	if branch == "" && console.IsTerminal(os.Stdin) {
		branch = console.PromptDefault("Branch name", name)
	}
	if branch == "" {
		return name, "", errors.New("branch is required: --branch / -b")
	}
	if name == "" {
		name = deriveName(branch)
	}
	return name, branch, nil
}

func recipeCreateFailureFromError(err error) recipeCreateErrorOutput {
	failure := recipeCreateErrorOutput{Code: createErrorProvisionFailed, Message: err.Error()}
	var preparationErr *workspace.PreparationError
	if !errors.As(err, &preparationErr) {
		return failure
	}
	failure.Message = preparationErr.Cause.Error()
	if preparationErr.CleanupErr != nil {
		failure.CleanupError = preparationErr.CleanupErr.Error()
	}
	var executionErr *recipe.ExecutionError
	if errors.As(preparationErr.Cause, &executionErr) {
		failure.Code = executionErr.Code
		failure.Job = executionErr.Job
		failure.Step = executionErr.Step
		failure.StepName = executionErr.StepName
		failure.Message = executionErr.Err.Error()
	}
	return failure
}

func writeRecipeCreateFailure(cmd *cobra.Command, name, recipeName string, created bool, failure recipeCreateErrorOutput, cause error) error {
	if createJSON {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(recipeCreateOutput{
			Created: created, Name: name, Recipe: recipeName, Error: &failure,
		}); err != nil {
			return err
		}
	}
	return &recipeCreateRunError{output: failure, cause: cause}
}

type recipeCreateOptions struct {
	Recipe  string
	Repos   string
	Preset  string
	All     bool
	Replace bool
	Track   bool
	Force   bool
}

func validateRecipeCreateOptions(options recipeCreateOptions) error {
	if options.Recipe == "" {
		return nil
	}
	var conflicts []string
	if options.Repos != "" {
		conflicts = append(conflicts, "--repos")
	}
	if options.Preset != "" {
		conflicts = append(conflicts, "--preset")
	}
	if options.All {
		conflicts = append(conflicts, "--all")
	}
	if options.Replace {
		conflicts = append(conflicts, "--replace")
	}
	if options.Track {
		conflicts = append(conflicts, "--track")
	}
	if options.Force {
		conflicts = append(conflicts, "--force")
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("--recipe cannot be combined with %s", strings.Join(conflicts, ", "))
	}
	return nil
}

type recipeRepositoryCandidate struct {
	Path   string
	Remote string
}

type resolvedRecipeRepositories struct {
	Names       []string
	RepoMap     map[string]string
	BaseCommits map[string]string
}

func resolveRecipeRepositories(model *recipe.Recipe, cfg *models.Config) (resolvedRecipeRepositories, error) {
	result := resolvedRecipeRepositories{
		Names:       sortedRecipeRepositoryIDs(model.Repositories),
		RepoMap:     make(map[string]string, len(model.Repositories)),
		BaseCommits: make(map[string]string, len(model.Repositories)),
	}
	candidates := findRecipeRepositoryCandidates(cfg.RepoDirs)
	seenIdentities := make(map[string]string, len(model.Repositories))

	for _, id := range result.Names {
		repository := model.Repositories[id]
		identity, err := gitops.CanonicalRemoteIdentity(repository.URL)
		if err != nil {
			return resolvedRecipeRepositories{}, fmt.Errorf("repository %s: %w", id, err)
		}
		if existingID, duplicate := seenIdentities[identity]; duplicate {
			return resolvedRecipeRepositories{}, fmt.Errorf("repositories %s and %s resolve to the same remote", existingID, id)
		}
		seenIdentities[identity] = id

		path, found, err := selectLocalRecipeRepository(repository.URL, candidates)
		if err != nil {
			return resolvedRecipeRepositories{}, fmt.Errorf("repository %s: %w", id, err)
		}
		if !found {
			if len(cfg.RepoDirs) == 0 {
				return resolvedRecipeRepositories{}, fmt.Errorf("repository %s is not local and no repo_dirs are configured", id)
			}
			console.Infof("Cloning %s ...", terminalSafe(repository.URL))
			clonedPath, _, err := gitops.Clone(repository.URL, cfg.RepoDirs[0])
			if err != nil {
				return resolvedRecipeRepositories{}, fmt.Errorf("repository %s: %w", id, err)
			}
			path = clonedPath
		}
		resolvedRemote := gitops.RemoteURL(path, "origin")
		resolvedIdentity, remoteErr := gitops.CanonicalRemoteIdentity(resolvedRemote)
		if remoteErr != nil || resolvedIdentity != identity {
			return resolvedRecipeRepositories{}, fmt.Errorf("repository %s: local clone at %s does not have the expected origin", id, path)
		}
		result.RepoMap[id] = path
	}

	var wg sync.WaitGroup
	errs := make([]error, len(result.Names))
	var mu sync.Mutex
	for index, id := range result.Names {
		wg.Add(1)
		go func(index int, id string) {
			defer wg.Done()
			path := result.RepoMap[id]
			if err := gitops.Fetch(path); err != nil {
				errs[index] = fmt.Errorf("repository %s: fetch failed: %w", id, err)
				return
			}
			sha, err := gitops.ResolveCommit(path, model.Repositories[id].Ref)
			if err != nil {
				errs[index] = fmt.Errorf("repository %s: %w", id, err)
				return
			}
			mu.Lock()
			result.BaseCommits[id] = sha
			mu.Unlock()
		}(index, id)
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return resolvedRecipeRepositories{}, err
	}
	return result, nil
}

func findRecipeRepositoryCandidates(repoDirs []string) []recipeRepositoryCandidate {
	var candidates []recipeRepositoryCandidate
	seenPaths := make(map[string]bool)
	for _, root := range repoDirs {
		discover.WalkRepos([]string{root}, 3, func(path, _ string, _ int, _ bool) bool {
			canonicalPath := path
			if resolved, err := filepath.EvalSymlinks(path); err == nil {
				canonicalPath = resolved
			}
			if absolute, err := filepath.Abs(canonicalPath); err == nil {
				canonicalPath = absolute
			}
			if !seenPaths[canonicalPath] {
				seenPaths[canonicalPath] = true
				candidates = append(candidates, recipeRepositoryCandidate{Path: canonicalPath, Remote: gitops.RemoteURL(canonicalPath, "origin")})
			}
			return true
		})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })
	return candidates
}

func selectLocalRecipeRepository(remote string, candidates []recipeRepositoryCandidate) (string, bool, error) {
	identity, err := gitops.CanonicalRemoteIdentity(remote)
	if err != nil {
		return "", false, err
	}
	var matches []string
	for _, candidate := range candidates {
		candidateIdentity, err := gitops.CanonicalRemoteIdentity(candidate.Remote)
		if err == nil && candidateIdentity == identity {
			matches = append(matches, candidate.Path)
		}
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", false, fmt.Errorf("multiple local repositories match %s: %s", remote, strings.Join(matches, ", "))
	}
	if len(matches) == 1 {
		return matches[0], true, nil
	}
	return "", false, nil
}

func sortedRecipeRepositoryIDs(repositories map[string]recipe.Repository) []string {
	ids := make([]string, 0, len(repositories))
	for id := range repositories {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
