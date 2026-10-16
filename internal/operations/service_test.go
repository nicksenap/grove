package operations

import (
	"errors"
	"testing"

	"github.com/nicksenap/grove/internal/lifecycle"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/workspace"
)

func TestCreateRunsPostCreateAfterWorkspaceCreation(t *testing.T) {
	var order []string
	ws := models.Workspace{Name: "feature", Path: "/workspaces/feature", Branch: "feat/feature"}
	deps := &fakeWorkspace{workspace: ws, order: &order}
	svc := Service{
		Workspace: deps,
		Store:     deps,
		RunHook: func(name string, vars lifecycle.Vars) error {
			order = append(order, "hook:"+name)
			if vars.Name != ws.Name || vars.Path != ws.Path || vars.Branch != ws.Branch {
				t.Fatalf("unexpected hook vars: %+v", vars)
			}
			return nil
		},
	}

	result, err := svc.Create(CreateRequest{Name: ws.Name, Options: workspace.CreateOpts{Branch: ws.Branch}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Workspace.Name != ws.Name {
		t.Fatalf("unexpected result: %+v", result)
	}
	assertOrder(t, order, "create", "hook:post_create")
}

func TestAddReposRunsPostAddRepoAfterReposAreAdded(t *testing.T) {
	var order []string
	ws := models.Workspace{Name: "feature", Path: "/workspaces/feature", Branch: "feat/feature"}
	deps := &fakeWorkspace{workspace: ws, order: &order, added: 1}
	svc := Service{
		Workspace: deps,
		Store:     deps,
		RunHook: func(name string, vars lifecycle.Vars) error {
			order = append(order, "hook:"+name)
			if vars.Name != ws.Name || vars.Path != ws.Path || vars.Branch != ws.Branch {
				t.Fatalf("unexpected hook vars: %+v", vars)
			}
			return nil
		},
	}

	if err := svc.AddRepos(AddReposRequest{Name: ws.Name, Repos: []string{"web"}}); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, order, "add", "hook:post_add_repo")
}

func TestAddReposSkipsHookWhenNothingWasAdded(t *testing.T) {
	var order []string
	deps := &fakeWorkspace{workspace: models.Workspace{Name: "feature"}, order: &order}
	svc := Service{
		Workspace: deps,
		Store:     deps,
		RunHook: func(name string, _ lifecycle.Vars) error {
			order = append(order, "hook:"+name)
			return nil
		},
	}

	if err := svc.AddRepos(AddReposRequest{Name: "feature", Repos: []string{"web"}}); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, order, "add")
}

func TestAddReposAbortingHookIsAHookError(t *testing.T) {
	var order []string
	deps := &fakeWorkspace{workspace: models.Workspace{Name: "feature"}, order: &order, added: 1}
	svc := Service{
		Workspace: deps,
		Store:     deps,
		RunHook: func(name string, _ lifecycle.Vars) error {
			return &lifecycle.HookError{Hook: name, Err: errors.New("boom"), Abort: true}
		},
	}

	err := svc.AddRepos(AddReposRequest{Name: "feature", Repos: []string{"web"}})
	var hookErr *HookError
	if !errors.As(err, &hookErr) || hookErr.Hook != "post_add_repo" {
		t.Fatalf("expected post_add_repo HookError, got %v", err)
	}
}

func TestCreateUsesResolvedBranch(t *testing.T) {
	var order []string
	deps := &fakeWorkspace{workspace: models.Workspace{Name: "feat-x"}, order: &order}
	svc := Service{
		Workspace: deps,
		Store:     deps,
		RunHook:   func(string, lifecycle.Vars) error { return nil },
		ResolveHook: func(name string, vars lifecycle.Vars) (string, error) {
			order = append(order, "resolve:"+name)
			if vars.Branch != "feat-x" || vars.Name != "feat-x" {
				t.Fatalf("unexpected resolve vars: %+v", vars)
			}
			return "erebe/feat-x", nil
		},
	}

	if _, err := svc.Create(CreateRequest{Name: "feat-x", Options: workspace.CreateOpts{Branch: "feat-x"}}); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, order, "resolve:resolve_branch", "create")
	if deps.createOpts.Branch != "erebe/feat-x" {
		t.Fatalf("created with branch %q, want erebe/feat-x", deps.createOpts.Branch)
	}
}

func TestCreateKeepsBranchWhenResolverUnset(t *testing.T) {
	var order []string
	deps := &fakeWorkspace{workspace: models.Workspace{Name: "feat-x"}, order: &order}
	svc := Service{
		Workspace:   deps,
		Store:       deps,
		RunHook:     func(string, lifecycle.Vars) error { return nil },
		ResolveHook: func(string, lifecycle.Vars) (string, error) { return "", lifecycle.ErrNoHook },
	}

	if _, err := svc.Create(CreateRequest{Name: "feat-x", Options: workspace.CreateOpts{Branch: "feat-x"}}); err != nil {
		t.Fatal(err)
	}
	if deps.createOpts.Branch != "feat-x" {
		t.Fatalf("created with branch %q, want feat-x", deps.createOpts.Branch)
	}
}

func TestCreateTrackSkipsResolver(t *testing.T) {
	var order []string
	deps := &fakeWorkspace{workspace: models.Workspace{Name: "pr"}, order: &order}
	svc := Service{
		Workspace: deps,
		Store:     deps,
		RunHook:   func(string, lifecycle.Vars) error { return nil },
		ResolveHook: func(string, lifecycle.Vars) (string, error) {
			t.Fatal("resolver must not run with --track")
			return "", nil
		},
	}

	opts := workspace.CreateOpts{Branch: "someone/pr-head", BranchMode: workspace.BranchModeTrack}
	if _, err := svc.Create(CreateRequest{Name: "pr", Options: opts}); err != nil {
		t.Fatal(err)
	}
	if deps.createOpts.Branch != "someone/pr-head" {
		t.Fatalf("created with branch %q, want someone/pr-head", deps.createOpts.Branch)
	}
}

func TestResolveBranchThenCreateResolvesOnce(t *testing.T) {
	var order []string
	deps := &fakeWorkspace{workspace: models.Workspace{Name: "feat-x"}, order: &order}
	svc := Service{
		Workspace: deps,
		Store:     deps,
		RunHook:   func(string, lifecycle.Vars) error { return nil },
		ResolveHook: func(name string, _ lifecycle.Vars) (string, error) {
			order = append(order, "resolve")
			return "erebe/feat-x", nil
		},
	}

	req := CreateRequest{Name: "feat-x", Options: workspace.CreateOpts{Branch: "feat-x"}}
	req, err := svc.ResolveBranch(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(req); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, order, "resolve", "create")
	if deps.createOpts.Branch != "erebe/feat-x" {
		t.Fatalf("created with branch %q, want erebe/feat-x", deps.createOpts.Branch)
	}
}

func TestCreateResolverFailureAbortsBeforeCreate(t *testing.T) {
	tests := map[string]func(string, lifecycle.Vars) (string, error){
		"hook fails": func(name string, _ lifecycle.Vars) (string, error) {
			return "", &lifecycle.HookError{Hook: name, Err: errors.New("boom"), Abort: true}
		},
		"invalid branch name": func(string, lifecycle.Vars) (string, error) {
			return "bad..name", nil
		},
	}
	for name, resolve := range tests {
		t.Run(name, func(t *testing.T) {
			var order []string
			deps := &fakeWorkspace{workspace: models.Workspace{Name: "feat-x"}, order: &order}
			svc := Service{Workspace: deps, Store: deps, ResolveHook: resolve,
				RunHook: func(string, lifecycle.Vars) error { return nil }}

			result, err := svc.Create(CreateRequest{Name: "feat-x", Options: workspace.CreateOpts{Branch: "feat-x"}})
			var hookErr *HookError
			if !errors.As(err, &hookErr) || hookErr.Hook != "resolve_branch" {
				t.Fatalf("expected resolve_branch HookError, got %v", err)
			}
			if result.Created || len(order) != 0 {
				t.Fatalf("workspace must not be created: result=%+v order=%v", result, order)
			}
		})
	}
}

func TestDeleteAbortingPreDeletePreventsDeletion(t *testing.T) {
	var order []string
	ws := models.Workspace{Name: "feature", Path: "/workspaces/feature", Branch: "feat/feature"}
	deps := &fakeWorkspace{workspace: ws, order: &order}
	hookErr := &lifecycle.HookError{Hook: "pre_delete", Err: errors.New("blocked"), Abort: true}
	svc := Service{Workspace: deps, Store: deps, RunHook: func(string, lifecycle.Vars) error { order = append(order, "hook"); return hookErr }}

	result, err := svc.Delete(DeleteRequest{Name: ws.Name, Options: workspace.RemoveOptions{Force: true}})
	var operationHookErr *HookError
	if !errors.As(err, &operationHookErr) || operationHookErr.Hook != "pre_delete" || !errors.Is(err, hookErr) {
		t.Fatalf("error = %v, want pre_delete hook error", err)
	}
	if result.Deleted {
		t.Fatalf("unexpected deleted result: %+v", result)
	}
	assertOrder(t, order, "hook")
}

func TestDeleteWarningContinuesAndReportsWarning(t *testing.T) {
	var order []string
	var warnings []string
	ws := models.Workspace{Name: "feature", Path: "/workspaces/feature", Branch: "feat/feature"}
	deps := &fakeWorkspace{workspace: ws, order: &order}
	hookErr := &lifecycle.HookError{Hook: "pre_delete", Err: errors.New("warning")}
	svc := Service{
		Workspace: deps,
		Store:     deps,
		RunHook:   func(string, lifecycle.Vars) error { order = append(order, "hook"); return hookErr },
		Warn:      func(message string) { warnings = append(warnings, message) },
	}

	result, err := svc.Delete(DeleteRequest{Name: ws.Name})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Deleted || len(warnings) != 1 {
		t.Fatalf("result = %+v, warnings = %v", result, warnings)
	}
	assertOrder(t, order, "hook", "delete")
}

func TestNoHookBypassesLifecycleForCreateAndDelete(t *testing.T) {
	var order []string
	ws := models.Workspace{Name: "feature", Path: "/workspaces/feature", Branch: "feat/feature"}
	deps := &fakeWorkspace{workspace: ws, order: &order}
	svc := Service{Workspace: deps, Store: deps, RunHook: func(string, lifecycle.Vars) error { return lifecycle.ErrNoHook }}

	if _, err := svc.Create(CreateRequest{Name: ws.Name}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Delete(DeleteRequest{Name: ws.Name}); err != nil {
		t.Fatal(err)
	}
	assertOrder(t, order, "create", "delete")
}

func TestCloseRequiresConfiguredHook(t *testing.T) {
	svc := Service{RunHook: func(string, lifecycle.Vars) error { return lifecycle.ErrNoHook }}

	result, err := svc.Close(CloseRequest{})
	if !errors.Is(err, ErrHookNotConfigured) || result.Closed {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestCloseReturnsOnCloseFailure(t *testing.T) {
	hookErr := &lifecycle.HookError{Hook: "on_close", Err: errors.New("failed")}
	svc := Service{RunHook: func(name string, vars lifecycle.Vars) error {
		if name != "on_close" || vars != (lifecycle.Vars{}) {
			t.Fatalf("unexpected hook call: %s %+v", name, vars)
		}
		return hookErr
	}}

	result, err := svc.Close(CloseRequest{})
	var operationHookErr *HookError
	if !errors.As(err, &operationHookErr) || operationHookErr.Hook != "on_close" || !errors.Is(err, hookErr) || result.Closed {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestCloseReportsSuccess(t *testing.T) {
	svc := Service{RunHook: func(string, lifecycle.Vars) error { return nil }}

	result, err := svc.Close(CloseRequest{})
	if err != nil || !result.Closed {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
}

func TestCreateStateReloadErrorIsNotAHookError(t *testing.T) {
	var order []string
	deps := &fakeWorkspace{workspace: models.Workspace{Name: "feature"}, order: &order, getErr: errors.New("state unavailable")}
	svc := Service{Workspace: deps, Store: deps}

	result, err := svc.Create(CreateRequest{Name: "feature", Options: workspace.CreateOpts{
		Branch: "feat/feature", Cfg: &models.Config{WorkspaceDir: "/workspaces"},
	}})
	var hookErr *HookError
	if err == nil || errors.As(err, &hookErr) {
		t.Fatalf("error = %v, want non-hook state error", err)
	}
	if !result.Created || result.Workspace.Path != "/workspaces/feature" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestDeletePassesAuthorizedWorkspaceIdentity(t *testing.T) {
	var order []string
	ws := models.Workspace{Name: "feature", Path: "/workspaces/feature", Branch: "feat/feature", CreatedAt: "created-1"}
	deps := &fakeWorkspace{workspace: ws, order: &order}
	svc := Service{Workspace: deps, Store: deps, RunHook: func(string, lifecycle.Vars) error { return nil }}

	if _, err := svc.Delete(DeleteRequest{Name: ws.Name}); err != nil {
		t.Fatal(err)
	}
	if deps.deleteOpts.ExpectedCreatedAt != ws.CreatedAt || deps.deleteOpts.ExpectedPath != ws.Path {
		t.Fatalf("delete options = %+v", deps.deleteOpts)
	}
}

type fakeWorkspace struct {
	workspace  models.Workspace
	order      *[]string
	getErr     error
	deleteOpts workspace.RemoveOptions
	added      int
	createOpts workspace.CreateOpts
}

func (f *fakeWorkspace) AddRepos(string, []string, map[string]string) (int, error) {
	*f.order = append(*f.order, "add")
	return f.added, nil
}

func (f *fakeWorkspace) CreateWithOpts(_ string, opts workspace.CreateOpts) error {
	*f.order = append(*f.order, "create")
	f.createOpts = opts
	return nil
}

func (f *fakeWorkspace) DeleteWithOptions(_ string, opts workspace.RemoveOptions) error {
	*f.order = append(*f.order, "delete")
	f.deleteOpts = opts
	return nil
}

func (f *fakeWorkspace) GetWorkspace(string) (*models.Workspace, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	ws := f.workspace
	return &ws, nil
}

func assertOrder(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
