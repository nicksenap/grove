package lifecycle

import "testing"

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
