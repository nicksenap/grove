package cmd

import (
	"errors"
	"os"
	"testing"
)

func TestIsUnknownCommandErr(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"unknown command", errors.New(`unknown command "foo" for "gw"`), true},
		{"unrelated error", errors.New("some other failure"), false},
		{"contains word in context", errors.New("the unknown command handler fired"), true}, // substring match is deliberate
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnknownCommandErr(tt.err); got != tt.want {
				t.Errorf("isUnknownCommandErr(%q) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestExtractUnknownCommand(t *testing.T) {
	tests := []struct {
		name, msg, want string
	}{
		{"standard cobra format", `unknown command "foo" for "gw"`, "foo"},
		{"nested quotes command", `unknown command "plugin-thing" for "gw"`, "plugin-thing"},
		{"no quotes", `unknown command foo`, ""},
		{"only one quote", `unknown command "foo`, ""},
		{"empty command name", `unknown command "" for "gw"`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractUnknownCommand(errors.New(tt.msg)); got != tt.want {
				t.Errorf("extractUnknownCommand(%q) = %q, want %q", tt.msg, got, tt.want)
			}
		})
	}
}

func TestPluginArgs(t *testing.T) {
	// Save and restore os.Args — tests mutate it.
	orig := os.Args
	defer func() { os.Args = orig }()

	tests := []struct {
		name string
		args []string
		plug string
		want []string
	}{
		{
			name: "plugin with args",
			args: []string{"gw", "archive", "save", "--tag", "foo"},
			plug: "archive",
			want: []string{"save", "--tag", "foo"},
		},
		{
			name: "plugin with no args",
			args: []string{"gw", "archive"},
			plug: "archive",
			want: nil,
		},
		{
			name: "plugin name matches binary name — must skip os.Args[0]",
			args: []string{"gw", "something", "else"},
			plug: "gw", // pathological: if we didn't skip [0], this would match wrongly
			want: nil,
		},
		{
			name: "plugin not present",
			args: []string{"gw", "other"},
			plug: "archive",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Args = tt.args
			got := pluginArgs(tt.plug)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (got %v)", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
