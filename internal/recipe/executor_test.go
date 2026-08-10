package recipe

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestExecutorRunsNeedsBeforeDependentsAndStepsSequentially(t *testing.T) {
	recipe := &Recipe{Jobs: map[string]Job{
		"setup":  {Repository: "api", Steps: []Step{{Run: "one"}, {Run: "two"}}},
		"verify": {Repository: "api", Needs: []string{"setup"}, Steps: []Step{{Run: "three"}}},
	}}
	var mu sync.Mutex
	var commands []string
	executor := Executor{RunStep: func(_ context.Context, _, command string, _ io.Writer) error {
		mu.Lock()
		commands = append(commands, command)
		mu.Unlock()
		return nil
	}}

	report, err := executor.Execute(context.Background(), recipe, map[string]string{"api": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"one", "two", "three"}
	if len(commands) != len(want) {
		t.Fatalf("commands = %v, want %v", commands, want)
	}
	for i := range want {
		if commands[i] != want[i] {
			t.Fatalf("commands = %v, want %v", commands, want)
		}
	}
	if len(report.Jobs) != 2 || report.Jobs[0].ID != "setup" || report.Jobs[1].ID != "verify" {
		t.Fatalf("unexpected report: %+v", report)
	}
}

func TestExecutorParallelizesAcrossRepositoriesButSerializesWithinOne(t *testing.T) {
	recipe := &Recipe{Jobs: map[string]Job{
		"api-a": {Repository: "api", Steps: []Step{{Run: "api-a"}}},
		"api-b": {Repository: "api", Steps: []Step{{Run: "api-b"}}},
		"web":   {Repository: "web", Steps: []Step{{Run: "web"}}},
	}}
	var mu sync.Mutex
	running := map[string]int{}
	maxByRepo := map[string]int{}
	maxGlobal := 0
	executor := Executor{MaxParallel: 4, RunStep: func(_ context.Context, dir, _ string, _ io.Writer) error {
		mu.Lock()
		running[dir]++
		global := 0
		for _, count := range running {
			global += count
		}
		if running[dir] > maxByRepo[dir] {
			maxByRepo[dir] = running[dir]
		}
		if global > maxGlobal {
			maxGlobal = global
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		running[dir]--
		mu.Unlock()
		return nil
	}}
	paths := map[string]string{"api": t.TempDir(), "web": t.TempDir()}

	if _, err := executor.Execute(context.Background(), recipe, paths); err != nil {
		t.Fatal(err)
	}
	if maxGlobal < 2 {
		t.Fatalf("max global concurrency = %d, want at least 2", maxGlobal)
	}
	for dir, maxRunning := range maxByRepo {
		if maxRunning != 1 {
			t.Fatalf("concurrency for %s = %d, want 1", dir, maxRunning)
		}
	}
}

func TestExecutorUsesLexicalOrderForReadyJobsInSameRepository(t *testing.T) {
	recipe := &Recipe{Jobs: map[string]Job{
		"z-last":  {Repository: "api", Steps: []Step{{Run: "z"}}},
		"a-first": {Repository: "api", Steps: []Step{{Run: "a"}}},
	}}
	var commands []string
	executor := Executor{RunStep: func(_ context.Context, _, command string, _ io.Writer) error {
		commands = append(commands, command)
		return nil
	}}
	if _, err := executor.Execute(context.Background(), recipe, map[string]string{"api": t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || commands[0] != "a" || commands[1] != "z" {
		t.Fatalf("commands = %v, want [a z]", commands)
	}
}

func TestExecutorFailureCancelsQueuedJobsAndIdentifiesStep(t *testing.T) {
	boom := errors.New("boom")
	recipe := &Recipe{Jobs: map[string]Job{
		"setup": {Repository: "api", Steps: []Step{{Name: "Install", Run: "fail"}}},
		"later": {Repository: "api", Needs: []string{"setup"}, Steps: []Step{{Run: "must-not-run"}}},
	}}
	var commands []string
	executor := Executor{RunStep: func(_ context.Context, _, command string, _ io.Writer) error {
		commands = append(commands, command)
		if command == "fail" {
			return boom
		}
		return nil
	}}

	_, err := executor.Execute(context.Background(), recipe, map[string]string{"api": t.TempDir()})
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) {
		t.Fatalf("error = %v, want ExecutionError", err)
	}
	if executionErr.Code != ErrorStepFailed || executionErr.Job != "setup" || executionErr.Step != 1 || executionErr.StepName != "Install" {
		t.Fatalf("unexpected execution error: %+v", executionErr)
	}
	if len(commands) != 1 || commands[0] != "fail" {
		t.Fatalf("commands = %v, queued job should not run", commands)
	}
}

func TestExecutorEscapesControlCharactersInStepMetadata(t *testing.T) {
	var output bytes.Buffer
	recipe := &Recipe{Jobs: map[string]Job{
		"setup": {Repository: "api", Steps: []Step{{Name: "Build\x1b[31m\u009b", Run: "true"}}},
	}}
	executor := Executor{Output: &output, RunStep: func(_ context.Context, _, _ string, _ io.Writer) error { return nil }}
	if _, err := executor.Execute(context.Background(), recipe, map[string]string{"api": t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(output.String(), '\x1b') || strings.ContainsRune(output.String(), '\u009b') ||
		!strings.Contains(output.String(), `\x1b`) || !strings.Contains(output.String(), `\x9b`) {
		t.Fatalf("metadata was not terminal-safe: %q", output.String())
	}
}

func TestExecutorDefaultRunnerUsesWorkingDirectory(t *testing.T) {
	worktree := t.TempDir()
	workingDirectory := filepath.Join(worktree, "services", "api")
	if err := os.MkdirAll(workingDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	recipe := &Recipe{Jobs: map[string]Job{
		"setup": {Repository: "api", WorkingDirectory: "services/api", Steps: []Step{{Run: "printf ready > marker.txt"}}},
	}}
	if _, err := (Executor{}).Execute(context.Background(), recipe, map[string]string{"api": worktree}); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(workingDirectory, "marker.txt")); err != nil || string(data) != "ready" {
		t.Fatalf("marker = %q, err=%v", data, err)
	}
}

func TestExecutorRejectsWorkingDirectorySymlinkEscape(t *testing.T) {
	worktree := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(worktree, "escape")); err != nil {
		t.Fatal(err)
	}
	recipe := &Recipe{Jobs: map[string]Job{
		"setup": {Repository: "api", WorkingDirectory: "escape", Steps: []Step{{Run: "touch escaped"}}},
	}}
	_, err := (Executor{}).Execute(context.Background(), recipe, map[string]string{"api": worktree})
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != ErrorStepFailed {
		t.Fatalf("error = %v, want working-directory ExecutionError", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("command escaped worktree: %v", err)
	}
}

func TestExecutorTimeoutKillsChildProcessGroup(t *testing.T) {
	worktree := t.TempDir()
	pidFile := filepath.Join(worktree, "child.pid")
	command := "sleep 30 & echo $! > " + pidFile + "; wait"
	recipe := &Recipe{Jobs: map[string]Job{
		"slow": {Repository: "api", TimeoutMinutes: 1, Steps: []Step{{Run: command}}},
	}}
	executor := Executor{timeoutUnit: 200 * time.Millisecond}
	_, err := executor.Execute(context.Background(), recipe, map[string]string{"api": worktree})
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != ErrorJobTimeout {
		t.Fatalf("error = %v, want timeout", err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("child process %d survived job timeout", pid)
	}
}

func TestExecutorHonorsParentCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recipe := &Recipe{Jobs: map[string]Job{
		"setup": {Repository: "api", Steps: []Step{{Run: "must-not-run"}}},
	}}
	ran := false
	executor := Executor{RunStep: func(_ context.Context, _, _ string, _ io.Writer) error {
		ran = true
		return nil
	}}
	_, err := executor.Execute(ctx, recipe, map[string]string{"api": t.TempDir()})
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != ErrorCancelled || ran {
		t.Fatalf("error=%v ran=%v, want cancellation before execution", err, ran)
	}
}

func TestExecutorCancellationStopsRunningSibling(t *testing.T) {
	recipe := &Recipe{Jobs: map[string]Job{
		"fail": {Repository: "api", Steps: []Step{{Run: "fail"}}},
		"slow": {Repository: "web", Steps: []Step{{Run: "slow"}}},
	}}
	slowCancelled := make(chan struct{}, 1)
	executor := Executor{RunStep: func(ctx context.Context, _, command string, _ io.Writer) error {
		if command == "fail" {
			time.Sleep(20 * time.Millisecond)
			return errors.New("boom")
		}
		<-ctx.Done()
		slowCancelled <- struct{}{}
		return ctx.Err()
	}}
	_, err := executor.Execute(context.Background(), recipe, map[string]string{"api": t.TempDir(), "web": t.TempDir()})
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Job != "fail" {
		t.Fatalf("error = %v, want original failing job", err)
	}
	select {
	case <-slowCancelled:
	case <-time.After(time.Second):
		t.Fatal("running sibling was not cancelled")
	}
}

func TestExecutorEnforcesDefaultConcurrencyLimit(t *testing.T) {
	jobs := make(map[string]Job)
	worktrees := make(map[string]string)
	for i := 0; i < 8; i++ {
		id := fmt.Sprintf("job-%d", i)
		repoID := fmt.Sprintf("repo-%d", i)
		jobs[id] = Job{Repository: repoID, Steps: []Step{{Run: id}}}
		worktrees[repoID] = t.TempDir()
	}
	var mu sync.Mutex
	running, maxRunning := 0, 0
	executor := Executor{RunStep: func(_ context.Context, _, _ string, _ io.Writer) error {
		mu.Lock()
		running++
		if running > maxRunning {
			maxRunning = running
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return nil
	}}
	if _, err := executor.Execute(context.Background(), &Recipe{Jobs: jobs}, worktrees); err != nil {
		t.Fatal(err)
	}
	if maxRunning != DefaultMaxParallel {
		t.Fatalf("max concurrency = %d, want %d", maxRunning, DefaultMaxParallel)
	}
}

func TestExecutorTimesOutJob(t *testing.T) {
	recipe := &Recipe{Jobs: map[string]Job{
		"slow": {Repository: "api", TimeoutMinutes: 1, Steps: []Step{{Run: "wait"}}},
	}}
	executor := Executor{
		timeoutUnit: 10 * time.Millisecond,
		RunStep: func(ctx context.Context, _, _ string, _ io.Writer) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}

	_, err := executor.Execute(context.Background(), recipe, map[string]string{"api": t.TempDir()})
	var executionErr *ExecutionError
	if !errors.As(err, &executionErr) || executionErr.Code != ErrorJobTimeout {
		t.Fatalf("error = %v, want timeout ExecutionError", err)
	}
}
