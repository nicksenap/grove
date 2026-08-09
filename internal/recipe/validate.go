package recipe

import (
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

func validateRecipe(recipe *Recipe, locations map[string]location) []ValidationError {
	validator := validator{locations: locations}
	validateMetadata(recipe, locations, &validator)
	validateRepositories(recipe.Repositories, &validator)
	validateJobs(recipe, &validator)
	validateCycles(recipe.Jobs, &validator)
	return validator.errors
}

func validateMetadata(recipe *Recipe, locations map[string]location, validator *validator) {
	if _, present := locations["version"]; !present {
		validator.add("required", "version", "version is required")
	} else if recipe.Version != 1 {
		validator.add("unsupported_version", "version", fmt.Sprintf("unsupported Recipe version: %d", recipe.Version))
	}
	if _, present := locations["name"]; present && strings.TrimSpace(recipe.Name) == "" {
		validator.add("required", "name", "name must not be empty when provided")
	} else if utf8.RuneCountInString(recipe.Name) > maxNameLength {
		validator.add("too_long", "name", fmt.Sprintf("name exceeds %d characters", maxNameLength))
	}
	if len(recipe.Repositories) == 0 {
		validator.add("required", "repositories", "at least one repository is required")
	}
	if len(recipe.Repositories) > maxRepositories {
		validator.add("too_many_repositories", "repositories", fmt.Sprintf("at most %d repositories are allowed", maxRepositories))
	}
	if len(recipe.Jobs) > maxJobs {
		validator.add("too_many_jobs", "jobs", fmt.Sprintf("at most %d jobs are allowed", maxJobs))
	}
}

func validateRepositories(repositories map[string]Repository, validator *validator) {
	for _, id := range sortedKeys(repositories) {
		repository := repositories[id]
		basePath := "repositories." + id
		if !validID(id) {
			validator.add("invalid_id", basePath, "repository ID must match ^[a-z][a-z0-9_-]{0,63}$")
		}
		if strings.TrimSpace(repository.URL) == "" {
			validator.add("required", basePath+".url", "repository URL is required")
		} else if !validGitURL(repository.URL) {
			validator.add("invalid_git_url", basePath+".url", "repository URL must use HTTP(S), SSH, git, file://, or SCP syntax")
		}
		if strings.TrimSpace(repository.Ref) == "" {
			validator.add("required", basePath+".ref", "repository ref is required")
		}
	}
}

func validateJobs(recipe *Recipe, validator *validator) {
	for _, id := range sortedKeys(recipe.Jobs) {
		validateJob(id, recipe.Jobs[id], recipe, validator)
	}
}

func validateJob(id string, job Job, recipe *Recipe, validator *validator) {
	basePath := "jobs." + id
	if !validID(id) {
		validator.add("invalid_id", basePath, "job ID must match ^[a-z][a-z0-9_-]{0,63}$")
	}
	if _, ok := recipe.Repositories[job.Repository]; !ok {
		if strings.TrimSpace(job.Repository) == "" {
			validator.add("required", basePath+".repository", "job repository is required")
		} else {
			validator.add("unknown_repository", basePath+".repository", "unknown repository: "+job.Repository)
		}
	}
	if !validWorkingDirectory(job.WorkingDirectory) {
		validator.add("invalid_path", basePath+".working-directory", "working-directory must be a relative path inside the repository")
	}
	validateSteps(job.Steps, basePath, validator)
	validateDependencies(id, job.Needs, recipe.Jobs, basePath, validator)
}

func validateSteps(steps []Step, basePath string, validator *validator) {
	if len(steps) == 0 {
		validator.add("required", basePath+".steps", "at least one step is required")
	}
	if len(steps) > maxStepsPerJob {
		validator.add("too_many_steps", basePath+".steps", fmt.Sprintf("at most %d steps are allowed", maxStepsPerJob))
	}
	for index, step := range steps {
		if strings.TrimSpace(step.Run) == "" {
			validator.add("required", fmt.Sprintf("%s.steps[%d].run", basePath, index), "step run command is required")
		}
	}
}

func validateDependencies(id string, dependencies []string, jobs map[string]Job, basePath string, validator *validator) {
	if len(dependencies) > maxDependenciesPerJob {
		validator.add("too_many_dependencies", basePath+".needs", fmt.Sprintf("at most %d dependencies are allowed", maxDependenciesPerJob))
	}
	seen := make(map[string]struct{}, len(dependencies))
	for index, dependency := range dependencies {
		dependencyPath := fmt.Sprintf("%s.needs[%d]", basePath, index)
		if _, duplicate := seen[dependency]; duplicate {
			validator.add("duplicate_dependency", dependencyPath, "duplicate dependency: "+dependency)
		}
		seen[dependency] = struct{}{}
		if dependency == id {
			validator.add("self_dependency", dependencyPath, "job cannot depend on itself")
		} else if _, ok := jobs[dependency]; !ok {
			validator.add("unknown_job", dependencyPath, "unknown job: "+dependency)
		}
	}
}

func validateCycles(jobs map[string]Job, validator *validator) {
	const (
		unvisited = iota
		visiting
		visited
	)
	state := make(map[string]int, len(jobs))
	var visit func(string)
	visit = func(id string) {
		state[id] = visiting
		for index, dependency := range jobs[id].Needs {
			if dependency == id {
				continue
			}
			if _, ok := jobs[dependency]; !ok {
				continue
			}
			switch state[dependency] {
			case unvisited:
				visit(dependency)
			case visiting:
				validator.add("dependency_cycle", fmt.Sprintf("jobs.%s.needs[%d]", id, index), fmt.Sprintf("dependency cycle between %s and %s", id, dependency))
			}
		}
		state[id] = visited
	}
	for _, id := range sortedKeys(jobs) {
		if state[id] == unvisited {
			visit(id)
		}
	}
}

func validGitURL(value string) bool {
	if value == "" || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	if parsed.Scheme != "" {
		switch parsed.Scheme {
		case "http", "https", "ssh", "git":
			return parsed.Host != "" && strings.Trim(parsed.Path, "/") != ""
		case "file":
			return parsed.Path != ""
		default:
			return false
		}
	}

	at := strings.IndexByte(value, '@')
	colon := strings.IndexByte(value, ':')
	return at > 0 && colon > at+1 && colon < len(value)-1
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 64 || id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for _, char := range id[1:] {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			continue
		}
		return false
	}
	return true
}

func validWorkingDirectory(value string) bool {
	if value == "" || value == "." {
		return true
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	if strings.HasPrefix(normalized, "/") || len(normalized) >= 2 && normalized[1] == ':' {
		return false
	}
	cleaned := path.Clean(normalized)
	return cleaned == normalized && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

type location struct{ line, column int }

type validator struct {
	locations map[string]location
	errors    []ValidationError
}

func (v *validator) add(code, valuePath, message string) {
	loc := nearestLocation(valuePath, v.locations)
	v.errors = append(v.errors, ValidationError{Code: code, Path: valuePath, Line: loc.line, Column: loc.column, Message: message})
}

func pathAtLine(locations map[string]location, line int) string {
	if line == 0 {
		return ""
	}
	paths := make([]string, 0)
	for valuePath, loc := range locations {
		if loc.line == line {
			paths = append(paths, valuePath)
		}
	}
	if len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	return paths[0]
}

func nearestLocation(valuePath string, locations map[string]location) location {
	candidate := valuePath
	for {
		if loc, ok := locations[candidate]; ok {
			return loc
		}
		if index := strings.LastIndexAny(candidate, ".["); index >= 0 {
			candidate = candidate[:index]
			continue
		}
		return location{}
	}
}

func collectLocations(node *yaml.Node, nodePath string, locations map[string]location) {
	if node == nil {
		return
	}
	if nodePath != "" {
		locations[nodePath] = location{line: node.Line, column: node.Column}
	}
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			childPath := joinPath(nodePath, key.Value)
			locations[childPath] = location{line: key.Line, column: key.Column}
			collectLocations(value, childPath, locations)
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			collectLocations(child, fmt.Sprintf("%s[%d]", nodePath, index), locations)
		}
	}
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
