package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/config"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/models"
)

func TestParseRepoList(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"single", "api", []string{"api"}},
		{"several", "api,web,worker", []string{"api", "web", "worker"}},
		{"padded", " api , web ", []string{"api", "web"}},
		// Trailing and doubled separators used to yield an empty repo name, which
		// surfaced as the useless error `repo  not found`.
		{"trailing comma", "api,", []string{"api"}},
		{"leading comma", ",api", []string{"api"}},
		{"empty middle entry", "api, ,web", []string{"api", "web"}},
		{"only separators", ",,", nil},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRepoList(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseRepoList(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseRepoList(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

// "There are no workspaces" must report one code no matter which command hit it —
// gw rename used to report INTERNAL (exit 1) while add-repo and remove-repo
// reported NO_WORKSPACES (exit 3), so the code told an agent nothing.
func TestNoWorkspacesErrIsOneClassifiedError(t *testing.T) {
	err := noWorkspacesErr()
	if machine.CodeFor(err) != machine.CodeNoWorkspaces {
		t.Errorf("code = %s, want %s", machine.CodeFor(err), machine.CodeNoWorkspaces)
	}
	if machine.ExitCodeFor(err) != machine.ExitNotFound {
		t.Errorf("exit = %d, want %d", machine.ExitCodeFor(err), machine.ExitNotFound)
	}
	if err.Fix == "" || len(err.NextActions) == 0 {
		t.Error("the error should tell the caller how to proceed")
	}
}

// ---------------------------------------------------------------------------
// Interactive workspace selection
// ---------------------------------------------------------------------------

// withGroveDir points state lookups at a temp dir holding the given workspaces.
func withGroveDir(t *testing.T, workspaces []models.Workspace) {
	t.Helper()
	dir := t.TempDir()
	data, err := json.MarshalIndent(workspaces, "", "  ")
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	original := config.GroveDir
	config.GroveDir = dir
	t.Cleanup(func() { config.GroveDir = original })
}

func TestPickWorkspaceNameOffersEveryWorkspace(t *testing.T) {
	withGroveDir(t, []models.Workspace{
		{Name: "alpha", Branch: "feat/a"},
		{Name: "beta", Branch: "feat/b"},
	})

	p := newScriptedPrompter(t)
	p.picks["Select workspace"] = "beta"
	withPrompter(t, p)

	if got := pickWorkspaceName("Select workspace:"); got != "beta" {
		t.Errorf("got %q, want beta", got)
	}
}

func TestPickWorkspaceNamesSupportsMultiSelect(t *testing.T) {
	withGroveDir(t, []models.Workspace{
		{Name: "alpha"}, {Name: "beta"}, {Name: "gamma"},
	})

	p := newScriptedPrompter(t)
	p.multi["Select workspaces"] = []string{"alpha", "gamma"}
	withPrompter(t, p)

	got := pickWorkspaceNames("Select workspaces to delete:")
	if len(got) != 2 || got[0] != "alpha" || got[1] != "gamma" {
		t.Errorf("got %v, want [alpha gamma]", got)
	}
}

// The cmd layer offers every workspace and nothing more; whether a sole choice is
// auto-selected is picker's own behavior, covered by picker's tests. The seam stops
// at this boundary deliberately — reimplementing that shortcut here would be a
// second definition of it.
func TestPickWorkspaceNameOffersTheOnlyWorkspace(t *testing.T) {
	withGroveDir(t, []models.Workspace{{Name: "only"}})

	p := newScriptedPrompter(t)
	p.picks["Select workspace"] = "only"
	withPrompter(t, p)

	if got := pickWorkspaceName("Select workspace:"); got != "only" {
		t.Errorf("got %q, want only", got)
	}
	if offered := p.choicesFor("Select workspace"); len(offered) != 1 || offered[0] != "only" {
		t.Errorf("offered %v, want exactly [only]", offered)
	}
}
