# Plugin Extraction: Claude & Zellij

Grove's core should be a pure git worktree orchestrator. Tool-specific integrations
(Claude Code, Zellij) should live in plugins, composable via the hook system.

## Why

Today Grove has Claude Code and Zellij logic baked into its core. This was the right
move initially — build it in, validate the feature, iterate fast. But now that the
integrations are proven, keeping them in core has costs:

- **Most Grove users don't use all the tools.** A tmux user carries dead Zellij code.
  A Cursor user carries Claude memory sync. The core is harder to understand because
  of integrations they don't care about.
- **Every new tool becomes a config toggle.** What about Cursor, Windsurf, tmux, Warp,
  the next editor or multiplexer? Either each one gets hardcoded into core (turning it
  into a grab-bag of integrations) or some are plugins and some aren't (inconsistent).
- **The integrations are take-it-or-leave-it.** Users can't customize the Claude memory
  flow, skip sync for certain repos, or chain their own steps.

The plugin + hook architecture makes Grove tool-agnostic. Hooks are the contract,
plugins are the adapters:

- Claude user? `gw plugin install nicksenap/gw-claude`
- Zellij user? `gw plugin install nicksenap/gw-zellij`
- tmux user? `on_close = "tmux kill-pane"` — one line, no plugin needed
- Cursor user? Someone writes `gw-cursor`, or just a `post_create` shell one-liner
- Something we've never heard of? They write a hook or a plugin. Grove doesn't need to know.

The friction is real but one-time: install a plugin, add a few lines to config, done.
After that it's invisible. And `gw init` can offer to install common plugins
interactively to smooth the first-run experience.

## Current state

### Claude-specific code in core

| Location | What it does |
|---|---|
| `internal/claude/claude.go` | `RehydrateMemory`, `HarvestMemory`, `MigrateMemoryDir`, `FindOrphanedMemoryDirs`, `EncodePath` |
| `internal/claude/claude_test.go` | Tests for the above |
| `internal/hook/hook.go` | Claude Code hook event handler (`Handler`, `StatusData`, event processing) |
| `internal/hook/hook_test.go` | Tests for hook handler |
| `internal/hook/installer.go` | Registers hooks in `~/.claude/settings.json` (`Installer`, `Install`, `Uninstall`, `Backup`) |
| `internal/hook/installer_test.go` | Tests for installer |
| `cmd/claude_hook.go` | `gw hook install/uninstall/status` subcommands |
| `cmd/root.go` | Registers `claudeHookCmd` and `gw _hook` hidden command |
| `internal/workspace/workspace.go:97-103` | Rehydrate memory on create (gated by `claude_memory_sync`) |
| `internal/workspace/workspace.go:289-295` | Harvest memory on delete |
| `internal/workspace/workspace.go:411-415` | Migrate memory dir on rename |
| `internal/workspace/service.go` | `ClaudeDir` field on Service |
| `internal/workspace/workspace.go:849` | Orphan detection in Doctor |
| `internal/models/models.go:76` | `ClaudeMemorySync` config field |
| `cmd/create.go` | Legacy `copyParentCLAUDEmd` fallback |
| `cmd/doctor.go` | Doctor nudge for `post_create` hook (gated on `~/.claude`) |

### Zellij-specific code in core

| Location | What it does |
|---|---|
| `cmd/go_cmd.go:209-214` | `zellijCloseFallback()` — runs `zellij action close-pane` |
| `cmd/go_cmd.go:45-51` | `on_close` hook with Zellij fallback |
| `cmd/doctor.go:68-77` | Doctor nudge for `on_close` hook (gated on `ZELLIJ_SESSION_NAME`) |
| `internal/hook/hook.go:37,141` | `ZellijSession` field in status data |

## Target architecture

### `gw-claude` plugin

A standalone binary (Go, separate repo) installed via `gw plugin install nicksenap/gw-claude`.

**Subcommands:**

```
gw claude sync rehydrate <workspace-path>    # copy memory from source repos to worktrees
gw claude sync harvest <workspace-path>      # copy memory from worktrees back to source repos
gw claude sync migrate <old-path> <new-path> # migrate memory dir on rename
gw claude copy-md <workspace-path>           # copy CLAUDE.md from parent dir
gw claude doctor                             # check for orphaned memory dirs
gw claude hook install                       # register hooks in ~/.claude/settings.json
gw claude hook uninstall                     # remove hooks
gw claude hook status                        # check if hooks are installed
gw claude hook handle --event <event>        # handle a Claude Code hook event (status tracking)
```

**Environment variables available from plugin system:**
- `GROVE_DIR` — `~/.grove`
- `GROVE_CONFIG` — `~/.grove/config.toml`
- `GROVE_STATE` — `~/.grove/state.json`
- `GROVE_WORKSPACE` — current workspace name (if in one)

**Hook configuration:**

```toml
[hooks]
post_create = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
pre_delete = "gw claude sync harvest {path}"
post_rename = "gw claude sync migrate {old_path} {path}"
```

### `gw-zellij` plugin

Even simpler — could be a shell script.

**Subcommands:**

```
gw zellij close-pane    # close current Zellij pane
```

**Hook configuration:**

```toml
[hooks]
on_close = "gw zellij close-pane"
```

## What stays in core

- `internal/lifecycle/` — the hook runner (fires `post_create`, `pre_delete`, `on_close`, etc.)
- `internal/plugin/` — plugin discovery, execution, install from GitHub
- `[hooks]` config section in `internal/models/`
- `gw go --close-tab` — fires `on_close` hook (no Zellij knowledge)
- `gw create` — fires `post_create` hook (no Claude knowledge)
- `gw delete` — fires `pre_delete` hook (no Claude knowledge)

## New lifecycle hooks needed

| Hook | When | Vars |
|---|---|---|
| `post_create` | After workspace creation | `{name}`, `{path}`, `{branch}` |
| `pre_delete` | Before worktree removal | `{name}`, `{path}`, `{branch}` |
| `post_rename` | After workspace rename | `{name}`, `{path}`, `{branch}`, `{old_path}` |
| `on_close` | Already exists | `{name}`, `{path}`, `{branch}` |

`post_rename` needs a new `{old_path}` var added to `lifecycle.Vars`.

> **Note:** `post_rename` is deferred — rename is rare and the only consumer
> (Claude memory migration) is an edge case. A TODO in `workspace.Rename()`
> marks the spot. Add the hook + `{old_path}` var if a real need surfaces.

## Migration plan

### Phase 1: Add missing hooks + vars

1. ~~Add `pre_delete` hook call before teardown~~ ✅ (`cmd/delete.go`)
2. ~~`post_rename` + `{old_path}`~~ deferred (TODO in `workspace.Rename()`)

### Phase 2: Create `gw-claude` plugin repo

1. New repo `nicksenap/gw-claude`
2. Move `internal/claude/` logic into plugin
3. Move `internal/hook/` (handler + installer) into plugin
4. GoReleaser setup for cross-platform binary distribution
5. `gw plugin install nicksenap/gw-claude` works

### Phase 3: Create `gw-zellij` plugin

1. New repo `nicksenap/gw-zellij` (or just a shell script)
2. Single command: `gw zellij close-pane` → `zellij action close-pane`

### Phase 4: Remove from core

1. Delete `internal/claude/`, `internal/hook/`
2. Delete `cmd/claude_hook.go`
3. Remove `claude_memory_sync` from config model
4. Remove `ClaudeDir` from Service
5. Remove `zellijCloseFallback()` from `cmd/go_cmd.go`
6. Remove Claude/Zellij doctor checks
7. Remove `ZellijSession` from hook status data
8. Update doctor to suggest `gw plugin install` if plugins are missing

## Golden path recipes

> **TODO:** Write these once the plugins exist and exact commands are finalized.

Recipes for common setups — copy-paste into `~/.grove/config.toml` after installing
the relevant plugins. Should cover at least:

- Claude Code + Zellij (the "full" setup)
- Claude Code + tmux
- Minimal (no plugins, shell one-liners only)
- Cursor / other editors (community contributed)

These should live in the main README or a dedicated `docs/recipes.md` once stable.

## Open questions

- Should the hook status tracking (`internal/hook/`) be part of `gw-claude` or stay in core?
  It powers gw-dash's session monitoring. If it stays in core, it's a generic "Claude Code
  session tracker" which is still Claude-specific. Probably belongs in the plugin.
- Should `gw-dash` depend on `gw-claude` for status data, or should status be a shared format?
- Do we need a `post_delete` hook too (after cleanup)?
