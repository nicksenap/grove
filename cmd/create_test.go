package cmd

import (
	"reflect"
	"testing"

	"github.com/nicksenap/grove/internal/discover"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/picker"
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
	repos := []discover.RepoInfo{
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

func TestBuildCreateTabs(t *testing.T) {
	presets := map[string]models.Preset{
		"web":     {Repos: []string{"web"}},
		"backend": {Repos: []string{"api", "worker"}},
	}
	tabs, presetByChoice := buildCreateTabs(presets, []string{"worker", "removed", "api"}, []string{"api", "web", "worker"})

	want := []picker.Tab{
		{Title: "Presets", Choices: []string{"backend  (api, worker)", "web  (web)"}},
		{Title: "Latest", Choices: []string{"worker", "api"}, Multi: true},
		{Title: "All repos", Choices: []string{"(all)", "api", "web", "worker"}, Multi: true},
	}
	if !reflect.DeepEqual(tabs, want) {
		t.Errorf("tabs = %+v\nwant %+v", tabs, want)
	}
	if presetByChoice["backend  (api, worker)"] != "backend" {
		t.Errorf("presetByChoice = %v", presetByChoice)
	}
}

func TestBuildCreateTabs_OmitsEmptyTabs(t *testing.T) {
	tabs, _ := buildCreateTabs(nil, nil, []string{"api"})
	if len(tabs) != 1 || tabs[0].Title != "All repos" {
		t.Errorf("tabs = %+v, want only All repos", tabs)
	}
}
