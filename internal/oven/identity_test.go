package oven

import (
	"testing"

	"github.com/nicksenap/grove/internal/recipe"
)

func TestGenerationIdentityIsCanonical(t *testing.T) {
	first := &recipe.Recipe{
		Version: 1,
		Name:    "stack",
		Repositories: map[string]recipe.Repository{
			"web": {URL: "https://example.com/web.git", Ref: "main"},
			"api": {URL: "https://example.com/api.git", Ref: "v1"},
		},
		Jobs: map[string]recipe.Job{
			"verify": {
				Repository: "api",
				Needs:      []string{"web", "api"},
				Steps:      []recipe.Step{{Name: "Check", Run: "make check"}},
			},
		},
	}
	second := &recipe.Recipe{
		Version: 1,
		Name:    "stack",
		Repositories: map[string]recipe.Repository{
			"api": {URL: "https://example.com/api.git", Ref: "v1"},
			"web": {URL: "https://example.com/web.git", Ref: "main"},
		},
		Jobs: map[string]recipe.Job{
			"verify": {
				Repository: "api",
				Needs:      []string{"api", "web"},
				Steps:      []recipe.Step{{Name: "Check", Run: "make check"}},
			},
		},
	}
	commitsA := map[string]string{"web": "2222", "api": "1111"}
	commitsB := map[string]string{"api": "1111", "web": "2222"}

	firstRecipe, firstGeneration, err := Identity(first, commitsA, "runner-a")
	if err != nil {
		t.Fatal(err)
	}
	secondRecipe, secondGeneration, err := Identity(second, commitsB, "runner-a")
	if err != nil {
		t.Fatal(err)
	}
	if firstRecipe != secondRecipe || firstGeneration != secondGeneration {
		t.Fatalf("canonical identities differ: recipe %s/%s generation %s/%s", firstRecipe, secondRecipe, firstGeneration, secondGeneration)
	}
}

func TestGenerationIdentityChangesWithInputs(t *testing.T) {
	model := &recipe.Recipe{
		Version: 1,
		Repositories: map[string]recipe.Repository{
			"api": {URL: "https://example.com/api.git", Ref: "main"},
		},
		Jobs: map[string]recipe.Job{
			"setup": {Repository: "api", Steps: []recipe.Step{{Run: "make setup"}, {Run: "make check"}}},
		},
	}
	baseRecipe, baseGeneration, err := Identity(model, map[string]string{"api": "aaaa"}, "runner-a")
	if err != nil {
		t.Fatal(err)
	}
	_, changedCommit, _ := Identity(model, map[string]string{"api": "bbbb"}, "runner-a")
	_, changedRunner, _ := Identity(model, map[string]string{"api": "aaaa"}, "runner-b")
	changedModel := *model
	changedModel.Jobs = map[string]recipe.Job{
		"setup": {Repository: "api", Steps: []recipe.Step{{Run: "make check"}, {Run: "make setup"}}},
	}
	changedRecipe, changedSteps, _ := Identity(&changedModel, map[string]string{"api": "aaaa"}, "runner-a")
	if baseGeneration == changedCommit || baseGeneration == changedRunner || baseGeneration == changedSteps {
		t.Fatal("generation identity did not change with commit, runner, or ordered steps")
	}
	if baseRecipe == changedRecipe {
		t.Fatal("Recipe identity did not change with Recipe steps")
	}
}

func TestRecipeIdentityTreatsMissingJobsAsEmpty(t *testing.T) {
	withoutJobs := &recipe.Recipe{Version: 1, Repositories: map[string]recipe.Repository{
		"api": {URL: "https://example.com/api.git", Ref: "main"},
	}}
	withEmptyJobs := *withoutJobs
	withEmptyJobs.Jobs = map[string]recipe.Job{}
	first, _, err := Identity(withoutJobs, map[string]string{"api": "aaaa"}, "runner")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := Identity(&withEmptyJobs, map[string]string{"api": "aaaa"}, "runner")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("missing and empty jobs differ: %s != %s", first, second)
	}
}

func TestGenerationIdentityRequiresEveryCommit(t *testing.T) {
	model := &recipe.Recipe{Version: 1, Repositories: map[string]recipe.Repository{
		"api": {URL: "https://example.com/api.git", Ref: "main"},
	}}
	if _, _, err := Identity(model, map[string]string{}, "runner"); err == nil {
		t.Fatal("expected missing commit error")
	}
}
