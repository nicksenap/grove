// Package recipe parses and validates versioned Grove Recipe files.
package recipe

const (
	MaxRecipeBytes        = 1 << 20
	maxYAMLDepth          = 32
	maxYAMLNodes          = 100_000
	maxNameLength         = 128
	maxRepositories       = 64
	maxJobs               = 256
	maxStepsPerJob        = 64
	maxDependenciesPerJob = 64
)

// Recipe is the versioned description of a prepared workspace.
type Recipe struct {
	Version      int                   `yaml:"version" json:"version"`
	Name         string                `yaml:"name" json:"name"`
	Repositories map[string]Repository `yaml:"repositories" json:"repositories"`
	Jobs         map[string]Job        `yaml:"jobs" json:"jobs"`
}

// Repository identifies one Git repository and the base ref to prepare.
type Repository struct {
	URL string `yaml:"url" json:"url"`
	Ref string `yaml:"ref" json:"ref"`
}

// Job is one node in the preparation DAG. Steps run sequentially within a job.
type Job struct {
	Repository       string   `yaml:"repository" json:"repository"`
	WorkingDirectory string   `yaml:"working-directory,omitempty" json:"working_directory,omitempty"`
	TimeoutMinutes   int      `yaml:"timeout-minutes,omitempty" json:"timeout_minutes,omitempty"`
	Needs            []string `yaml:"needs,omitempty" json:"needs,omitempty"`
	Steps            []Step   `yaml:"steps" json:"steps"`
}

// Step is one shell command in a job.
type Step struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	Run  string `yaml:"run" json:"run"`
}

// ValidationError is a stable, machine-readable Recipe diagnostic.
type ValidationError struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

// Result contains either a valid Recipe or validation errors.
type Result struct {
	Recipe *Recipe           `json:"-"`
	Errors []ValidationError `json:"errors"`
}
