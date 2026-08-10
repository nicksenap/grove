package recipe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nicksenap/grove/internal/streamio"
)

const (
	DefaultMaxParallel = 4
	DefaultJobTimeout  = 360

	ErrorStepFailed   = "recipe_step_failed"
	ErrorJobTimeout   = "recipe_job_timeout"
	ErrorCancelled    = "recipe_cancelled"
	ErrorInvalidGraph = "recipe_invalid_graph"
)

// StepRunner executes one Recipe step in dir and writes combined output.
type StepRunner func(ctx context.Context, dir, command string, output io.Writer) error

// Executor runs a validated Recipe job DAG.
type Executor struct {
	MaxParallel int
	Output      io.Writer
	RunStep     StepRunner
	timeoutUnit time.Duration
}

// Report summarizes jobs that started during one execution.
type Report struct {
	Jobs []JobResult `json:"jobs"`
}

// JobResult describes one completed or failed job.
type JobResult struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	StepsCompleted int    `json:"steps"`
}

// ExecutionError identifies the Recipe job and step that failed.
type ExecutionError struct {
	Code     string `json:"code"`
	Job      string `json:"job,omitempty"`
	Step     int    `json:"step,omitempty"`
	StepName string `json:"step_name,omitempty"`
	Err      error  `json:"-"`
}

func (e *ExecutionError) Error() string {
	if e.Job == "" {
		return e.Err.Error()
	}
	step := ""
	if e.Step > 0 {
		step = fmt.Sprintf(" step %d", e.Step)
		if e.StepName != "" {
			step += " (" + e.StepName + ")"
		}
	}
	return fmt.Sprintf("Recipe job %s%s: %s", e.Job, step, e.Err)
}

func (e *ExecutionError) Unwrap() error { return e.Err }

type jobOutcome struct {
	id     string
	result JobResult
	err    *ExecutionError
}

type executionState struct {
	executor     Executor
	recipe       *Recipe
	worktrees    map[string]string
	output       io.Writer
	runStep      StepRunner
	timeoutUnit  time.Duration
	pending      map[string]struct{}
	completed    map[string]bool
	runningRepos map[string]bool
	outcomes     chan jobOutcome
}

func (s *executionState) startReady(ctx context.Context, running, maxParallel int) int {
	for _, id := range sortedPending(s.pending) {
		if running >= maxParallel {
			break
		}
		job := s.recipe.Jobs[id]
		if s.runningRepos[job.Repository] || !dependenciesComplete(job.Needs, s.completed) {
			continue
		}
		delete(s.pending, id)
		s.runningRepos[job.Repository] = true
		running++
		go func(id string, job Job) {
			s.outcomes <- s.executor.runJob(ctx, id, job, s.worktrees[job.Repository], s.output, s.runStep, s.timeoutUnit)
		}(id, job)
	}
	return running
}

// Execute runs ready jobs concurrently, while serializing jobs that target the
// same repository. The first real failure cancels and drains running siblings.
func (e Executor) Execute(ctx context.Context, recipe *Recipe, worktrees map[string]string) (Report, error) {
	if len(recipe.Jobs) == 0 {
		return Report{Jobs: []JobResult{}}, nil
	}
	maxParallel := e.MaxParallel
	if maxParallel <= 0 {
		maxParallel = DefaultMaxParallel
	}
	if maxParallel > len(recipe.Jobs) {
		maxParallel = len(recipe.Jobs)
	}
	output := e.Output
	if output == nil {
		output = io.Discard
	}
	runStep := e.RunStep
	if runStep == nil {
		runStep = runShellStep
	}
	timeoutUnit := e.timeoutUnit
	if timeoutUnit <= 0 {
		timeoutUnit = time.Minute
	}
	output = &lockedWriter{writer: output}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	state := executionState{
		executor: e, recipe: recipe, worktrees: worktrees, output: output,
		runStep: runStep, timeoutUnit: timeoutUnit,
		pending:      make(map[string]struct{}, len(recipe.Jobs)),
		completed:    make(map[string]bool, len(recipe.Jobs)),
		runningRepos: make(map[string]bool, len(recipe.Repositories)),
		outcomes:     make(chan jobOutcome, len(recipe.Jobs)),
	}
	for id := range recipe.Jobs {
		state.pending[id] = struct{}{}
	}
	running := 0
	results := make(map[string]JobResult, len(recipe.Jobs))
	var firstFailure *ExecutionError

	for len(state.pending) > 0 || running > 0 {
		if firstFailure == nil && runCtx.Err() == nil {
			running = state.startReady(runCtx, running, maxParallel)
		}

		if running == 0 {
			if firstFailure != nil {
				break
			}
			if err := runCtx.Err(); err != nil {
				firstFailure = &ExecutionError{Code: ErrorCancelled, Err: err}
				break
			}
			firstFailure = &ExecutionError{Code: ErrorInvalidGraph, Err: errors.New("Recipe jobs cannot be scheduled")}
			break
		}

		outcome := <-state.outcomes
		running--
		state.runningRepos[recipe.Jobs[outcome.id].Repository] = false
		results[outcome.id] = outcome.result
		if outcome.err != nil {
			if firstFailure == nil {
				firstFailure = outcome.err
				cancel()
			}
			continue
		}
		state.completed[outcome.id] = true
	}

	report := Report{Jobs: sortedJobResults(results)}
	if firstFailure != nil {
		return report, firstFailure
	}
	return report, nil
}

func (e Executor) runJob(ctx context.Context, id string, job Job, worktree string, output io.Writer, runStep StepRunner, timeoutUnit time.Duration) jobOutcome {
	result := JobResult{ID: id, Status: "succeeded"}
	timeout := job.TimeoutMinutes
	if timeout == 0 {
		timeout = DefaultJobTimeout
	}
	jobCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*timeoutUnit)
	defer cancel()

	dir, err := resolveWorkingDirectory(worktree, job.WorkingDirectory)
	if err != nil {
		result.Status = "failed"
		stepName := ""
		if len(job.Steps) > 0 {
			stepName = job.Steps[0].Name
		}
		return jobOutcome{id: id, result: result, err: &ExecutionError{Code: ErrorStepFailed, Job: id, Step: 1, StepName: stepName, Err: err}}
	}
	for index, step := range job.Steps {
		label := step.Name
		if label == "" {
			label = "run"
		}
		fmt.Fprintf(output, "[%s/%d] %s\n", id, index+1, safeRecipeMetadata(label))
		prefix := fmt.Sprintf("[%s/%d] ", id, index+1)
		stepOutput := streamio.New(prefix, output)
		err := runStep(jobCtx, dir, step.Run, stepOutput)
		stepOutput.Flush()
		if err == nil {
			result.StepsCompleted++
			continue
		}
		result.Status = "failed"
		code := ErrorStepFailed
		cause := err
		if errors.Is(jobCtx.Err(), context.DeadlineExceeded) {
			code = ErrorJobTimeout
			cause = fmt.Errorf("timed out after %d minutes", timeout)
		} else if ctx.Err() != nil {
			code = ErrorCancelled
			cause = ctx.Err()
		}
		return jobOutcome{id: id, result: result, err: &ExecutionError{Code: code, Job: id, Step: index + 1, StepName: step.Name, Err: cause}}
	}
	return jobOutcome{id: id, result: result}
}

func resolveWorkingDirectory(worktree, relative string) (string, error) {
	root, err := filepath.EvalSymlinks(worktree)
	if err != nil {
		return "", fmt.Errorf("resolving worktree path: %w", err)
	}
	candidate := root
	if relative != "" && relative != "." {
		candidate = filepath.Join(root, filepath.FromSlash(relative))
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolving working-directory: %w", err)
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("working-directory resolves outside repository worktree")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("reading working-directory: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working-directory is not a directory")
	}
	return resolved, nil
}

func safeRecipeMetadata(value string) string {
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

func runShellStep(ctx context.Context, dir, command string, output io.Writer) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdin = nil
	cmd.Stdout = output
	cmd.Stderr = output
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}

func dependenciesComplete(needs []string, completed map[string]bool) bool {
	for _, dependency := range needs {
		if !completed[dependency] {
			return false
		}
	}
	return true
}

func sortedPending(pending map[string]struct{}) []string {
	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sortedJobResults(results map[string]JobResult) []JobResult {
	ids := make([]string, 0, len(results))
	for id := range results {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sorted := make([]JobResult, 0, len(ids))
	for _, id := range ids {
		sorted = append(sorted, results[id])
	}
	return sorted
}

type lockedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *lockedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(data)
}
