package cmd

import (
	"testing"

	"github.com/nicksenap/grove/internal/machine"
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
