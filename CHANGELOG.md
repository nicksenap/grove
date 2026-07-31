# Changelog

## Unreleased

### Breaking

- Removed Grove's built-in MCP server. `gw mcp-serve`, the generated `.mcp.json`
  `grove` entry, the announcements SQLite database, and the `announce` /
  `get_announcements` MCP tools are gone — the latter two return as the
  `gw announce` / `gw announcements` commands below. The `gw` CLI is now the only
  first-party agent interface. Run `gw doctor --fix` to strip the stale `grove` entry from
  `.mcp.json` files in existing workspaces (other MCP servers are preserved).

### Features

- `gw announce` / `gw announcements`: cross-workspace coordination for agents
  working in parallel, replacing the MCP server's `announce` /
  `get_announcements` tools. Notes are keyed by normalized repo remote, expire
  after 30 days, and recent ones surface in `gw context` so an agent receives
  them while orienting. Backed by a lock-free directory of JSON files under
  `~/.grove/announcements/` — no SQLite.

### Maintenance

- Dropped the `modernc.org/sqlite` dependency tree; the release binary shrank
  from 13.0 MB to 9.0 MB (-31%).

## v1.1.11

### Features

- `gw create <name>` now defaults the interactive branch prompt to the workspace name — just hit Enter to accept it (shown as a Confirm-style bracket default).

### Fixes

- The update notice now suggests the correct Homebrew command: `brew update && brew upgrade grove` (the formula is `grove`, not `gw`).

### Maintenance

- Bumped `modernc.org/sqlite` and `actions/setup-go` (6 → 7).
- README touch-ups.

## v1.1.10

### Fixes

- `gw status` now reports each worktree's **live** current branch instead of the branch recorded when the workspace was created. If you `git switch` to a different branch inside a worktree, `gw status` (and its `--json` output) now reflect where the worktree actually is, and a detached HEAD is shown as `(detached)`. Branch detection falls back to the recorded branch if the live lookup fails, so status stays robust.

Special thanks to [@igor-kupczynski](https://github.com/igor-kupczynski) for spotting and fixing the stale status branch reporting. 🙌

## v1.1.9

### Features

- Global lifecycle hooks (`post_create`, `pre_delete`, `on_close`) can now be written as a **table with metadata**, not just a command string. The new `[hooks.<name>]` form accepts `command`, `description`, `stream`, `timeout`, and `on_failure`:

  ```toml
  [hooks.post_create]
  command     = "npm install && npm run build"
  description = "Install deps and build assets"
  stream      = true        # stream output live to the terminal
  timeout     = "5m"         # abort if the hook runs too long
  on_failure  = "abort"      # "warn" (default) or "abort"
  ```

  The plain string form (`post_create = "..."`) still works unchanged, and config saved by `gw wizard` keeps metadata-free hooks as bare strings.
- `stream = true` streams a hook's output **live** to the terminal as it runs, each line prefixed with the hook name (e.g. `[post_create] added 412 packages`) — so a hook that installs dependencies or builds assets shows progress instead of looking hung.
- When a hook is **not** streaming (the default), its output is captured and **echoed on failure** instead of being discarded — so a failed hook shows the actual command output rather than a bare `exit status 1`. Successful non-streaming hooks stay quiet.
- `timeout` aborts a runaway hook after a [Go duration](https://pkg.go.dev/time#ParseDuration), and `on_failure = "abort"` lets a hook make its failure fatal to the command. Hook output goes to stderr, keeping Grove's stdout clean for shell integration.

### Internal

- Extracted the per-line prefixing writer (previously private to `gw run`) into a shared `internal/streamio` package, now used by both `gw run` and the hook paths. It also fixes a dropped trailing line (output with no final newline) and a mid-line re-prefix bug on very large unbroken output.

Special thanks to [@fabianhuss](https://github.com/fabianhuss) for the metadata-driven hooks work that powers this release. 🙌

## v1.1.8

### Features

- `gw create` can now seed a workspace from an **existing remote branch** with `--track` (e.g. a pull-request head): instead of creating a new branch from the base, it checks out `origin/<branch>` as a tracking worktree. If the remote branch is missing (deleted, force-pushed, or a fork PR whose head lives on another remote), it falls back to creating a new branch from base with a warning.
- `gw create` records where a workspace came from. New `--source-url`, `--source-provider`, `--source-ref`, and `--source-title` flags persist an opaque `source` link on the workspace, which is surfaced in `gw status` (and its `--json`) and exposed to the `post_create` hook via the new `{source_url}`, `{source_ref}`, and `{source_title}` placeholders.
- `gw create --repos` now accepts git URLs and clones them into the first configured repo dir (matching `gw add-repo` behavior).
- New `gw repos` command lists discovered repos with their origin remote and derived `owner/repo` name. `gw repos --json` gives machine-readable output so external tooling can map a remote `owner/repo` back to a local clone.
- New global `--no-hooks` flag (shorthand `-n`) skips all global lifecycle hooks (`post_create`, `pre_delete`, `on_close`) for a single command — useful for scripting or one-off operations. Per-repo `.grove.toml` hooks are unaffected.

These primitives are provider-agnostic building blocks: they let an external plugin (e.g. `gw-grab`) turn a GitHub PR / Notion / Slack URL into a ready-to-work workspace, while Grove core stays a pure git worktree orchestrator.

### Internal

- e2e suite now covers `gw repos`, `gw create --track` (incl. the missing-remote fallback), source-link persistence/surfacing, and the `post_create` `{source_*}` placeholders. The MCP e2e tests now use a portable timeout wrapper (`gtimeout` fallback) so the suite runs green on macOS.

## v1.1.7

### Fixes

- `gw go --delete` now logs a warning to `~/.grove/grove.log` when the detached cleanup subprocess fails to spawn, instead of silently leaving the workspace on disk.
- `gw mcp` now rejects malformed JSON-RPC messages (e.g. `"method": 123`) instead of coercing them to zero values. Previously, bad fields were silently dropped and the server replied with a confusing "Method not found: " error (trailing empty string); now they take the consistent "skip malformed" path.

## v1.1.6

### Fixes

- `.mcp.json` is no longer written into every repo worktree — only the workspace root. The per-repo copies were leaving every worktree dirty with an untracked file, which caused `gw sync` to refuse rebasing. Claude Code picks up the workspace-root `.mcp.json` since the shell integration `cd`s you there after `gw create`.
- Nushell auto-cd after `gw create` is now more robust. The wrapper uses an explicit `mktemp` template path (avoiding ambiguity between nushell's builtin and BSD `mktemp -t`), safer file reads, and prints the workspace path as a fallback if `cd` can't happen.
- Errors during `.mcp.json` cleanup on `gw delete` are now logged instead of silently swallowed.

### Migration note

Existing workspaces created before this release still have stale `.mcp.json` files in each repo worktree. Clean them up with:

```bash
find ~/.grove/workspaces/*/*/.mcp.json -delete
```

This preserves the workspace-root `.mcp.json` files.

## v1.1.5

### Fixes

- Workspace deletion now fully cleans up branches, even when they contain unmerged commits. Previously, `gw delete` would warn "branch has unmerged commits, kept" but still remove the workspace from `gw list` — leaving an orphan branch with no way to find it through Grove.
- Fetch failure warnings during `gw create` and `gw sync` now explain that local state is used, instead of the vague "continuing" message.

## v1.1.4

### New: `gw bug-report`

Collects system info, workspace state, doctor output, and recent logs, then opens a pre-filled GitHub issue in your browser for review before submitting. Use `--print` to output the report to stdout instead.

```bash
gw bug-report          # opens GitHub issue in browser
gw bug-report --print  # prints report to stdout
```

Auto-detects non-TTY environments (CI, piped output) and prints instead of launching a browser.

### `--json` flag for `preset` and `plugin list`

All table-rendering commands now support `--json` / `-j` for machine-readable output:

```bash
gw preset list --json
gw preset show backend --json
gw plugin list --json
```

### Clone retry with exponential backoff

`git clone` operations now retry up to 3 times with exponential backoff (1s, 2s, 4s) on transient network failures. Auth errors (SSH key issues, host key verification) are detected and fail immediately without retrying. Partial clone directories are cleaned up between attempts.

## v1.1.3

### `gw add-repo` now supports remote git URLs

Pass HTTPS, SSH, or `file://` URLs directly to `--repos` and Grove will clone the repo into your first configured `repo_dir` before adding it to the workspace. Works alongside local repo names — mix and match in a single command.

```bash
gw add-repo my-workspace -r https://github.com/owner/new-service.git
gw add-repo my-workspace -r api,https://github.com/owner/lib.git
```

Clones are idempotent: if the repo already exists locally, it's reused. Includes path traversal protection and remote URL verification on re-use.

## v1.1.2

### New: `gw create --replace`

Tear down the current workspace and create a new one in a single step. Detects the current workspace from your cwd, prompts for confirmation (pass `-f` to skip), runs the `pre_delete` hook, deletes it, then creates the new workspace. The old branch is freed so the new workspace can reuse it.

```bash
cd ~/.grove/workspaces/old-feature
gw create new-feature -b feat/new -r api,web --replace
```

### `gw add-repo` auto-detects current workspace

Running `gw add-repo` with no workspace NAME now defaults to the workspace containing your cwd instead of always showing a picker — matching the behavior of `gw status`, `gw run`, and `gw go`. Falls back to the picker when you're not inside a workspace.

## v1.1.0

### Plugin architecture — Grove is now tool-agnostic

Claude Code and Zellij integrations have been extracted from core into standalone plugins. Grove's core is now a pure git worktree orchestrator; tool-specific behavior is composable via lifecycle hooks.

**Install plugins:**

```bash
gw plugin install nicksenap/gw-claude   # Claude Code memory sync + session tracking
gw plugin install nicksenap/gw-zellij   # Zellij close-pane
```

**Or run the new wizard:**

```bash
gw wizard   # detects your tools, installs plugins, configures hooks
```

### New: `pre_delete` lifecycle hook

Fires before workspace teardown. Used by `gw-claude` to harvest memory back to source repos before worktrees are destroyed.

```toml
[hooks]
post_create = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
pre_delete = "gw claude sync harvest {path}"
on_close = "gw zellij close-pane"
```

### New: `gw wizard`

Interactive setup that detects your environment (Claude Code, Zellij) and offers to install the right plugins and configure hooks. Run it after `gw init` or after upgrading.

### Breaking changes

- `gw hook install/uninstall/status` removed — use `gw claude hook install` (from the plugin) instead
- `gw _hook` hidden command removed — the plugin handles this now
- `claude_memory_sync` config field removed — memory sync is now opt-in via hooks
- Legacy Zellij fallback removed — configure `[hooks] on_close` instead
- Legacy `CLAUDE.md` copy fallback removed — use `post_create` hook instead

### Migration from v1.0.x

```bash
gw hook uninstall                        # remove old core hooks (if installed)
gw plugin install nicksenap/gw-claude    # install the plugin
gw wizard                                # configure everything interactively
```

## v1.0.4

### `gw ws delete` — interactive delete under the `ws` subcommand

`gw ws delete` now works the same as `gw delete` — with interactive multi-select when no name is given, `--force` flag, and tab completion. Consistent UX across both entry points.

### Global lifecycle hooks

New `[hooks]` section in `~/.grove/config.toml` lets you integrate Grove with any terminal multiplexer — not just Zellij.

```toml
[hooks]
on_close = "zellij action close-pane"
```

`gw go -c` now fires the `on_close` hook instead of hardcoding Zellij. Placeholders `{name}`, `{path}`, `{branch}` are expanded with shell quoting to prevent injection.

Existing Zellij users: everything keeps working via a fallback. Run `gw doctor` to see the migration hint.

### Doctor checks for missing hooks

`gw doctor` now flags when you're running inside Zellij without an `on_close` hook configured, with a suggested action to add one.
