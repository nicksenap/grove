package workspace

import (
	"os"
	"os/exec"

	"github.com/nicksenap/grove/internal/announce"
	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/stats"
)

// Service orchestrates workspace operations with injectable dependencies.
type Service struct {
	State        *state.Store
	Stats        *stats.Tracker
	Announce     *announce.Store
	RunCmd       func(dir, cmd string) error
	RunCmdSilent func(dir, cmd string) error
}

// NewService creates a Service with production dependencies.
func NewService() *Service {
	return &Service{
		State:        state.NewStore(config.GroveDir),
		Stats:        stats.NewTracker(config.GroveDir),
		Announce:     announce.NewStore(config.GroveDir),
		RunCmd:       prodRunCmd,
		RunCmdSilent: prodRunCmdSilent,
	}
}

// prodRunCmd runs a per-repo hook with its output visible. In machine mode its
// stdout goes to stderr: hook output is arbitrary text, and stdout is reserved
// for the single response envelope.
func prodRunCmd(dir, cmdStr string) error {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	if machine.Enabled() {
		cmd.Stdout = os.Stderr
	}
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func prodRunCmdSilent(dir, cmdStr string) error {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Dir = dir
	return cmd.Run()
}
