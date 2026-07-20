package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nicksenap/grove/internal/models"
)

// operationRecordVersion is the schema version for durable recovery records.
// Bump it only with an explicit migration; unknown newer versions are surfaced
// by doctor but never mutated.
const operationRecordVersion = 1

// OperationKind identifies which mutating operation a recovery record describes.
type OperationKind string

const (
	OpCreate OperationKind = "create"
	OpAdd    OperationKind = "add"
	OpRemove OperationKind = "remove"
	OpDelete OperationKind = "delete"
	OpSync   OperationKind = "sync"
	OpRename OperationKind = "rename"
)

// KnownOperationKind reports whether k is a recognized recovery-record kind.
func KnownOperationKind(k OperationKind) bool {
	switch k {
	case OpCreate, OpAdd, OpRemove, OpDelete, OpSync, OpRename:
		return true
	}
	return false
}

// RepoStatus is the status of a single repository within an operation.
type RepoStatus string

const (
	RepoPending     RepoStatus = "pending"
	RepoInProgress  RepoStatus = "in_progress"
	RepoDone        RepoStatus = "done"
	RepoFailed      RepoStatus = "failed"
	RepoCompensated RepoStatus = "compensated"
	RepoSkipped     RepoStatus = "skipped"
)

// ResourceOwnership records whether a Git resource (branch or worktree) was
// created by this operation. It is deliberately three-valued so that repair can
// stay conservative: an empty (unknown) value must never be compensated
// destructively.
type ResourceOwnership string

const (
	// OwnUnknown means ownership was not determined (e.g. the operation was
	// interrupted before it recorded whether it created the resource). Repair
	// must not destroy an unknown-ownership resource.
	OwnUnknown ResourceOwnership = ""
	// OwnPreexisting means the resource existed before this operation; it must
	// never be compensated away.
	OwnPreexisting ResourceOwnership = "preexisting"
	// OwnCreated means this operation created the resource; it is safe to
	// compensate.
	OwnCreated ResourceOwnership = "created"
)

// CommitStatus records the state of the single authoritative state commit. It
// lets repair distinguish "never committed" from "commit issued but the result
// is ambiguous" (e.g. a directory-sync error after a successful rename), so a
// repair reconciles authoritative state rather than blindly compensating.
type CommitStatus string

const (
	// CommitPending means the state commit has not been attempted yet.
	CommitPending CommitStatus = ""
	// CommitAttempted means the commit was issued but its durability is
	// uncertain; repair must reconcile against authoritative state.
	CommitAttempted CommitStatus = "attempted"
	// CommitDone means the commit is confirmed durable.
	CommitDone CommitStatus = "committed"
)

// ProvisionMode records how a repository worktree was (or must be) provisioned
// so a constructive retry reproduces the original semantics.
type ProvisionMode string

const (
	// ProvisionFromBase creates a new branch from the resolved base branch.
	ProvisionFromBase ProvisionMode = ""
	// ProvisionTrack checks out an existing remote branch via a tracking
	// worktree (e.g. a PR head).
	ProvisionTrack ProvisionMode = "track"
)

// RepoOperation captures per-repository progress, resource ownership, and the
// last coded error so repair can compensate exactly what this operation created
// and retain actionable detail for mixed outcomes.
type RepoOperation struct {
	RepoName     string `json:"repo_name"`
	SourceRepo   string `json:"source_repo,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
	Branch       string `json:"branch,omitempty"`
	// BaseBranch is the resolved base branch for this repo (may differ per repo
	// via .grove.toml). Mode records how the worktree must be provisioned.
	BaseBranch string        `json:"base_branch,omitempty"`
	Mode       ProvisionMode `json:"mode,omitempty"`
	Phase      string        `json:"phase,omitempty"`
	Status     RepoStatus    `json:"status"`

	// Ownership of each resource this operation may compensate.
	BranchOwnership   ResourceOwnership `json:"branch_ownership,omitempty"`
	WorktreeOwnership ResourceOwnership `json:"worktree_ownership,omitempty"`

	// Per-repository coded error for mixed-outcome operations.
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
}

// OperationRecord is a durable, operation-specific recovery journal entry. It is
// written before the first workspace Git/filesystem mutation and removed only
// after the state commit or complete compensation succeeds.
type OperationRecord struct {
	Version   int           `json:"version"`
	ID        string        `json:"id"`
	Kind      OperationKind `json:"kind"`
	Workspace string        `json:"workspace"`

	// Repair-critical target identity captured up front.
	Path       string `json:"path,omitempty"`        // workspace root path
	BaseBranch string `json:"base_branch,omitempty"` // resolved base branch
	// RootOwnership records whether this operation created the workspace root
	// directory, so repair can distinguish an operation-created root (safe to
	// remove on compensation) from a pre-existing one.
	RootOwnership ResourceOwnership `json:"root_ownership,omitempty"`
	// Source preserves create provenance so a repaired/completed create can
	// reconstruct the intended workspace exactly.
	Source *models.WorkspaceSource `json:"source,omitempty"`
	// Force records the destructive authorization granted to this operation.
	// Repair must never widen force beyond what was recorded here.
	Force bool `json:"force,omitempty"`

	// Rename identity (only meaningful for OpRename). Typed rather than stashed
	// in Details because both sides are required to complete or revert a rename.
	RenameFrom     string `json:"rename_from,omitempty"`
	RenameTo       string `json:"rename_to,omitempty"`
	RenameFromPath string `json:"rename_from_path,omitempty"`
	RenameToPath   string `json:"rename_to_path,omitempty"`

	// Phase is the high-level operation phase (operation-specific vocabulary).
	Phase        string       `json:"phase"`
	CommitStatus CommitStatus `json:"commit_status,omitempty"`
	CreatedAt    string       `json:"created_at"`
	UpdatedAt    string       `json:"updated_at"`
	PID          int          `json:"pid"`

	Repos []RepoOperation `json:"repos,omitempty"`

	// LastError is the latest retryable error message, if any.
	LastError string `json:"last_error,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`

	// Details carries auxiliary operation-specific fields not modeled above.
	// Repair-critical fields must use typed columns, never this map.
	Details map[string]string `json:"details,omitempty"`
}

// Supported reports whether this record can be safely repaired by the current
// binary. Only the exact current schema version with a known non-empty kind is
// supported; unversioned, newer, or malformed records are surfaced by doctor
// but must never be mutated.
func (r *OperationRecord) Supported() bool {
	return r.Version == operationRecordVersion && KnownOperationKind(r.Kind)
}

// OperationStore persists recovery records under ~/.grove/operations/.
type OperationStore struct {
	Dir string

	// Test-only durability fault seams; nil in production.
	failWrite        func() error
	failSync         func() error
	failRename       func() error
	failDirSync      func() error
	failDirEntrySync func() error
}

// NewOperationStore creates an OperationStore for the given grove dir.
func NewOperationStore(groveDir string) *OperationStore {
	return &OperationStore{Dir: filepath.Join(groveDir, "operations")}
}

// opSeq is a process-local monotonic counter that guarantees ids created within
// the same clock tick still sort in creation order.
var opSeq uint64

// NewOperationID returns a strictly time-ordered, unique operation id. The
// leading nanosecond timestamp plus a monotonic sequence guarantees lexical
// ordering matches creation order even within the same clock tick.
func NewOperationID(kind OperationKind, workspace string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	ts := time.Now().UTC().Format("20060102T150405.000000000")
	seq := atomic.AddUint64(&opSeq, 1)
	safe := sanitizeIDPart(workspace)
	return fmt.Sprintf("%s-%012d-%s-%s-%s", ts, seq, kind, safe, hex.EncodeToString(b[:]))
}

// sanitizeIDPart makes a string safe to embed in a filename component.
func sanitizeIDPart(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_':
			return r
		default:
			return '-'
		}
	}, s)
}

// validID guards against path escape via a crafted id.
func validID(id string) bool {
	if id == "" || strings.ContainsAny(id, "/\\") || strings.Contains(id, "..") {
		return false
	}
	return id == filepath.Base(id)
}

func (o *OperationStore) path(id string) string {
	return filepath.Join(o.Dir, id+".json")
}

// Write durably persists rec, stamping version and timestamps. It returns the
// record id.
func (o *OperationStore) Write(rec *OperationRecord) error {
	if !KnownOperationKind(rec.Kind) {
		return fmt.Errorf("unknown or empty operation kind %q", rec.Kind)
	}
	if rec.Version == 0 {
		rec.Version = operationRecordVersion
	} else if rec.Version != operationRecordVersion {
		return fmt.Errorf("refusing to write unsupported operation record version %d", rec.Version)
	}
	if rec.ID == "" {
		rec.ID = NewOperationID(rec.Kind, rec.Workspace)
	}
	if !validID(rec.ID) {
		return fmt.Errorf("invalid operation id %q", rec.ID)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if rec.CreatedAt == "" {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	if rec.PID == 0 {
		rec.PID = os.Getpid()
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	if err := o.mkdirAllSync(o.Dir); err != nil {
		return err
	}
	return writeFileDurableWith(o.path(rec.ID), data, 0o644, durableSeams{
		failWrite:   o.failWrite,
		failSync:    o.failSync,
		failRename:  o.failRename,
		failDirSync: o.failDirSync,
	})
}

// Read loads a single record by id.
func (o *OperationStore) Read(id string) (*OperationRecord, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid operation id %q", id)
	}
	data, err := os.ReadFile(o.path(id))
	if err != nil {
		return nil, err
	}
	var rec OperationRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("corrupt operation record %s: %w", id, err)
	}
	return &rec, nil
}

// List returns all recovery records sorted by id (time-ordered). Corrupt files
// are reported as errors alongside the records that did parse.
func (o *OperationStore) List() ([]OperationRecord, error) {
	entries, err := os.ReadDir(o.Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var recs []OperationRecord
	var firstErr error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") {
			continue // temp file in flight
		}
		id := strings.TrimSuffix(e.Name(), ".json")
		rec, err := o.Read(id)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		// Guard against a record whose embedded ID disagrees with its filename;
		// repair keys off the ID, so a mismatch is treated as corrupt.
		if rec.ID != id {
			if firstErr == nil {
				firstErr = fmt.Errorf("operation record %s has mismatched id %q", id, rec.ID)
			}
			continue
		}
		recs = append(recs, *rec)
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].ID < recs[j].ID })
	return recs, firstErr
}

// Delete removes a record by id and syncs the journal directory so the removal
// is durable. It is a no-op if the record (or the journal directory) is absent.
func (o *OperationStore) Delete(id string) error {
	if !validID(id) {
		return fmt.Errorf("invalid operation id %q", id)
	}
	err := os.Remove(o.path(id))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if _, statErr := os.Stat(o.Dir); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil // nothing to sync
		}
		return statErr
	}
	return o.syncDir(o.Dir)
}

// mkdirAllSync ensures dir exists and fsyncs its parent so the directory entry
// is durable. It always syncs the parent — even when dir already exists — so a
// retry after an earlier parent-sync failure still makes the entry durable.
func (o *OperationStore) mkdirAllSync(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return o.syncDir(filepath.Dir(dir))
}

// syncDir fsyncs a directory so entry creation/removal is durable, honoring the
// test-only directory-sync fault seam.
func (o *OperationStore) syncDir(dir string) error {
	if o.failDirEntrySync != nil {
		if err := o.failDirEntrySync(); err != nil {
			return err
		}
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}
