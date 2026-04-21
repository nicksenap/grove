package workspace

import (
	"fmt"
	"os"

	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
)

// ResolveWorkspace finds a workspace by name or auto-detects from cwd.
func ResolveWorkspace(name string) (*models.Workspace, error) {
	if name != "" {
		ws, err := state.GetWorkspace(name)
		if err != nil {
			return nil, err
		}
		if ws == nil {
			return nil, fmt.Errorf("workspace %s not found", name)
		}
		return ws, nil
	}

	// Auto-detect from cwd
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("cannot determine working directory: %w", err)
	}

	ws, err := state.FindWorkspaceByPath(cwd)
	if err != nil {
		return nil, err
	}
	if ws != nil {
		return ws, nil
	}

	return nil, fmt.Errorf("not inside a workspace. Provide a workspace name or cd into one")
}
