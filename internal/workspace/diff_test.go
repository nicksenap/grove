package workspace

import (
	"io"
	"os/exec"
	"testing"
)

func TestDiffExitCode(t *testing.T) {
	tests := map[string]struct {
		err  error
		want int
	}{
		"ok":            {nil, 0},
		"differences":   {exec.Command("sh", "-c", "exit 1").Run(), 1},
		"git error":     {exec.Command("sh", "-c", "exit 129").Run(), 129},
		"not started":   {exec.Command("/nonexistent/git").Run(), 128},
		"killed signal": {exec.Command("sh", "-c", "kill -TERM $$").Run(), 128},
	}
	for name, tt := range tests {
		if got := diffExitCode("repo", tt.err, io.Discard); got != tt.want {
			t.Errorf("%s: diffExitCode = %d, want %d", name, got, tt.want)
		}
	}
}

func TestBrokenPipe(t *testing.T) {
	if !brokenPipe(exec.Command("sh", "-c", "kill -PIPE $$").Run()) {
		t.Error("SIGPIPE should be a broken pipe")
	}
	if brokenPipe(exec.Command("sh", "-c", "kill -TERM $$").Run()) {
		t.Error("SIGTERM is not a broken pipe")
	}
}
