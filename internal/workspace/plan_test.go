package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/models"
)

func actionsOf(plan *Plan) []string {
	out := make([]string, len(plan.Changes))
	for i, c := range plan.Changes {
		out[i] = c.Action
	}
	return out
}

func hasAction(plan *Plan, action string) bool {
	for _, c := range plan.Changes {
		if c.Action == action {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Planning
// ---------------------------------------------------------------------------

func TestPlanCreateDescribesChangesWithoutMutating(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	plan, err := env.svc.PlanCreate("planned", CreateOpts{
		Branch:  "feat/planned",
		Repos:   []string{"api"},
		RepoMap: env.repoMap,
		Cfg:     env.cfg,
	}, "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if plan.Kind != PlanKindCreate || plan.Workspace != "planned" {
		t.Errorf("plan identity = %s/%s", plan.Kind, plan.Workspace)
	}
	if plan.Destructive {
		t.Error("creating a workspace destroys nothing")
	}
	if plan.Fingerprint == "" {
		t.Error("a plan without a fingerprint cannot be verified")
	}
	for _, want := range []string{ActionCreateWorkspaceDir, ActionCreateBranch, ActionCreateWorktree} {
		if !hasAction(plan, want) {
			t.Errorf("plan missing %s: %v", want, actionsOf(plan))
		}
	}

	// Nothing may exist yet — a plan is a preview.
	if _, err := os.Stat(filepath.Join(env.wsDir, "planned")); !os.IsNotExist(err) {
		t.Error("planning must not create the workspace directory")
	}
	if ws, _ := env.svc.State.GetWorkspace("planned"); ws != nil {
		t.Error("planning must not write state")
	}
}

// A destructive plan has to enumerate every path and branch it would destroy;
// "delete workspace x" is not reviewable.
func TestPlanDeleteEnumeratesDestruction(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("doomed", "feat/doomed", []string{"api", "web"}, env.repoMap, env.cfg)

	plan, err := env.svc.PlanDelete("doomed", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !plan.Destructive {
		t.Error("a delete plan must be marked destructive")
	}

	worktrees, branches := 0, 0
	for _, c := range plan.Changes {
		switch c.Action {
		case ActionRemoveWorktree:
			worktrees++
			if c.Path == "" || !c.Destructive {
				t.Errorf("worktree removal must name a destructive path: %+v", c)
			}
		case ActionDeleteBranch:
			branches++
			if c.Branch == "" {
				t.Errorf("branch deletion must name the branch: %+v", c)
			}
		}
	}
	if worktrees != 2 || branches != 2 {
		t.Errorf("expected 2 worktree + 2 branch deletions, got %d + %d", worktrees, branches)
	}
	if !hasAction(plan, ActionRemoveStateEntry) {
		t.Error("plan should include removing the state entry")
	}

	// Still there — planning is read-only.
	if ws, _ := env.svc.State.GetWorkspace("doomed"); ws == nil {
		t.Error("planning must not delete anything")
	}
}

// The warning is the whole reason to review a delete.
func TestPlanDeleteWarnsAboutUncommittedWork(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("risky", "feat/risky", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "risky", "api")
	os.WriteFile(filepath.Join(wt, "wip.txt"), []byte("unsaved"), 0o644)

	plan, err := env.svc.PlanDelete("risky", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	found := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "uncommitted") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an uncommitted-changes warning, got %v", plan.Warnings)
	}
}

// Planning must not succeed where execution would fail validation, or a plan is
// worthless as a check.
func TestPlanCreateSharesValidationWithExecution(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("taken", "feat/taken", []string{"api"}, env.repoMap, env.cfg)

	cases := []struct {
		name string
		opts CreateOpts
		code machine.Code
	}{
		{"duplicate name", CreateOpts{Branch: "feat/x", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg}, machine.CodeWorkspaceExists},
		{"unknown repo", CreateOpts{Branch: "feat/y", Repos: []string{"ghost"}, RepoMap: env.repoMap, Cfg: env.cfg}, machine.CodeRepoNotFound},
		{"no branch", CreateOpts{Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg}, machine.CodeUsage},
		{"no repos", CreateOpts{Branch: "feat/z", RepoMap: env.repoMap, Cfg: env.cfg}, machine.CodeUsage},
		{"branch already worktreed", CreateOpts{Branch: "feat/taken", Repos: []string{"api"}, RepoMap: env.repoMap, Cfg: env.cfg}, machine.CodeWorktreeExists},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wsName := "taken"
			if tc.code != machine.CodeWorkspaceExists {
				wsName = "fresh-" + strings.ReplaceAll(tc.name, " ", "-")
			}

			_, planErr := env.svc.PlanCreate(wsName, tc.opts, "test")
			if machine.CodeFor(planErr) != tc.code {
				t.Fatalf("plan error = %v (%s), want %s", planErr, machine.CodeFor(planErr), tc.code)
			}
			// Execution must reject it the same way.
			_, execErr := env.svc.CreateWithOpts(wsName, tc.opts)
			if machine.CodeFor(execErr) != tc.code {
				t.Errorf("execution error = %v (%s), want %s", execErr, machine.CodeFor(execErr), tc.code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Applying
// ---------------------------------------------------------------------------

func TestApplyCreateExecutesThePlan(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")

	plan, err := env.svc.PlanCreate("applied", CreateOpts{
		Branch:  "feat/applied",
		Repos:   []string{"api", "web"},
		RepoMap: env.repoMap,
		Cfg:     env.cfg,
		Source:  &models.WorkspaceSource{Provider: "github", URL: "https://example.test/pr/1"},
	}, "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	result, err := env.svc.Apply(plan, "test")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Kind != PlanKindCreate || result.Created == nil {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Created.Repos) != 2 {
		t.Errorf("expected 2 repos created, got %d", len(result.Created.Repos))
	}

	ws, _ := env.svc.State.GetWorkspace("applied")
	if ws == nil {
		t.Fatal("workspace should exist after apply")
	}
	// Provenance recorded in the plan survives the round trip.
	if ws.Source == nil || ws.Source.Provider != "github" {
		t.Errorf("source = %+v, want the planned provenance", ws.Source)
	}
	if ws.Path != plan.Path {
		t.Errorf("applied path %s != planned path %s", ws.Path, plan.Path)
	}
}

func TestApplyDeleteExecutesThePlan(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("bye", "feat/bye", []string{"api"}, env.repoMap, env.cfg)

	plan, err := env.svc.PlanDelete("bye", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	result, err := env.svc.Apply(plan, "test")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Deleted == nil || !result.Deleted.StateRemoved {
		t.Fatalf("result = %+v", result)
	}
	if ws, _ := env.svc.State.GetWorkspace("bye"); ws != nil {
		t.Error("workspace should be gone")
	}
}

// The core safety property: work appearing after review must invalidate the plan
// rather than being destroyed by it.
func TestApplyRefusesPlanAfterStateChanged(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("guarded", "feat/guarded", []string{"api"}, env.repoMap, env.cfg)

	plan, err := env.svc.PlanDelete("guarded", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// An agent starts editing after the plan was reviewed.
	wt := filepath.Join(env.wsDir, "guarded", "api")
	os.WriteFile(filepath.Join(wt, "new-work.txt"), []byte("precious"), 0o644)

	_, err = env.svc.Apply(plan, "test")
	if machine.CodeFor(err) != machine.CodeStateChanged {
		t.Fatalf("apply error = %v (%s), want %s", err, machine.CodeFor(err), machine.CodeStateChanged)
	}
	if ws, _ := env.svc.State.GetWorkspace("guarded"); ws == nil {
		t.Error("a refused apply must not have deleted anything")
	}
	if _, err := os.Stat(filepath.Join(wt, "new-work.txt")); err != nil {
		t.Error("the uncommitted work must survive")
	}
}

func TestApplyRefusesPlanWhenRepoSetChanged(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.createRepo("web")
	env.svc.Create("shifting", "feat/shifting", []string{"api"}, env.repoMap, env.cfg)

	plan, err := env.svc.PlanDelete("shifting", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if _, err := env.svc.AddRepos("shifting", []string{"web"}, env.repoMap); err != nil {
		t.Fatalf("add-repo: %v", err)
	}

	if _, err := env.svc.Apply(plan, "test"); machine.CodeFor(err) != machine.CodeStateChanged {
		t.Fatalf("apply error = %v (%s), want %s", err, machine.CodeFor(err), machine.CodeStateChanged)
	}
}

func TestApplyRefusesUnknownSchemaVersion(t *testing.T) {
	env := setupTestEnv(t)
	plan := &Plan{SchemaVersion: PlanSchemaVersion + 1, Kind: PlanKindDelete, Workspace: "x"}

	if _, err := env.svc.Apply(plan, "test"); machine.CodeFor(err) != machine.CodeUsage {
		t.Errorf("error = %v (%s), want %s", err, machine.CodeFor(err), machine.CodeUsage)
	}
}

func TestApplyRefusesPlanWithoutFingerprint(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("nofp", "feat/nofp", []string{"api"}, env.repoMap, env.cfg)

	plan, _ := env.svc.PlanDelete("nofp", "test")
	plan.Fingerprint = ""

	if _, err := env.svc.Apply(plan, "test"); machine.CodeFor(err) != machine.CodeUsage {
		t.Errorf("error = %v (%s), want %s", err, machine.CodeFor(err), machine.CodeUsage)
	}
}

// ---------------------------------------------------------------------------
// Plan documents
// ---------------------------------------------------------------------------

// `gw plan ... --format json > plan.json` writes an envelope, so apply must
// accept that as readily as a bare plan.
func TestParsePlanAcceptsEnvelopeAndBareDocument(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("wrapped", "feat/wrapped", []string{"api"}, env.repoMap, env.cfg)
	plan, _ := env.svc.PlanDelete("wrapped", "test")

	bare, _ := json.Marshal(plan)
	fromBare, err := ParsePlan(bare)
	if err != nil {
		t.Fatalf("bare plan: %v", err)
	}
	if fromBare.Fingerprint != plan.Fingerprint {
		t.Error("bare round trip lost the fingerprint")
	}

	envelope, _ := json.Marshal(map[string]any{
		"ok": true, "schemaVersion": 1, "result": plan, "next_actions": []any{},
	})
	fromEnvelope, err := ParsePlan(envelope)
	if err != nil {
		t.Fatalf("envelope plan: %v", err)
	}
	if fromEnvelope.Fingerprint != plan.Fingerprint || fromEnvelope.Kind != plan.Kind {
		t.Errorf("envelope round trip = %+v", fromEnvelope)
	}
}

// Applying a saved *failure* envelope must be refused rather than parsed into an
// empty plan.
func TestParsePlanRejectsFailureEnvelope(t *testing.T) {
	data := []byte(`{"ok":false,"schemaVersion":1,"error":{"code":"WORKSPACE_EXISTS","message":"nope"}}`)
	if _, err := ParsePlan(data); machine.CodeFor(err) != machine.CodeUsage {
		t.Errorf("error = %v, want a USAGE refusal", err)
	}
}

func TestParsePlanRejectsNonPlanJSON(t *testing.T) {
	for _, data := range [][]byte{
		[]byte(`{"hello":"world"}`),
		[]byte(`not json at all`),
	} {
		if _, err := ParsePlan(data); err == nil {
			t.Errorf("expected a rejection for %s", data)
		}
	}
}

func TestLoadPlanFromFileAndStdin(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("loadable", "feat/loadable", []string{"api"}, env.repoMap, env.cfg)
	plan, _ := env.svc.PlanDelete("loadable", "test")
	data, _ := json.Marshal(plan)

	path := filepath.Join(env.dir, "plan.json")
	os.WriteFile(path, data, 0o644)

	fromFile, err := LoadPlan(path, nil)
	if err != nil {
		t.Fatalf("from file: %v", err)
	}
	if fromFile.Workspace != "loadable" {
		t.Errorf("workspace = %s", fromFile.Workspace)
	}

	fromStdin, err := LoadPlan("-", strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("from stdin: %v", err)
	}
	if fromStdin.Fingerprint != plan.Fingerprint {
		t.Error("stdin round trip lost the fingerprint")
	}

	if _, err := LoadPlan(filepath.Join(env.dir, "missing.json"), nil); machine.CodeFor(err) != machine.CodeUsage {
		t.Errorf("a missing plan file should be a USAGE error, got %v", err)
	}
}

// Apply rebuilds the repo set from the plan, not from current discovery, so what
// runs is what was reviewed.
func TestApplyUsesPlanRepoSourcePaths(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")

	plan, err := env.svc.PlanCreate("pinned", CreateOpts{
		Branch:  "feat/pinned",
		Repos:   []string{"api"},
		RepoMap: env.repoMap,
		Cfg:     env.cfg,
	}, "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	opts := createOptsFromPlan(plan)
	if len(opts.Repos) != 1 || opts.Repos[0] != "api" {
		t.Fatalf("repos = %v", opts.Repos)
	}
	if opts.RepoMap["api"] != env.repoMap["api"] {
		t.Errorf("source path = %q, want %q", opts.RepoMap["api"], env.repoMap["api"])
	}
	if opts.Cfg.WorkspaceDir != filepath.Dir(plan.Path) {
		t.Errorf("workspace dir = %q, want %q", opts.Cfg.WorkspaceDir, filepath.Dir(plan.Path))
	}
}

// ---------------------------------------------------------------------------
// Unsaved-work warnings
// ---------------------------------------------------------------------------

func warningsContaining(plan *Plan, substr string) []string {
	var out []string
	for _, w := range plan.Warnings {
		if strings.Contains(w, substr) {
			out = append(out, w)
		}
	}
	return out
}

// The most dangerous case: commits that were never pushed exist nowhere else, so
// a delete plan that stayed silent about them would be actively misleading.
func TestPlanDeleteWarnsAboutNeverPushedCommits(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.svc.Create("unpushed", "feat/unpushed", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "unpushed", "api")
	os.WriteFile(filepath.Join(wt, "only-here.txt"), []byte("irreplaceable"), 0o644)
	env.run(wt, "git", "add", ".")
	env.run(wt, "git", "commit", "-q", "-m", "work that exists nowhere else")

	plan, err := env.svc.PlanDelete("unpushed", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if got := warningsContaining(plan, "never pushed"); len(got) == 0 {
		t.Errorf("expected a never-pushed warning, got %v", plan.Warnings)
	}
}

// Once the branch exists on the remote, the comparison is against it.
func TestPlanDeleteWarnsAboutUnpushedCommitsOnPushedBranch(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.svc.Create("ahead", "feat/ahead", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "ahead", "api")
	env.run(wt, "git", "push", "-q", "-u", "origin", "feat/ahead")

	os.WriteFile(filepath.Join(wt, "later.txt"), []byte("after push"), 0o644)
	env.run(wt, "git", "add", ".")
	env.run(wt, "git", "commit", "-q", "-m", "committed after pushing")

	plan, err := env.svc.PlanDelete("ahead", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if got := warningsContaining(plan, "unpushed commit"); len(got) != 1 {
		t.Errorf("expected one unpushed-commit warning, got %v", plan.Warnings)
	}
}

// A fully pushed branch has nothing to lose, so the plan should stay quiet rather
// than crying wolf on every delete.
func TestPlanDeleteQuietWhenEverythingIsPushed(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepoWithRemote("api")
	env.svc.Create("clean", "feat/clean", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "clean", "api")
	env.run(wt, "git", "push", "-q", "-u", "origin", "feat/clean")

	plan, err := env.svc.PlanDelete("clean", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("expected no warnings for a fully pushed branch, got %v", plan.Warnings)
	}
}

// An unreadable worktree is not evidence of a clean one.
func TestPlanDeleteWarnsWhenStatusCannotBeChecked(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("broken", "feat/broken", []string{"api"}, env.repoMap, env.cfg)

	// Remove the worktree directory out from under Grove, so git status fails.
	os.RemoveAll(filepath.Join(env.wsDir, "broken", "api"))

	plan, err := env.svc.PlanDelete("broken", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if got := warningsContaining(plan, "could not check"); len(got) == 0 {
		t.Errorf("expected a warning about the failed check, got %v", plan.Warnings)
	}
}

// A boolean "is dirty" fingerprint would let work added to an already-dirty repo
// slip through, which is the case a coding agent actually produces: it starts from
// a workspace that already had scratch files.
func TestApplyRefusesWhenAlreadyDirtyRepoGainsMoreWork(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("dirtier", "feat/dirtier", []string{"api"}, env.repoMap, env.cfg)

	wt := filepath.Join(env.wsDir, "dirtier", "api")
	os.WriteFile(filepath.Join(wt, "scratch.txt"), []byte("pre-existing mess"), 0o644)

	plan, err := env.svc.PlanDelete("dirtier", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	// Still dirty, but now dirty with something else.
	os.WriteFile(filepath.Join(wt, "precious.txt"), []byte("added after review"), 0o644)

	if _, err := env.svc.Apply(plan, "test"); machine.CodeFor(err) != machine.CodeStateChanged {
		t.Fatalf("apply error = %v (%s), want %s", err, machine.CodeFor(err), machine.CodeStateChanged)
	}
	if _, err := os.Stat(filepath.Join(wt, "precious.txt")); err != nil {
		t.Error("the work added after review must survive")
	}
}

// Committing leaves the worktree clean, so a commit made after review must be
// caught by the commit SHA rather than by dirtiness.
func TestApplyRefusesWhenNewCommitAppearsAfterPlan(t *testing.T) {
	env := setupTestEnv(t)
	env.createRepo("api")
	env.svc.Create("committed", "feat/committed", []string{"api"}, env.repoMap, env.cfg)

	plan, err := env.svc.PlanDelete("committed", "test")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	wt := filepath.Join(env.wsDir, "committed", "api")
	os.WriteFile(filepath.Join(wt, "feature.txt"), []byte("new work"), 0o644)
	env.run(wt, "git", "add", ".")
	env.run(wt, "git", "commit", "-q", "-m", "work committed after the plan was reviewed")

	if _, err := env.svc.Apply(plan, "test"); machine.CodeFor(err) != machine.CodeStateChanged {
		t.Fatalf("apply error = %v (%s), want %s", err, machine.CodeFor(err), machine.CodeStateChanged)
	}
	if ws, _ := env.svc.State.GetWorkspace("committed"); ws == nil {
		t.Error("a refused apply must not have deleted the workspace")
	}
}
