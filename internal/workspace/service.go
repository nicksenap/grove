package workspace

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/stats"
)

// Service orchestrates workspace operations with injectable dependencies.
type Service struct {
	State          *state.Store
	Stats          *stats.Tracker
	WorkspaceDir   string
	RunCmd         func(dir, cmd string) error
	RunCmdSilent   func(dir, cmd string) error
	RemoveWorktree func(repo, path string, force bool) error // optional test seam
	PruneWorktree  func(repo string) error                   // optional test seam
	RepairWorktree func(repo, path string) error             // optional test seam
	UnlinkTrash    func(path string) error                   // optional test seam
	StartUnlink    func(path string) error                   // optional test seam
	RemoveState    func(name string) error                   // optional test seam
}

// NewService creates a Service with production dependencies.
func NewService() *Service {
	wsDir := config.DefaultWorkspaceDir
	if cfg, err := config.Load(); err == nil && cfg != nil && cfg.WorkspaceDir != "" {
		wsDir = cfg.WorkspaceDir
	}
	return &Service{
		State:        state.NewStore(config.GroveDir),
		Stats:        stats.NewTracker(config.GroveDir),
		WorkspaceDir: wsDir,
		RunCmd:       prodRunCmd,
		RunCmdSilent: prodRunCmdSilent,
		StartUnlink:  startUnlinkProcess,
	}
}

var newUnlinkCommand = func(path string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		exe = "gw"
	}
	return exec.Command(exe, "unlink-trash", path)
}

func startUnlinkProcess(path string) error {
	cmd := newUnlinkCommand(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() //nolint:errcheck -- unlink failures are logged by the child
	return nil
}

func (s *Service) removeWorktree(repo, path string, force bool) error {
	if s.RemoveWorktree != nil {
		return s.RemoveWorktree(repo, path, force)
	}
	return gitops.WorktreeRemove(repo, path, force)
}

func prodRunCmd(dir, cmdStr string) error {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func prodRunCmdSilent(dir, cmdStr string) error {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = dir
	return cmd.Run()
}
