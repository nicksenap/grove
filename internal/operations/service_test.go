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

func TestCreatePreparedUsesSamePostCreateFailurePolicy(t *testing.T) {
	var order []string
	ws := models.Workspace{Name: "recipe", Path: "/workspaces/recipe", Branch: "feat/recipe"}
	deps := &fakeWorkspace{workspace: ws, order: &order}
	hookErr := &lifecycle.HookError{Hook: "post_create", Err: errors.New("failed"), Abort: true}
	svc := Service{Workspace: deps, Store: deps, RunHook: func(string, lifecycle.Vars) error { return hookErr }}

	result, err := svc.Create(CreateRequest{
		Name: ws.Name,
		Preparation: &Preparation{
			Options: workspace.PreparationOpts{CreateOpts: workspace.CreateOpts{Branch: ws.Branch}},
			Run:     func(models.Workspace) error { order = append(order, "prepare"); return nil },
		},
	})

	var operationHookErr *HookError
	if !errors.As(err, &operationHookErr) || operationHookErr.Hook != "post_create" || !errors.Is(err, hookErr) {
		t.Fatalf("error = %v, want post_create hook error", err)
	}
	if !result.Created || result.Workspace.Name != ws.Name {
		t.Fatalf("completed workspace missing from result: %+v", result)
	}
	assertOrder(t, order, "create-prepared", "prepare")
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
}

func (f *fakeWorkspace) CreateWithOpts(string, workspace.CreateOpts) error {
	*f.order = append(*f.order, "create")
	return nil
}

func (f *fakeWorkspace) CreateWithPreparation(_ string, _ workspace.PreparationOpts, prepare func(models.Workspace) error) error {
	*f.order = append(*f.order, "create-prepared")
	return prepare(f.workspace)
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
