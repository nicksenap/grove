# Changelog

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
