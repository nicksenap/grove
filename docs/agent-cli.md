# Agent CLI contract

Grove's CLI is its only first-party agent interface. There is no MCP server and
no daemon: a coding agent or CI script with shell access drives Grove through
`gw` and parses one JSON envelope per command.

This page is the contract. It is versioned, and Grove treats it as public API.

## Machine mode

Add `--format json` (short: `-o json`) to any command:

```bash
gw context --format json
gw list --format json
gw status feature-x --format json
gw create feat-x -r svc-auth,api-gateway -b feat/x --format json
gw doctor --format json
```

In machine mode Grove guarantees:

| Guarantee | Detail |
| --- | --- |
| One response | stdout carries exactly one JSON envelope, nothing else |
| Clean channels | progress, warnings, hook output, and debug logs go to stderr |
| No decoration | no colors, spinners, prompts, or version-update notices |
| No TTY needed | commands never require a terminal and never block on input |
| Structured failure | errors use the same envelope, with a stable code and a meaningful exit code |

Anything Grove would have asked interactively becomes a `USAGE` error instead of
a prompt, so a command either does the requested work or explains what argument
was missing.

`--format text` (the default) keeps the existing human output — tables, colors,
and interactive pickers — unchanged.

## Response envelope

Success:

```json
{
  "ok": true,
  "schemaVersion": 1,
  "result": { "name": "feat-x", "path": "/Users/me/.grove/workspaces/feat-x" },
  "next_actions": [
    { "description": "Inspect repo state", "command": "gw status feat-x --format json" }
  ]
}
```

Failure:

```json
{
  "ok": false,
  "schemaVersion": 1,
  "error": {
    "code": "WORKTREE_DIRTY",
    "message": "api has uncommitted changes"
  },
  "fix": "Commit, stash, or explicitly force deletion",
  "next_actions": [
    { "description": "Inspect changes", "command": "gw status api --format json" }
  ]
}
```

Fields:

| Field | Type | Notes |
| --- | --- | --- |
| `ok` | bool | Always present. `false` means the command did not complete its request. |
| `schemaVersion` | int | Envelope version. Currently `1`. |
| `result` | object | Present when `ok` is `true`. Command-specific; never `null`. |
| `error` | object | Present when `ok` is `false`. Has `code`, `message`, and optional `details`. |
| `fix` | string | Optional human-readable remedy for the error. |
| `warnings` | string[] | Optional non-fatal problems (e.g. a repo whose fetch failed). Also printed to stderr. |
| `next_actions` | array | Always present, possibly empty. Each entry has `description` and a runnable `command`. |

`ok: true` with a non-empty `warnings` array is normal and means "the request
succeeded, some non-essential part degraded". Commands that touch several repos
report per-repo outcomes inside `result`, so a partial success is always
inspectable rather than collapsed into one boolean.

## Error codes

Codes are stable identifiers. Branch on `error.code`, never on `error.message`.

| Code | Exit | Meaning |
| --- | --- | --- |
| `USAGE` | 2 | Malformed invocation: bad flag, missing argument, or input Grove cannot obtain non-interactively. |
| `WORKSPACE_NOT_FOUND` | 3 | No workspace by that name, and none inferable from the cwd. |
| `REPO_NOT_FOUND` | 3 | Named repo is not in the workspace or not discoverable. |
| `NO_WORKSPACES` | 3 | The operation needs at least one workspace to exist. |
| `WORKSPACE_EXISTS` | 4 | A workspace with that name already exists. |
| `WORKTREE_EXISTS` | 4 | The branch already has a worktree in that repo. |
| `WORKTREE_DIRTY` | 4 | Uncommitted changes block the operation. |
| `BRANCH_CONFLICT` | 4 | The requested branch state conflicts with the repo's. |
| `STATE_CHANGED` | 4 | Relevant state changed since a plan was produced (see `gw apply`). |
| `NOT_INITIALIZED` | 5 | Grove has no config yet — run `gw init <repo-dir>`. |
| `GIT_FAILED` | 5 | A git subprocess failed. |
| `HOOK_FAILED` | 5 | A lifecycle or per-repo hook failed with `on_failure = "abort"`. |
| `PERMISSION_DENIED` | 6 | Filesystem or credential permission failure. |
| `TRANSIENT` | 7 | May succeed on retry (network, lock contention). |
| `CANCELLED` | 8 | The user aborted an interactive flow. |
| `INTERNAL` | 1 | Unclassified failure. Grove does not model this case yet; treat the message as opaque. |

### Exit code classes

Exit codes group failures so a shell caller can react without parsing JSON:

| Exit | Class | Agent response |
| --- | --- | --- |
| 0 | success | continue |
| 1 | internal | report; do not retry blindly |
| 2 | usage | fix the invocation |
| 3 | not found | re-discover state (`gw context --format json`) |
| 4 | conflict | resolve state, or re-plan |
| 5 | precondition | fix the environment |
| 6 | permission | escalate to a human |
| 7 | transient | retry with backoff |
| 8 | cancelled | stop |

Only exit 7 is safe to retry unconditionally.

## Compatibility policy

`schemaVersion` describes the envelope, not the per-command `result` payloads.

Compatible changes (no version bump):

- adding a field to the envelope, a `result`, or an `error.details`
- adding a new error code, exit-code class, or `next_actions` entry
- changing any `message`, `fix`, or `description` text
- adding a command or flag

Breaking changes (bump `schemaVersion`, announce in `CHANGELOG.md`):

- removing or renaming an envelope field
- removing or renaming an error code
- changing an existing code's exit class
- changing the type or meaning of an existing `result` field

Clients should:

1. reject `schemaVersion` greater than the version they were written against;
2. ignore unknown fields and unknown error codes (fall back to the exit class);
3. read `ok` first, then `error.code`, then `result`.

## Lifecycle example

A full create → inspect → sync → delete loop with no human-formatted output:

```bash
# 1. Discover where we are and what exists.
gw context --format json

# 2. Create a workspace across two repos.
gw create feat-x -r svc-auth,api-gateway -b feat/x --format json

# 3. Inspect per-repo git state.
gw status feat-x --format json

# 4. Rebase onto base branches; per-repo outcomes come back in result.repos.
gw sync feat-x --format json

# 5. Preview a destructive operation before running it.
gw plan delete feat-x --format json > plan.json
gw apply plan.json --format json
```

Every step returns one envelope; a non-zero exit tells the caller which class of
recovery to attempt.

## Plan and apply

Mutating operations have a review step. `gw plan` describes what would change;
`gw apply` executes a plan that has been reviewed.

```bash
gw plan create feat-x -r svc-auth,api-gateway -b feat/x --format json > plan.json
gw plan delete feat-x --format json
gw apply plan.json --format json
gw plan create feat-x -r api -b feat/x --format json | gw apply - --format json
```

A plan lists every repository, path, and branch it would touch, with each change
marked `destructive` or not:

```json
{
  "schema_version": 1,
  "kind": "delete",
  "workspace": "feat-x",
  "destructive": true,
  "changes": [
    { "action": "remove_worktree", "repo": "api", "path": "/…/feat-x/api", "destructive": true },
    { "action": "delete_branch", "repo": "api", "branch": "feat/x", "destructive": true,
      "detail": "force-deleted, including unmerged commits" }
  ],
  "warnings": ["api has uncommitted changes that would be destroyed"],
  "fingerprint": "3b1985…"
}
```

Two guarantees make a plan worth more than a printed warning:

1. **Same validation path.** A plan is produced by the checks execution runs, so
   a plan that succeeds cannot fail validation at apply time.
2. **State pinning.** The `fingerprint` covers the state the plan depends on. For
   a delete that includes each repo's exact uncommitted changes and current
   commit, so work added after review — even to a repo that was already dirty, or
   a commit made on a clean one — invalidates the plan. `gw apply` recomputes it
   and fails with `STATE_CHANGED` (exit 4) rather than applying a plan that was
   reviewed against a different world.

`gw apply` accepts a bare plan document, a saved `--format json` envelope, or `-`
for stdin. A saved *failure* envelope is refused rather than parsed as an empty
plan. `schema_version` versions the plan document independently of the response
envelope; an unrecognized version is refused instead of misread.

Plans are non-interactive by design: `gw plan create` requires `--repos`,
`--preset`, or `--all` rather than falling back to a picker, because a plan has
to be reproducible from its inputs.

## Cross-agent coordination

When several agents work in parallel workspaces on the same repos, they can leave
each other notes:

```bash
# In workspace "alpha": tell everyone touching these repos what changed.
gw announce -c breaking_change -m "auth tokens are now opaque strings" --format json

# In workspace "beta": notes about your repos arrive with your normal orientation.
gw context --format json      # result.announcements
gw announcements --format json # dedicated read, 30-day horizon
```

Notes are keyed by each repo's normalized remote (`git@github.com:org/api.git`
and `https://github.com/org/api` both key on `org/api`), so different worktrees
of the same upstream match. A workspace never sees its own notes. Notes expire
after 30 days; `gw context` shows the last 7 days, capped at 20.

Categories: `breaking_change`, `warning`, `status`, `info`.

Coordination is advisory. An unreadable store degrades to zero announcements
rather than failing the command an agent was actually running.

## Migrating from the MCP server

Grove ≤ 1.1.11 shipped a built-in MCP server (`gw mcp-serve`) and wrote a `grove`
entry into each workspace's `.mcp.json`. Both were removed — the CLI covers the
same ground for any client with shell access.

```bash
gw doctor         # reports leftover .mcp.json grove entries
gw doctor --fix   # removes only Grove's entry, preserving other MCP servers
```

The MCP server's `announce` / `get_announcements` tools live on as `gw announce`
and `gw announcements` (see above). Coordination came from a shared store on
disk, not from the protocol, so the CLI provides it to any agent with a shell —
and `gw context` now delivers notes during orientation instead of hoping an agent
notices a tool in a list. The SQLite database is gone; the store is a directory of
small JSON files under `~/.grove/announcements/`.

If you need an MCP surface, an external adapter can wrap this CLI contract
without changes to Grove core.
