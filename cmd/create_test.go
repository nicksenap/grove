package cmd

import (
	"testing"

	"github.com/nicksenap/grove/internal/discover"
)

func TestDeriveName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"feat/login", "feat-login"},
		{"feat/my feature", "feat-my-feature"},
		{"main", "main"},
		{"/leading", "leading"},
		{"trailing/", "trailing"},
		{"/both/", "both"},
		{"a/b/c", "a-b-c"},
		{"  spaced  ", "spaced"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := deriveName(tt.in); got != tt.want {
				t.Errorf("deriveName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRepoNamesList(t *testing.T) {
	repos := []discover.Repo{
		{Name: "api"},
		{Name: "web"},
		{Name: "worker"},
	}
	got := repoNamesList(repos)
	want := []string{"api", "web", "worker"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRepoNamesList_Empty(t *testing.T) {
	got := repoNamesList(nil)
	if got == nil {
		t.Error("expected non-nil empty slice (callers strings.Join), got nil")
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
