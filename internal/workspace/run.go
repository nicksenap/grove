package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/nicksenap/grove/internal/console"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/streamio"
)

// RunnableRepo holds a repo's run commands.
type RunnableRepo struct {
	RepoName     string
	WorktreePath string
	SourceRepo   string
	RunCmds      []string
	PreRun       string
	PostRun      string
}

// GetRunnable returns repos that have run hooks defined.
func GetRunnable(ws *models.Workspace) []RunnableRepo {
	var result []RunnableRepo
	for _, r := range ws.Repos {
		cfg, _ := gitops.ReadGroveConfig(r.SourceRepo)
		if cfg == nil || len(cfg.Run) == 0 {
			continue
		}
		result = append(result, RunnableRepo{
			RepoName:     r.RepoName,
			WorktreePath: r.WorktreePath,
			SourceRepo:   r.SourceRepo,
			RunCmds:      []string(cfg.Run),
			PreRun:       cfg.PreRun,
			PostRun:      cfg.PostRun,
		})
	}
	return result
}

// Run executes run hooks for a workspace, printing output directly.
func Run(wsName string) error {
	ws, err := ResolveWorkspace(wsName)
	if err != nil {
		return err
	}
	ovenSlot, err := NewService().verifyOvenWorkspaceIfClaimed(*ws)
	if err != nil {
		return err
	}
	if ovenSlot != nil {
		applyOvenPhysicalPaths(ws, *ovenSlot)
	}

	runnable := GetRunnable(ws)
	if len(runnable) == 0 {
		console.Info("No repos have a run hook configured in .grove.toml")
		return nil
	}

	// Pre-run hooks (parallel)
	runHooks(runnable, "pre_run", func(r RunnableRepo) string { return r.PreRun })

	// Spawn all processes
	var procs []*exec.Cmd
	var mu sync.Mutex

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	var shuttingDown atomic.Bool

	var wg sync.WaitGroup
	for _, r := range runnable {
		cmdStr := strings.Join(r.RunCmds, " && ")
		cmd := exec.Command("sh", "-c", cmdStr)
		cmd.Dir = r.WorktreePath
		cmd.Stdin = nil
		// Give each child its own process group so the terminal's Ctrl+C
		// SIGINT doesn't reach them directly — only gw receives it.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		// Prefix output with repo name
		prefix := fmt.Sprintf("[%s] ", r.RepoName)
		outW := streamio.New(prefix, os.Stdout)
		errW := streamio.New(prefix, os.Stderr)
		cmd.Stdout = outW
		cmd.Stderr = errW

		if err := cmd.Start(); err != nil {
			console.Warningf("%s: failed to start: %s", r.RepoName, err)
			continue
		}

		console.Infof("%s: started (pid %d)", r.RepoName, cmd.Process.Pid)
		mu.Lock()
		procs = append(procs, cmd)
		mu.Unlock()

		wg.Add(1)
		go func(name string, c *exec.Cmd, outW, errW *streamio.PrefixWriter) {
			defer wg.Done()
			err := c.Wait()
			// Emit any trailing line the process printed without a newline.
			outW.Flush()
			errW.Flush()
			if err != nil {
				if !shuttingDown.Load() {
					console.Warningf("%s: exited with error: %s", name, err)
				}
			} else {
				console.Infof("%s: exited (0)", name)
			}
		}(r.RepoName, cmd, outW, errW)
	}

	// Wait for signal or all processes to complete
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-sigCh:
		shuttingDown.Store(true)
		console.Info("Shutting down...")
		mu.Lock()
		// Send SIGINT first — this is what processes expect from Ctrl+C
		// and handle most gracefully (Flask, pnpm, node all have SIGINT handlers).
		for _, p := range procs {
			if p.Process != nil {
				syscall.Kill(-p.Process.Pid, syscall.SIGINT)
			}
		}
		mu.Unlock()
		// Escalate: SIGTERM after 3s, SIGKILL after 8s
		termTimer := time.AfterFunc(3*time.Second, func() {
			mu.Lock()
			for _, p := range procs {
				if p.Process != nil {
					syscall.Kill(-p.Process.Pid, syscall.SIGTERM)
				}
			}
			mu.Unlock()
		})
		killTimer := time.AfterFunc(8*time.Second, func() {
			mu.Lock()
			for _, p := range procs {
				if p.Process != nil {
					syscall.Kill(-p.Process.Pid, syscall.SIGKILL)
				}
			}
			mu.Unlock()
		})
		wg.Wait()
		termTimer.Stop()
		killTimer.Stop()
	case <-done:
	}

	signal.Stop(sigCh)

	// Post-run hooks (parallel)
	runHooks(runnable, "post_run", func(r RunnableRepo) string { return r.PostRun })

	return nil
}

func runHooks(runnable []RunnableRepo, hookName string, getCmd func(RunnableRepo) string) {
	var wg sync.WaitGroup
	for _, r := range runnable {
		cmdStr := getCmd(r)
		if cmdStr == "" {
			continue
		}
		wg.Add(1)
		go func(repo RunnableRepo, cmd string) {
			defer wg.Done()
			c := exec.Command("sh", "-c", cmd)
			c.Dir = repo.WorktreePath
			c.Stdout = os.Stderr
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				console.Warningf("%s: %s hook failed: %s", repo.RepoName, hookName, err)
			}
		}(r, cmdStr)
	}
	wg.Wait()
}
