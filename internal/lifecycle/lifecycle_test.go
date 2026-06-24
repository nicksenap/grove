package lifecycle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nicksenap/grove/internal/config"
)

func TestShellQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"simple", "my-workspace", "'my-workspace'"},
		{"spaces", "my workspace", "'my workspace'"},
		{"single quotes", "it's", "'it'\\''s'"},
		{"semicolon injection", "x; rm -rf ~", "'x; rm -rf ~'"},
		{"subshell injection", "$(whoami)", "'$(whoami)'"},
		{"backtick injection", "`whoami`", "'`whoami`'"},
		{"pipe injection", "x | cat /etc/passwd", "'x | cat /etc/passwd'"},
		{"ampersand injection", "x && echo pwned", "'x && echo pwned'"},
		{"newline injection", "x\nrm -rf /", "'x\nrm -rf /'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuote(tt.in)
			if got != tt.want {
				t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		vars Vars
		want string
	}{
		{
			name: "no placeholders",
			cmd:  "zellij action close-pane",
			vars: Vars{},
			want: "zellij action close-pane",
		},
		{
			name: "all placeholders",
			cmd:  "echo {name} {path} {branch}",
			vars: Vars{Name: "ws-1", Path: "/tmp/ws", Branch: "feat/login"},
			want: "echo 'ws-1' '/tmp/ws' 'feat/login'",
		},
		{
			name: "injection in branch name",
			cmd:  "echo {branch}",
			vars: Vars{Branch: "feat/x; rm -rf ~"},
			want: "echo 'feat/x; rm -rf ~'",
		},
		{
			name: "injection via subshell",
			cmd:  "echo {name}",
			vars: Vars{Name: "$(rm -rf /)"},
			want: "echo '$(rm -rf /)'",
		},
		{
			name: "empty vars expand to nothing",
			cmd:  "do-thing {name}",
			vars: Vars{},
			want: "do-thing ",
		},
		{
			name: "source placeholders",
			cmd:  "seed {source_url} {source_ref} {source_title}",
			vars: Vars{SourceURL: "https://github.com/o/r/pull/7", SourceRef: "7", SourceTitle: "Fix bug"},
			want: "seed 'https://github.com/o/r/pull/7' '7' 'Fix bug'",
		},
		{
			name: "empty source placeholders expand to nothing",
			cmd:  "seed {source_url}{source_ref}{source_title}",
			vars: Vars{Name: "ws"},
			want: "seed ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expand(tt.cmd, tt.vars)
			if got != tt.want {
				t.Errorf("expand(%q, %+v) = %q, want %q", tt.cmd, tt.vars, got, tt.want)
			}
		})
	}
}

func TestRunDisabled(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "fired")

	// Point config at a temp config.toml with a post_create hook that writes a marker.
	origPath := config.ConfigPath
	config.ConfigPath = filepath.Join(dir, "config.toml")
	t.Cleanup(func() { config.ConfigPath = origPath })

	cfg := "[hooks]\npost_create = \"touch " + marker + "\"\n"
	if err := os.WriteFile(config.ConfigPath, []byte(cfg), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// With Disabled set, Run must skip the hook entirely and report ErrNoHook.
	Disabled = true
	t.Cleanup(func() { Disabled = false })

	if err := Run("post_create", Vars{}); !errors.Is(err, ErrNoHook) {
		t.Fatalf("Run(disabled) = %v, want ErrNoHook", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("hook fired despite Disabled (marker exists: %v)", err)
	}

	// Sanity: with Disabled cleared, the same hook fires.
	Disabled = false
	if err := Run("post_create", Vars{}); err != nil {
		t.Fatalf("Run(enabled) = %v, want nil", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("hook did not fire when enabled: %v", err)
	}
}
