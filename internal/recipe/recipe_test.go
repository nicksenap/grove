package recipe

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

const validRecipe = `version: 1
name: example-stack
repositories:
  app:
    url: https://github.com/acme/example-app.git
    ref: main
jobs:
  setup-api:
    repository: app
    working-directory: services/api
    steps:
      - name: Install
        run: make setup
  verify:
    repository: app
    needs: [setup-api]
    steps:
      - run: make check
`

func TestParseValidRecipe(t *testing.T) {
	result := Parse([]byte(validRecipe))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	if result.Recipe == nil {
		t.Fatal("expected recipe")
	}
	if result.Recipe.Version != 1 || result.Recipe.Name != "example-stack" {
		t.Fatalf("unexpected recipe: %+v", result.Recipe)
	}
	if len(result.Recipe.Repositories) != 1 || len(result.Recipe.Jobs) != 2 {
		t.Fatalf("unexpected counts: repos=%d jobs=%d", len(result.Recipe.Repositories), len(result.Recipe.Jobs))
	}
	if got := result.Recipe.Jobs["verify"].Needs; len(got) != 1 || got[0] != "setup-api" {
		t.Fatalf("unexpected needs: %v", got)
	}
}

func TestParseJobTimeout(t *testing.T) {
	input := strings.Replace(validRecipe, "needs: [setup-api]", "timeout-minutes: 30\n    needs: [setup-api]", 1)
	result := Parse([]byte(input))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	if got := result.Recipe.Jobs["verify"].TimeoutMinutes; got != 30 {
		t.Fatalf("timeout-minutes = %d, want 30", got)
	}
}

func TestGenericExample(t *testing.T) {
	data, err := os.ReadFile("../../examples/recipes/example-stack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	result := Parse(data)
	if len(result.Errors) != 0 {
		t.Fatalf("example is invalid: %+v", result.Errors)
	}
}

func TestParseRepositoryOnlyRecipe(t *testing.T) {
	result := Parse([]byte(`version: 1
repositories:
  app:
    url: https://github.com/acme/example-app.git
    ref: main
`))
	if len(result.Errors) != 0 {
		t.Fatalf("unexpected errors: %+v", result.Errors)
	}
	if result.Recipe == nil || result.Recipe.Name != "" || len(result.Recipe.Jobs) != 0 {
		t.Fatalf("unexpected recipe: %+v", result.Recipe)
	}
}

func TestParseRejectsMalformedAndNonStrictYAML(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		code string
	}{
		{"malformed", "version: [", "invalid_yaml"},
		{"unknown field", validRecipe + "extra: true\n", "unknown_field"},
		{"duplicate key", strings.Replace(validRecipe, "name: example-stack", "name: example-stack\nname: duplicate", 1), "duplicate_key"},
		{"anchor", strings.Replace(validRecipe, "name: example-stack", "name: &recipe_name example-stack", 1), "unsupported_yaml"},
		{"alias", strings.Replace(validRecipe, "name: example-stack", "name: &recipe_name example-stack\nother: *recipe_name", 1), "unsupported_yaml"},
		{"custom tag", strings.Replace(validRecipe, "name: example-stack", "name: !custom example-stack", 1), "unsupported_yaml"},
		{"expanded custom tag", strings.Replace(validRecipe, "name: example-stack", "name: !<tag:example.com,2024:display> example-stack", 1), "unsupported_yaml"},
		{"multiple documents", validRecipe + "---\nversion: 1\n", "multiple_documents"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse([]byte(tt.yaml))
			assertErrorCode(t, result, tt.code)
		})
	}
}

func TestParseRejectsIncorrectSchemaTypes(t *testing.T) {
	tests := []string{
		strings.Replace(validRecipe, "version: 1", "version: 1.0", 1),
		strings.Replace(validRecipe, "name: example-stack", "name: true", 1),
		strings.Replace(validRecipe, "url: https://github.com/acme/example-app.git", "url: true", 1),
		strings.Replace(validRecipe, "ref: main", "ref: null", 1),
		strings.Replace(validRecipe, "needs: [setup-api]", "needs: setup-api", 1),
		strings.Replace(validRecipe, "needs: [setup-api]", "timeout-minutes: '30'\n    needs: [setup-api]", 1),
		strings.Replace(validRecipe, "run: make check", "run: 123", 1),
		strings.SplitN(validRecipe, "jobs:", 2)[0] + "jobs: null\n",
	}
	for index, input := range tests {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			assertErrorCode(t, Parse([]byte(input)), "invalid_type")
		})
	}
}

func TestParseValidatesGitURLs(t *testing.T) {
	validSSH := strings.Replace(validRecipe, "https://github.com/acme/example-app.git", "ssh://git@example.com/acme/example-app.git", 1)
	if result := Parse([]byte(validSSH)); len(result.Errors) != 0 {
		t.Fatalf("valid SSH URL rejected: %+v", result.Errors)
	}
	for _, invalidURL := range []string{"https://", "http:// bad", "ext::sh -c id"} {
		t.Run(invalidURL, func(t *testing.T) {
			input := strings.Replace(validRecipe, "https://github.com/acme/example-app.git", invalidURL, 1)
			assertErrorCode(t, Parse([]byte(input)), "invalid_git_url")
		})
	}
}

func TestParseRejectsExcessiveYAMLDepth(t *testing.T) {
	input := validRecipe + "extra: " + strings.Repeat("[", maxYAMLDepth+1) + "value" + strings.Repeat("]", maxYAMLDepth+1) + "\n"
	assertErrorCode(t, Parse([]byte(input)), "yaml_too_complex")
}

func TestParseRejectsInvalidRecipeSemantics(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		code string
	}{
		{"missing version", strings.Replace(validRecipe, "version: 1\n", "", 1), "required"},
		{"unsupported version", strings.Replace(validRecipe, "version: 1", "version: 2", 1), "unsupported_version"},
		{"empty name", strings.Replace(validRecipe, "name: example-stack", "name: '  '", 1), "required"},
		{"invalid repository id", strings.Replace(validRecipe, "  app:", "  APP:", 1), "invalid_id"},
		{"invalid job id", strings.Replace(validRecipe, "  setup-api:", "  Setup.API:", 1), "invalid_id"},
		{"invalid git url", strings.Replace(validRecipe, "https://github.com/acme/example-app.git", "not-a-url", 1), "invalid_git_url"},
		{"unknown repository", strings.Replace(validRecipe, "repository: app", "repository: missing", 1), "unknown_repository"},
		{"unknown dependency", strings.Replace(validRecipe, "needs: [setup-api]", "needs: [missing]", 1), "unknown_job"},
		{"duplicate dependency", strings.Replace(validRecipe, "needs: [setup-api]", "needs: [setup-api, setup-api]", 1), "duplicate_dependency"},
		{"escaping working directory", strings.Replace(validRecipe, "services/api", "../../outside", 1), "invalid_path"},
		{"unclean working directory", strings.Replace(validRecipe, "services/api", "services/../api", 1), "invalid_path"},
		{"zero timeout", strings.Replace(validRecipe, "needs: [setup-api]", "timeout-minutes: 0\n    needs: [setup-api]", 1), "invalid_range"},
		{"excessive timeout", strings.Replace(validRecipe, "needs: [setup-api]", "timeout-minutes: 361\n    needs: [setup-api]", 1), "invalid_range"},
		{"empty steps", strings.Replace(validRecipe, "steps:\n      - name: Install\n        run: make setup", "steps: []", 1), "required"},
		{"empty command", strings.Replace(validRecipe, "run: make check", "run: '  '", 1), "required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse([]byte(tt.yaml))
			assertErrorCode(t, result, tt.code)
		})
	}
}

func TestParseLocatesMalformedYAML(t *testing.T) {
	result := Parse([]byte("version: 1\nrepositories: ["))
	err := findError(result, "invalid_yaml")
	if err == nil {
		t.Fatalf("expected invalid_yaml, got %+v", result.Errors)
	}
	if err.Line != 2 || err.Column == 0 {
		t.Fatalf("expected line 2 location, got %+v", err)
	}
}

func TestParseRejectsDependencyCycle(t *testing.T) {
	yaml := strings.Replace(validRecipe, "repository: app\n    working-directory", "repository: app\n    needs: [verify]\n    working-directory", 1)
	result := Parse([]byte(yaml))
	err := findError(result, "dependency_cycle")
	if err == nil {
		t.Fatalf("expected dependency_cycle, got %+v", result.Errors)
	}
	if err.Line == 0 || err.Column == 0 || err.Path == "" {
		t.Fatalf("expected located error, got %+v", err)
	}
}

func TestParseRejectsOversizedRecipe(t *testing.T) {
	result := Parse([]byte(strings.Repeat("x", MaxRecipeBytes+1)))
	assertErrorCode(t, result, "file_too_large")
}

func assertErrorCode(t *testing.T, result Result, code string) {
	t.Helper()
	if err := findError(result, code); err == nil {
		t.Fatalf("expected %q, got %+v", code, result.Errors)
	}
}

func findError(result Result, code string) *ValidationError {
	for i := range result.Errors {
		if result.Errors[i].Code == code {
			return &result.Errors[i]
		}
	}
	return nil
}
