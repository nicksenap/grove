# Hooks

Grove has two levels of hooks: **global hooks** for workspace-level actions, and **per-repo hooks** for repo-specific lifecycle events.

## Global hooks

Global hooks live in `~/.grove/config.toml` under `[hooks]`. Grove fires these on workspace-level actions — they're how you integrate with your terminal multiplexer or trigger notifications.

```toml
[hooks]
post_create = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
pre_delete = "gw claude sync harvest {path}"
on_close = "gw zellij close-pane"
```

Run `gw wizard` to configure these interactively.

### Available hooks

| Hook | Fired by | When | Example |
|------|----------|------|---------|
| `post_create` | `gw create` | After workspace creation | `gw claude sync rehydrate {path}` |
| `pre_delete` | `gw delete` | Before worktree removal | `gw claude sync harvest {path}` |
| `on_close` | `gw go -c` | Close terminal pane | `gw zellij close-pane`, `tmux kill-pane` |

### Placeholders

Grove expands placeholders in hook commands before running them via `sh -c`:

| Placeholder | Value | Example |
|---|---|---|
| `{name}` | Workspace name | `my-feature` |
| `{path}` | Workspace directory path | `/home/nick/.grove/workspaces/my-feature` |
| `{branch}` | Branch name | `feat/login` |

Values are automatically single-quoted to prevent shell injection — a branch named
`feat/x; rm -rf ~` expands to `'feat/x; rm -rf ~'`, not a destructive command.

Unused placeholders expand to an empty string.

```toml
[hooks]
post_create = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
pre_delete = "gw claude sync harvest {path}"
on_close = "tmux kill-pane"
```

### Hook output and behavior

A global hook can be written as a plain command string (above) **or** as a table
when you want to control how it runs. Both forms are valid and can be mixed:

```toml
[hooks]
pre_delete = "gw claude sync harvest {path}"   # simple string form

[hooks.post_create]                            # table form with metadata
command     = "npm install && npm run build"
description = "Install deps and build assets"
stream      = true        # show output live in the terminal
timeout     = "5m"         # abort the hook if it runs longer than this
on_failure  = "abort"      # "warn" (default) or "abort"
```

| Field | Default | Meaning |
|---|---|---|
| `command` | — | The shell command to run (required in table form). |
| `description` | — | What the hook is expected to do. Documents the hook inline. |
| `stream` | `false` | When `true`, the hook's output streams **live** to the terminal as it runs, each line prefixed with the hook name (e.g. `[post_create] added 412 packages`). Use this for hooks that install dependencies or build assets so the run doesn't look hung. |
| `timeout` | none | A [Go duration](https://pkg.go.dev/time#ParseDuration) (`30s`, `5m`, `1h`). The hook is aborted if it exceeds this, instead of hanging the terminal indefinitely. |
| `on_failure` | `warn` | `warn` logs a warning and continues; `abort` makes a hook failure fatal to the command. |

**Output when `stream` is off (the default):** the hook runs quietly and its
output is captured. On success nothing is printed; **on failure the captured
output is echoed** (prefixed) so you see what actually went wrong instead of a
bare `exit status 1`. Turn on `stream` only for hooks whose progress you want to
watch live.

> **Platform note:** `timeout` aborts a runaway hook by killing its whole
> process group, which relies on Unix process-group semantics. Grove ships for
> macOS and Linux only, so this is always available on supported platforms; the
> rest of the hook behavior is platform-independent.

Hook output goes to **stderr**, keeping Grove's stdout clean for shell
integration (e.g. `cd "$(gw go my-feature)"`).

### Skipping hooks

Pass `--no-hooks` (shorthand `-n`) to any command to skip all global hooks for that run — handy for scripting or one-off operations where you don't want side effects to fire:

```sh
gw --no-hooks create my-feature --repos svc-auth
gw -n delete my-feature --force
```

Per-repo hooks (`.grove.toml`) are unaffected.

---

## Per-repo hooks

Drop a `.grove.toml` in any repo to override defaults:

```toml
# merchant-portal/.grove.toml
base_branch = "stage"              # branch from origin/stage instead of origin/main
setup = "pnpm install"             # run after worktree creation
teardown = "rm -rf node_modules"   # run before worktree removal
pre_sync = "pnpm run build:check"  # run before rebase during sync
post_sync = "pnpm install"         # run after successful rebase
pre_run = "docker compose pull"    # run before gw run starts
run = "pnpm dev"                   # started by gw run (foreground)
post_run = "docker compose down"   # run on gw run exit / Ctrl+C
```

All hook keys accept a string or a list of commands:

```toml
setup = ["uv sync", "uv run pre-commit install"]
```

Hook failures are warnings — they never block the operation they're attached to.

## Design philosophy

The hook system follows the npm-style `pre`/`post` convention: one primitive (`run`, `sync`) with optional `pre_` and `post_` counterparts that fire around it. Instead of building special-purpose features (secret injection, dependency installs, container management), Grove gives you generic hook points and gets out of the way.

| When | Hooks | Example use |
|------|-------|-------------|
| Worktree created | `setup` | `pnpm install`, inject secrets, seed DB |
| Worktree removed | `teardown` | `rm -rf node_modules`, revoke temp creds |
| Before/after rebase | `pre_sync`, `post_sync` | Type-check before rebase, reinstall after |
| Dev session | `pre_run`, `run`, `post_run` | Pull containers, start dev server, tear down |

## `gw run`

`gw run` executes `run` hooks across all repos in a workspace, printing output with `[repo]` prefixes. It auto-detects the workspace from your current directory, or takes a name argument.

```bash
gw run              # auto-detect workspace from cwd
gw run my-feature   # explicit workspace name
```

Pre-run hooks fire before the processes start, post-run hooks fire after they exit. Ctrl+C gracefully terminates all processes.
