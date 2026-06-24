# Changelog

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
