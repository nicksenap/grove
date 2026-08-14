package oven

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"

	"github.com/nicksenap/grove/internal/recipe"
)

const CompatibilityVersion = "oven-v1"

type normalizedRecipe struct {
	Version      int                    `json:"version"`
	Name         string                 `json:"name,omitempty"`
	Repositories []normalizedRepository `json:"repositories"`
	Jobs         []normalizedJob        `json:"jobs"`
}

type normalizedRepository struct {
	ID  string `json:"id"`
	URL string `json:"url"`
	Ref string `json:"ref"`
}

type normalizedJob struct {
	ID               string           `json:"id"`
	Repository       string           `json:"repository"`
	Needs            []string         `json:"needs"`
	WorkingDirectory string           `json:"working_directory,omitempty"`
	TimeoutMinutes   int              `json:"timeout_minutes,omitempty"`
	Steps            []normalizedStep `json:"steps"`
}

type normalizedStep struct {
	Name string `json:"name,omitempty"`
	Run  string `json:"run"`
}

type normalizedGeneration struct {
	Recipe  normalizedRecipe   `json:"recipe"`
	Commits []normalizedCommit `json:"commits"`
	Runner  string             `json:"runner"`
}

type normalizedCommit struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
}

func Identity(model *recipe.Recipe, commits map[string]string, runner string) (string, string, error) {
	if model == nil {
		return "", "", fmt.Errorf("recipe is required")
	}
	if runner == "" {
		return "", "", fmt.Errorf("runner identity is required")
	}
	normalized := normalizeRecipe(model)
	commitList := make([]normalizedCommit, 0, len(normalized.Repositories))
	for _, repository := range normalized.Repositories {
		commit := commits[repository.ID]
		if commit == "" {
			return "", "", fmt.Errorf("repository %s has no resolved commit", repository.ID)
		}
		commitList = append(commitList, normalizedCommit{Repository: repository.ID, Commit: commit})
	}
	recipeKey, err := hashJSON(struct {
		Recipe normalizedRecipe `json:"recipe"`
		Runner string           `json:"runner"`
	}{Recipe: normalized, Runner: runner})
	if err != nil {
		return "", "", err
	}
	generation, err := hashJSON(normalizedGeneration{Recipe: normalized, Commits: commitList, Runner: runner})
	if err != nil {
		return "", "", err
	}
	return recipeKey, generation, nil
}

func RecipeIdentity(model *recipe.Recipe, runner string) (string, error) {
	if model == nil {
		return "", fmt.Errorf("recipe is required")
	}
	if runner == "" {
		return "", fmt.Errorf("runner identity is required")
	}
	return hashJSON(struct {
		Recipe normalizedRecipe `json:"recipe"`
		Runner string           `json:"runner"`
	}{Recipe: normalizeRecipe(model), Runner: runner})
}

func LocalRunnerIdentity() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return CompatibilityVersion + "/" + runtime.GOOS + "/" + runtime.GOARCH + "/" + hostname
}

func normalizeRecipe(model *recipe.Recipe) normalizedRecipe {
	normalized := normalizedRecipe{Version: model.Version, Name: model.Name}
	repositoryIDs := make([]string, 0, len(model.Repositories))
	for id := range model.Repositories {
		repositoryIDs = append(repositoryIDs, id)
	}
	sort.Strings(repositoryIDs)
	for _, id := range repositoryIDs {
		repository := model.Repositories[id]
		normalized.Repositories = append(normalized.Repositories, normalizedRepository{ID: id, URL: repository.URL, Ref: repository.Ref})
	}
	jobIDs := make([]string, 0, len(model.Jobs))
	for id := range model.Jobs {
		jobIDs = append(jobIDs, id)
	}
	sort.Strings(jobIDs)
	for _, id := range jobIDs {
		job := model.Jobs[id]
		needs := append([]string(nil), job.Needs...)
		sort.Strings(needs)
		normalizedJob := normalizedJob{
			ID: id, Repository: job.Repository, Needs: needs,
			WorkingDirectory: job.WorkingDirectory, TimeoutMinutes: job.TimeoutMinutes,
		}
		for _, step := range job.Steps {
			normalizedJob.Steps = append(normalizedJob.Steps, normalizedStep{Name: step.Name, Run: step.Run})
		}
		normalized.Jobs = append(normalized.Jobs, normalizedJob)
	}
	return normalized
}

func hashJSON(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
