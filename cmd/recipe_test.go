package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRunRecipeValidateHuman(t *testing.T) {
	path := writeRecipeFixture(t, validRecipeFixture)
	var stdout bytes.Buffer

	if err := runRecipeValidate(path, false, &stdout); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := stdout.String(); got != "Recipe valid: example-stack (1 repository, 1 job)\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunRecipeValidateRepositoryOnly(t *testing.T) {
	path := writeRecipeFixture(t, `version: 1
repositories:
  app:
    url: https://github.com/acme/example-app.git
    ref: main
`)
	var stdout bytes.Buffer

	if err := runRecipeValidate(path, false, &stdout); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := stdout.String(); got != "Recipe valid (1 repository, 0 jobs)\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunRecipeValidateJSON(t *testing.T) {
	path := writeRecipeFixture(t, validRecipeFixture)
	var stdout bytes.Buffer

	if err := runRecipeValidate(path, true, &stdout); err != nil {
		t.Fatalf("validate: %v", err)
	}
	var output recipeValidationOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if !output.Valid || output.Name != "example-stack" || output.Repositories == nil || *output.Repositories != 1 || output.Jobs == nil || *output.Jobs != 1 || len(output.Errors) != 0 {
		t.Fatalf("unexpected output: %+v", output)
	}
}

func TestRunRecipeValidateRepositoryOnlyJSONIncludesZeroJobs(t *testing.T) {
	path := writeRecipeFixture(t, `version: 1
repositories:
  app:
    url: https://github.com/acme/example-app.git
    ref: main
`)
	var stdout bytes.Buffer
	if err := runRecipeValidate(path, true, &stdout); err != nil {
		t.Fatalf("validate: %v", err)
	}
	var output map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if jobs, ok := output["jobs"]; !ok || jobs != float64(0) {
		t.Fatalf("expected jobs: 0, got %v", output)
	}
}

func TestHumanOutputEscapesTerminalControls(t *testing.T) {
	input := strings.Replace(validRecipeFixture, "needs: []", `needs: ["missing\u001b[31m"]`, 1)
	var stdout bytes.Buffer
	err := runRecipeValidate(writeRecipeFixture(t, input), false, &stdout)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.ContainsRune(err.Error(), '\x1b') || !strings.Contains(err.Error(), `\x1b`) {
		t.Fatalf("terminal control was not escaped: %q", err.Error())
	}
}

func TestRunRecipeValidateInvalidHuman(t *testing.T) {
	path := writeRecipeFixture(t, strings.Replace(validRecipeFixture, "needs: []", "needs: [missing]", 1))
	var stdout bytes.Buffer

	err := runRecipeValidate(path, false, &stdout)
	if !errors.Is(err, errRecipeInvalid) {
		t.Fatalf("error = %v, want errRecipeInvalid", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
	message := err.Error()
	if !strings.Contains(message, "jobs.setup.needs[0]") || !strings.Contains(message, "[unknown_job]") {
		t.Fatalf("error is not actionable: %s", message)
	}
}

func TestRunRecipeValidateInvalidJSON(t *testing.T) {
	path := writeRecipeFixture(t, strings.Replace(validRecipeFixture, "needs: []", "needs: [missing]", 1))
	var stdout bytes.Buffer

	err := runRecipeValidate(path, true, &stdout)
	if !errors.Is(err, errRecipeInvalid) {
		t.Fatalf("error = %v, want errRecipeInvalid", err)
	}
	var output recipeValidationOutput
	if decodeErr := json.Unmarshal(stdout.Bytes(), &output); decodeErr != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", decodeErr, stdout.String())
	}
	if output.Valid || len(output.Errors) == 0 || output.Errors[0].Code != "unknown_job" {
		t.Fatalf("unexpected output: %+v", output)
	}
}

func TestRunRecipeValidateRejectsNonRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipe.yaml")
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	err := runRecipeValidate(path, true, &stdout)
	if !errors.Is(err, errRecipeInvalid) {
		t.Fatalf("error = %v, want errRecipeInvalid", err)
	}
	var output recipeValidationOutput
	if decodeErr := json.Unmarshal(stdout.Bytes(), &output); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(output.Errors) != 1 || output.Errors[0].Code != "read_file" {
		t.Fatalf("unexpected output: %+v", output)
	}
}

func TestRunRecipeValidateUnreadableJSON(t *testing.T) {
	var stdout bytes.Buffer
	err := runRecipeValidate(filepath.Join(t.TempDir(), "missing.yaml"), true, &stdout)
	if !errors.Is(err, errRecipeInvalid) {
		t.Fatalf("error = %v, want errRecipeInvalid", err)
	}
	var output recipeValidationOutput
	if decodeErr := json.Unmarshal(stdout.Bytes(), &output); decodeErr != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", decodeErr, stdout.String())
	}
	if output.Valid || len(output.Errors) != 1 || output.Errors[0].Code != "read_file" {
		t.Fatalf("unexpected output: %+v", output)
	}
}

func writeRecipeFixture(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recipe.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const validRecipeFixture = `version: 1
name: example-stack
repositories:
  app:
    url: https://github.com/acme/example-app.git
    ref: main
jobs:
  setup:
    repository: app
    needs: []
    steps:
      - run: make setup
`
