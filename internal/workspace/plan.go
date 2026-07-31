package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nicksenap/grove/internal/gitops"
	"github.com/nicksenap/grove/internal/machine"
	"github.com/nicksenap/grove/internal/models"
)

// Plans let an agent (or a human) review a mutation before it happens, and then
// execute exactly what was reviewed.
//
// Two properties make a plan worth more than a printed warning:
//
//  1. It is produced by the same validation path as execution, so "the plan
//     succeeded" means the same thing as "validation passed".
//  2. It carries a fingerprint of the state it depends on. Apply recomputes that
//     fingerprint and refuses with STATE_CHANGED if anything relevant moved, so a
//     reviewed plan can never quietly execute against a different world.
//
// The transactional execution and rollback semantics underneath belong with the
// transactional-operations epic; this file owns the reviewable contract.

// PlanSchemaVersion versions the plan document itself, independently of the CLI
// response envelope. Apply refuses a version it does not understand rather than
// misreading fields.
const PlanSchemaVersion = 1

// PlanKind identifies which operation a plan describes.
type PlanKind string

const (
	PlanKindCreate PlanKind = "create"
	PlanKindDelete PlanKind = "delete"
)

// Planned action verbs. These are stable identifiers an agent can branch on to
// decide whether a plan needs human review.
const (
	ActionCreateWorkspaceDir = "create_workspace_dir"
	ActionCreateBranch       = "create_branch"
	ActionTrackBranch        = "track_remote_branch"
	ActionCreateWorktree     = "create_worktree"
	ActionRunSetupHook       = "run_setup_hook"
	ActionRunTeardownHook    = "run_teardown_hook"
	ActionRemoveWorktree     = "remove_worktree"
	ActionDeleteBranch       = "delete_branch"
	ActionRemoveWorkspaceDir = "remove_workspace_dir"
	ActionRemoveStateEntry   = "remove_state_entry"
)

// PlannedChange is one concrete mutation. Every repository, path, and branch a
// plan would touch appears here — a destructive plan that summarized itself as
// "delete workspace x" would not be reviewable.
type PlannedChange struct {
	Action      string `json:"action"`
	Repo        string `json:"repo,omitempty"`
	Path        string `json:"path,omitempty"`
	Branch      string `json:"branch,omitempty"`
	SourceRepo  string `json:"source_repo,omitempty"`
	Destructive bool   `json:"destructive"`
	Detail      string `json:"detail,omitempty"`
}

// Plan is a reviewable description of a mutation.
type Plan struct {
	SchemaVersion int       `json:"schema_version"`
	Kind          PlanKind  `json:"kind"`
	Workspace     string    `json:"workspace"`
	Branch        string    `json:"branch,omitempty"`
	Path          string    `json:"path"`
	CreatedAt     time.Time `json:"created_at"`
	GroveVersion  string    `json:"grove_version,omitempty"`

	// Destructive is true when any change destroys work: removing worktrees,
	// deleting branches, or deleting directories.
	Destructive bool            `json:"destructive"`
	Changes     []PlannedChange `json:"changes"`

	// Source is carried so applying a plan preserves provenance.
	Source *models.WorkspaceSource `json:"source,omitempty"`

	// Fingerprint pins the state this plan was computed against.
	Fingerprint string `json:"fingerprint"`

	// Warnings are conditions that do not block planning but deserve attention
	// before applying — e.g. a repo with uncommitted changes in a delete plan.
	Warnings []string `json:"warnings,omitempty"`
}

// DestructiveChanges returns only the changes that destroy something.
func (p *Plan) DestructiveChanges() []PlannedChange {
	var out []PlannedChange
	for _, c := range p.Changes {
		if c.Destructive {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Planning
// ---------------------------------------------------------------------------

// PlanCreate validates a create request and describes what it would do, without
// touching anything. It runs validateCreate — the same check CreateWithOpts runs
// — so a plan cannot succeed where execution would fail validation.
func (s *Service) PlanCreate(name string, opts CreateOpts, version string) (*Plan, error) {
	if err := s.validateCreate(name, opts); err != nil {
		return nil, err
	}

	wsPath := filepath.Join(opts.Cfg.WorkspaceDir, name)
	plan := &Plan{
		SchemaVersion: PlanSchemaVersion,
		Kind:          PlanKindCreate,
		Workspace:     name,
		Branch:        opts.Branch,
		Path:          wsPath,
		CreatedAt:     time.Now().UTC(),
		GroveVersion:  version,
		Source:        opts.Source,
	}

	plan.Changes = append(plan.Changes, PlannedChange{
		Action: ActionCreateWorkspaceDir,
		Path:   wsPath,
	})

	for _, repoName := range opts.Repos {
		changes, warnings := planRepoProvisioning(repoName, wsPath, opts)
		plan.Changes = append(plan.Changes, changes...)
		plan.Warnings = append(plan.Warnings, warnings...)
	}

	plan.Fingerprint = s.createFingerprint(name, opts)
	return plan, nil
}

// planRepoProvisioning describes what provisioning one repo would do. It mirrors
// provisionWorktreeNoFetch's branch resolution, so a plan reports the action the
// executor would actually take rather than a guess.
func planRepoProvisioning(repoName, wsPath string, opts CreateOpts) ([]PlannedChange, []string) {
	sourcePath := opts.RepoMap[repoName]
	wtPath := filepath.Join(wsPath, repoName)

	// Resolved once and threaded through: each of these queries is a git
	// subprocess, and planning should not pay for the same answer twice.
	branchExists := gitops.BranchExists(sourcePath, opts.Branch)

	changes, warnings := planBranchProvisioning(repoName, sourcePath, branchExists, opts)

	worktree := PlannedChange{
		Action:     ActionCreateWorktree,
		Repo:       repoName,
		Path:       wtPath,
		Branch:     opts.Branch,
		SourceRepo: sourcePath,
	}
	if branchExists {
		// Otherwise a reused branch is indistinguishable from a created one.
		worktree.Detail = "branch already exists locally; it will be checked out, not created"
	}

	changes = append(changes, worktree)
	return append(changes, planSetupHooks(repoName, wtPath, sourcePath)...), warnings
}

// planBranchProvisioning covers the branch half of provisioning: nothing when the
// branch already exists locally, a tracking checkout when track mode finds it on
// the remote, or a new branch from the resolved base.
func planBranchProvisioning(repoName, sourcePath string, branchExists bool, opts CreateOpts) ([]PlannedChange, []string) {
	if branchExists {
		return nil, nil
	}

	if effectiveBranchMode(repoName, opts) == BranchModeTrack {
		if gitops.RemoteBranchExists(sourcePath, opts.Branch) {
			return []PlannedChange{{
				Action:     ActionTrackBranch,
				Repo:       repoName,
				Branch:     opts.Branch,
				SourceRepo: sourcePath,
				Detail:     "tracking existing remote branch",
			}}, nil
		}
		// Track mode falls back to creating a branch, which is a surprise worth
		// surfacing before the plan is applied.
		base, warnings := resolveBaseForPlan(repoName, sourcePath)
		warnings = append(warnings, fmt.Sprintf(
			"%s: remote branch %s not found; a new branch would be created from %s instead",
			repoName, opts.Branch, base))
		return []PlannedChange{newBranchChange(repoName, sourcePath, opts.Branch, base)}, warnings
	}

	base, warnings := resolveBaseForPlan(repoName, sourcePath)
	return []PlannedChange{newBranchChange(repoName, sourcePath, opts.Branch, base)}, warnings
}

// effectiveBranchMode resolves opts.BranchMode for one repo: with TrackBranchRepo
// set, only that repo tracks and the rest get fresh branches.
func effectiveBranchMode(repoName string, opts CreateOpts) BranchMode {
	if opts.TrackBranchRepo != "" && repoName != opts.TrackBranchRepo {
		return BranchModeCreate
	}
	return opts.BranchMode
}

// resolveBaseForPlan reports the base branch a new branch would start from,
// warning when it had to fall back to HEAD.
func resolveBaseForPlan(repoName, sourcePath string) (string, []string) {
	base, err := gitops.ResolveBaseBranch(sourcePath)
	if err != nil {
		return "HEAD", []string{fmt.Sprintf(
			"%s: could not resolve a base branch; the new branch would start from HEAD", repoName)}
	}
	return base, nil
}

func newBranchChange(repoName, sourcePath, branch, base string) PlannedChange {
	return PlannedChange{
		Action:     ActionCreateBranch,
		Repo:       repoName,
		Branch:     branch,
		SourceRepo: sourcePath,
		Detail:     "from " + base,
	}
}

// planSetupHooks lists the .grove.toml setup commands provisioning would run.
func planSetupHooks(repoName, wtPath, sourcePath string) []PlannedChange {
	cfg, _ := gitops.ReadGroveConfig(sourcePath)
	if cfg == nil {
		return nil
	}
	changes := make([]PlannedChange, 0, len(cfg.Setup))
	for _, cmdStr := range cfg.Setup {
		changes = append(changes, PlannedChange{
			Action: ActionRunSetupHook,
			Repo:   repoName,
			Path:   wtPath,
			Detail: cmdStr,
		})
	}
	return changes
}

// PlanDelete describes everything deleting a workspace would destroy.
func (s *Service) PlanDelete(name, version string) (*Plan, error) {
	ws, err := s.State.GetWorkspace(name)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		return nil, ErrWorkspaceNotFound(name)
	}

	plan := &Plan{
		SchemaVersion: PlanSchemaVersion,
		Kind:          PlanKindDelete,
		Workspace:     ws.Name,
		Branch:        ws.Branch,
		Path:          ws.Path,
		CreatedAt:     time.Now().UTC(),
		GroveVersion:  version,
		Destructive:   true,
		Source:        ws.Source,
	}

	for _, r := range ws.Repos {
		changes, warnings := planRepoDestruction(r)
		plan.Changes = append(plan.Changes, changes...)
		plan.Warnings = append(plan.Warnings, warnings...)
	}

	plan.Changes = append(plan.Changes,
		PlannedChange{Action: ActionRemoveWorkspaceDir, Path: ws.Path, Destructive: true},
		PlannedChange{Action: ActionRemoveStateEntry, Detail: ws.Name, Destructive: true},
	)

	plan.Fingerprint = s.deleteFingerprint(ws)
	return plan, nil
}

// planRepoDestruction describes what deleting one repo's worktree would do, and
// warns about the work that would be lost with it.
func planRepoDestruction(r models.RepoWorktree) ([]PlannedChange, []string) {
	var changes []PlannedChange

	if cfg, _ := gitops.ReadGroveConfig(r.SourceRepo); cfg != nil && cfg.Teardown != "" {
		changes = append(changes, PlannedChange{
			Action: ActionRunTeardownHook, Repo: r.RepoName, Path: r.WorktreePath, Detail: cfg.Teardown,
		})
	}

	changes = append(changes,
		PlannedChange{
			Action: ActionRemoveWorktree, Repo: r.RepoName, Path: r.WorktreePath,
			Branch: r.Branch, SourceRepo: r.SourceRepo, Destructive: true,
		},
		PlannedChange{
			Action: ActionDeleteBranch, Repo: r.RepoName, Branch: r.Branch,
			SourceRepo: r.SourceRepo, Destructive: true,
			Detail: "force-deleted, including unmerged commits",
		})

	return changes, unsavedWorkWarnings(r)
}

// unsavedWorkWarnings reports work a delete would destroy: uncommitted changes,
// and commits that exist nowhere but this worktree. This is what a reviewer most
// needs to see, so every branch of it errs toward warning.
func unsavedWorkWarnings(r models.RepoWorktree) []string {
	var warnings []string

	status, err := gitops.RepoStatus(r.WorktreePath)
	switch {
	case err != nil:
		// An unreadable worktree is not evidence of a clean one. Saying nothing
		// here would be a plan claiming there is nothing to lose.
		warnings = append(warnings,
			fmt.Sprintf("%s: could not check for uncommitted changes (%s) — assume there may be work to lose", r.RepoName, err))
	case status != "":
		warnings = append(warnings,
			fmt.Sprintf("%s has uncommitted changes that would be destroyed", r.RepoName))
	}

	return append(warnings, unpushedCommitWarnings(r)...)
}

// unpushedCommitWarnings reports commits that deleting the branch would discard.
//
// The remote-tracking branch is the right comparison only when it exists. A branch
// that was never pushed has no origin/<branch> at all — which is the *most*
// dangerous case, since its commits exist nowhere else — so falling back to the
// base branch is what makes that case visible instead of silent.
func unpushedCommitWarnings(r models.RepoWorktree) []string {
	if gitops.RemoteBranchExists(r.SourceRepo, r.Branch) {
		ahead, _, err := gitops.CommitsAheadBehind(r.WorktreePath, "origin/"+r.Branch)
		if err != nil {
			return []string{fmt.Sprintf(
				"%s: could not compare %s against origin (%s) — assume there may be unpushed commits",
				r.RepoName, r.Branch, err)}
		}
		if ahead > 0 {
			return []string{fmt.Sprintf("%s has %d unpushed commit(s) on %s", r.RepoName, ahead, r.Branch)}
		}
		return nil
	}

	base, err := gitops.ResolveBaseBranch(r.SourceRepo)
	if err != nil {
		return []string{fmt.Sprintf(
			"%s: %s was never pushed and has no comparable base branch — its commits may exist only here",
			r.RepoName, r.Branch)}
	}

	ahead, _, err := gitops.CommitsAheadBehind(r.WorktreePath, base)
	if err != nil || ahead == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"%s: %s was never pushed — %d commit(s) exist only in this worktree and would be lost",
		r.RepoName, r.Branch, ahead)}
}

// validateCreate holds the pre-flight checks shared by planning and execution.
// Keeping them in one place is what makes a plan trustworthy: there is no
// validation an apply performs that a plan did not.
func (s *Service) validateCreate(name string, opts CreateOpts) error {
	if opts.Cfg == nil {
		return machine.Errorf(machine.CodeNotInitialized, "grove is not configured").
			WithActions(machine.NextAction("Initialize Grove", "gw init <repo-dir>"))
	}
	if name == "" {
		return machine.Errorf(machine.CodeUsage, "workspace name is required")
	}
	if opts.Branch == "" {
		return machine.Errorf(machine.CodeUsage, "branch is required")
	}
	if len(opts.Repos) == 0 {
		return machine.Errorf(machine.CodeUsage, "at least one repo is required").
			WithActions(machine.NextAction("List discovered repos", "gw repos --format json"))
	}

	existing, err := s.State.GetWorkspace(name)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrWorkspaceExists(name)
	}

	for _, repoName := range opts.Repos {
		sourcePath, ok := opts.RepoMap[repoName]
		if !ok {
			return ErrRepoNotFound(repoName)
		}
		if hasWT, _ := gitops.WorktreeHasBranch(sourcePath, opts.Branch); hasWT {
			return ErrWorktreeExists(opts.Branch, repoName)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Fingerprints
// ---------------------------------------------------------------------------

// createFingerprint pins what a create plan assumes: the target name is free and
// each repo's source path and branch situation are unchanged.
func (s *Service) createFingerprint(name string, opts CreateOpts) string {
	input := []any{"create", name, opts.Branch}

	repos := make([]string, len(opts.Repos))
	copy(repos, opts.Repos)
	sort.Strings(repos)
	for _, repoName := range repos {
		sourcePath := opts.RepoMap[repoName]
		input = append(input, []any{
			repoName,
			sourcePath,
			gitops.BranchExists(sourcePath, opts.Branch),
			gitops.RemoteBranchExists(sourcePath, opts.Branch),
		})
	}
	return hashOf(input)
}

// deleteFingerprint pins what a delete plan assumes. It includes each repo's
// dirty state on purpose: if work appears after the plan was reviewed, applying
// would destroy something nobody agreed to lose, so the plan must expire.
func (s *Service) deleteFingerprint(ws *models.Workspace) string {
	input := []any{"delete", ws.Name, ws.Path}

	repos := make([]models.RepoWorktree, len(ws.Repos))
	copy(repos, ws.Repos)
	sort.Slice(repos, func(i, j int) bool { return repos[i].RepoName < repos[j].RepoName })

	for _, r := range repos {
		status, _ := gitops.RepoStatus(r.WorktreePath)
		input = append(input, []any{r.RepoName, r.WorktreePath, r.Branch, r.SourceRepo, status != ""})
	}
	return hashOf(input)
}

func hashOf(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// Applying
// ---------------------------------------------------------------------------

// ApplyResult reports what an applied plan did. Exactly one of Created or Deleted
// is set, matching the plan's kind.
type ApplyResult struct {
	Kind    PlanKind      `json:"kind"`
	Created *CreateResult `json:"created,omitempty"`
	Deleted *DeleteResult `json:"deleted,omitempty"`
}

// Apply executes a previously produced plan, or fails without touching anything.
//
// It re-plans from current state and compares fingerprints, so a plan that was
// reviewed against a different world is refused with STATE_CHANGED rather than
// applied approximately. Repo source paths come from the plan itself, so what
// runs is what was reviewed even if repo discovery would now resolve differently.
func (s *Service) Apply(plan *Plan, version string) (*ApplyResult, error) {
	if plan == nil {
		return nil, machine.Errorf(machine.CodeUsage, "no plan provided")
	}
	if plan.SchemaVersion != PlanSchemaVersion {
		return nil, machine.Errorf(machine.CodeUsage,
			"unsupported plan schema_version %d (this Grove understands %d)",
			plan.SchemaVersion, PlanSchemaVersion).
			WithFix("Regenerate the plan with this version of gw")
	}

	switch plan.Kind {
	case PlanKindCreate:
		return s.applyCreate(plan, version)
	case PlanKindDelete:
		return s.applyDelete(plan, version)
	default:
		return nil, machine.Errorf(machine.CodeUsage, "unknown plan kind %q", plan.Kind)
	}
}

func (s *Service) applyCreate(plan *Plan, version string) (*ApplyResult, error) {
	opts := createOptsFromPlan(plan)

	current, err := s.PlanCreate(plan.Workspace, opts, version)
	if err != nil {
		return nil, err
	}
	if err := assertUnchanged(plan, current); err != nil {
		return nil, err
	}

	result, err := s.CreateWithOpts(plan.Workspace, opts)
	if err != nil {
		return nil, err
	}
	return &ApplyResult{Kind: PlanKindCreate, Created: result}, nil
}

func (s *Service) applyDelete(plan *Plan, version string) (*ApplyResult, error) {
	current, err := s.PlanDelete(plan.Workspace, version)
	if err != nil {
		return nil, err
	}
	if err := assertUnchanged(plan, current); err != nil {
		return nil, err
	}

	result, err := s.Delete(plan.Workspace)
	if err != nil {
		return nil, err
	}
	return &ApplyResult{Kind: PlanKindDelete, Deleted: result}, nil
}

// createOptsFromPlan reconstructs the create request from the plan document, so
// apply executes the reviewed repos and source paths rather than re-discovering
// them.
func createOptsFromPlan(plan *Plan) CreateOpts {
	repoMap := map[string]string{}
	var repos []string
	for _, c := range plan.Changes {
		if c.Action != ActionCreateWorktree || c.Repo == "" {
			continue
		}
		if _, seen := repoMap[c.Repo]; !seen {
			repos = append(repos, c.Repo)
		}
		repoMap[c.Repo] = c.SourceRepo
	}

	return CreateOpts{
		Branch:  plan.Branch,
		Repos:   repos,
		RepoMap: repoMap,
		Source:  plan.Source,
		// The workspace directory comes from the plan's own path, so a config
		// change between plan and apply cannot silently relocate the workspace.
		Cfg: &models.Config{WorkspaceDir: filepath.Dir(plan.Path)},
	}
}

// assertUnchanged compares a saved plan against a freshly computed one.
func assertUnchanged(saved, current *Plan) error {
	if saved.Fingerprint == "" {
		return machine.Errorf(machine.CodeUsage, "plan has no fingerprint, so it cannot be verified").
			WithFix("Regenerate the plan with gw plan")
	}
	if saved.Fingerprint == current.Fingerprint {
		return nil
	}
	return machine.Errorf(machine.CodeStateChanged,
		"state changed since the plan was created, so it was not applied").
		WithDetails(map[string]any{
			"plan_fingerprint":    saved.Fingerprint,
			"current_fingerprint": current.Fingerprint,
			"plan_created_at":     saved.CreatedAt,
			"current_warnings":    current.Warnings,
		}).
		WithFix("Re-plan and review the new plan before applying").
		WithActions(machine.NextAction("Regenerate the plan",
			fmt.Sprintf("gw plan %s %s --format json", saved.Kind, saved.Workspace)))
}

// LoadPlan reads a plan from a file, or from stdin when path is "-".
//
// It accepts both a bare plan document and a full CLI response envelope, because
// the natural way to save a plan is `gw plan ... --format json > plan.json`, which
// writes the envelope.
func LoadPlan(path string, stdin io.Reader) (*Plan, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, machine.Wrap(machine.CodeUsage, err, "reading plan: %s", err)
	}
	return ParsePlan(data)
}

// ParsePlan decodes a plan from bare or envelope-wrapped JSON.
func ParsePlan(data []byte) (*Plan, error) {
	var envelope struct {
		OK     bool            `json:"ok"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err == nil {
		if envelope.Error != nil {
			return nil, machine.Errorf(machine.CodeUsage,
				"refusing to apply a failed plan (%s: %s)", envelope.Error.Code, envelope.Error.Message)
		}
		if len(envelope.Result) > 0 {
			data = envelope.Result
		}
	}

	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, machine.Wrap(machine.CodeUsage, err, "plan is not valid JSON: %s", err)
	}
	if plan.Kind == "" {
		return nil, machine.Errorf(machine.CodeUsage, "not a plan document (no kind field)")
	}
	return &plan, nil
}
