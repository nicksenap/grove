package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/nicksenap/grove/internal/recipe"
	"github.com/spf13/cobra"
)

const offlineCommandAnnotation = "grove.offline"

var errRecipeInvalid = errors.New("recipe is invalid")

var recipeValidateJSON bool

var recipeCmd = &cobra.Command{
	Use:   "recipe",
	Short: "Inspect and validate workspace Recipes",
}

var recipeValidateCmd = &cobra.Command{
	Use:   "validate FILE",
	Short: "Validate a Recipe without executing it",
	Args:  cobra.ExactArgs(1),
	Annotations: map[string]string{
		offlineCommandAnnotation: "true",
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRecipeValidate(args[0], recipeValidateJSON, cmd.OutOrStdout())
	},
}

type recipeValidationOutput struct {
	Valid        bool                     `json:"valid"`
	Name         string                   `json:"name,omitempty"`
	Version      *int                     `json:"version,omitempty"`
	Repositories *int                     `json:"repositories,omitempty"`
	Jobs         *int                     `json:"jobs,omitempty"`
	Errors       []recipe.ValidationError `json:"errors"`
}

func runRecipeValidate(path string, jsonOutput bool, stdout io.Writer) error {
	data, readErr := readRecipeFile(path)
	if readErr != nil {
		result := recipe.Result{Errors: []recipe.ValidationError{{
			Code:    "read_file",
			Path:    path,
			Message: readErr.Error(),
		}}}
		return writeRecipeValidation(result, jsonOutput, stdout)
	}
	return writeRecipeValidation(recipe.Parse(data), jsonOutput, stdout)
}

func readRecipeFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("recipe path is not a regular file")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if openedInfo, err := file.Stat(); err != nil {
		return nil, err
	} else if !openedInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("recipe path is not a regular file")
	}

	data, err := io.ReadAll(io.LimitReader(file, recipe.MaxRecipeBytes+1))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func writeRecipeValidation(result recipe.Result, jsonOutput bool, stdout io.Writer) error {
	output := recipeValidationOutput{Valid: result.Recipe != nil && len(result.Errors) == 0, Errors: result.Errors}
	if output.Valid {
		output.Name = result.Recipe.Name
		output.Version = intPointer(result.Recipe.Version)
		output.Repositories = intPointer(len(result.Recipe.Repositories))
		output.Jobs = intPointer(len(result.Recipe.Jobs))
	}
	if output.Errors == nil {
		output.Errors = []recipe.ValidationError{}
	}

	if jsonOutput {
		if err := json.NewEncoder(stdout).Encode(output); err != nil {
			return err
		}
	} else if output.Valid {
		name := ""
		if output.Name != "" {
			name = ": " + terminalSafe(output.Name)
		}
		repositories := *output.Repositories
		jobs := *output.Jobs
		fmt.Fprintf(stdout, "Recipe valid%s (%d %s, %d %s)\n",
			name,
			repositories, pluralize(repositories, "repository", "repositories"),
			jobs, pluralize(jobs, "job", "jobs"))
	}

	if output.Valid {
		return nil
	}
	if jsonOutput {
		return errRecipeInvalid
	}
	return fmt.Errorf("%w:\n%s", errRecipeInvalid, formatRecipeErrors(output.Errors))
}

func formatRecipeErrors(validationErrors []recipe.ValidationError) string {
	lines := make([]string, 0, len(validationErrors))
	for _, validationErr := range validationErrors {
		location := validationErr.Path
		if location == "" {
			location = "recipe"
		}
		if validationErr.Line > 0 {
			location = fmt.Sprintf("%s:%d:%d", location, validationErr.Line, validationErr.Column)
		}
		lines = append(lines, fmt.Sprintf("  %s [%s] %s", terminalSafe(location), validationErr.Code, terminalSafe(validationErr.Message)))
	}
	return strings.Join(lines, "\n")
}

func terminalSafe(value string) string {
	var result strings.Builder
	for _, char := range value {
		if char < 0x20 || char == 0x7f || char >= 0x80 && char <= 0x9f {
			fmt.Fprintf(&result, "\\x%02x", char)
			continue
		}
		result.WriteRune(char)
	}
	return result.String()
}

func intPointer(value int) *int {
	return &value
}

func pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func init() {
	recipeValidateCmd.Flags().BoolVarP(&recipeValidateJSON, "json", "j", false, "Output validation result as JSON")
	recipeCmd.AddCommand(recipeValidateCmd)
}
