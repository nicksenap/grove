<p align="center">
  <img src="assets/logo-light.png" alt="Grove logo" width="120">
</p>

<h1 align="center">Grove (<code>gw</code>)</h1>

<p align="center"><b>grove</b> /ɡrōv/ <i>noun</i> — a small group of trees growing together.</p>

## Why?

Monorepos solve cross-project work, but not everyone has one. You've got separate repos, separate CI, separate deploys — and that's fine until you need to work across them.

One feature across three services means `git worktree add` three times, tracking three branches, jumping between three directories, cleaning up three worktrees when you're done. It's annoying.

Grove gives you the multi-repo worktree workflow that monorepos get for free. One command, one workspace, all repos on the same branch.

## Install

### Homebrew

```bash
brew tap nicksenap/grove
brew install grove
```

### PyPI

```bash
pipx install gw-cli
# or
pip install gw-cli
```

### From source

```bash
uv tool install .
```

Then add shell integration to your `.zshrc` (or `.bashrc`):

```bash
eval "$(gw shell-init)"
```

This enables `gw go` to change your working directory and auto-cds into new workspaces after `gw create`.

## Usage

```bash
# Setup — register one or more directories containing your repos
gw init ~/dev ~/work/microservices
gw add-dir ~/other/repos
gw remove-dir ~/old/repos
gw explore                                             # deep-scan for repos (2–3 levels)

# Workspaces
gw create my-feature -r svc-a,svc-b -b feat/login     # create workspace
gw list                                                # list workspaces
gw list -s                                             # list with git status summary
gw status my-feature                                   # git status across repos
gw sync my-feature                                     # rebase all repos onto base branch
gw go my-feature                                       # cd into workspace
gw run my-feature                                      # run dev processes (TUI)
gw add-repo my-feature -r svc-c                        # add a repo to existing workspace
gw remove-repo my-feature -r svc-a                     # remove a repo from workspace
gw rename my-feature --to new-name                     # rename a workspace
gw doctor                                              # diagnose workspace health issues
gw delete my-feature                                   # clean up (worktrees + branches)

# Presets — save repo groups for quick workspace creation
gw preset add backend -r svc-auth,svc-api,svc-worker
gw preset list
gw preset remove backend
gw create my-feature -p backend                        # use a preset instead of -r
```

All interactive menus support **type-to-search** filtering, arrow-key navigation (single-select), or arrow + tab (multi-select) with an `(all)` shortcut.

## Per-repo config

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

### Design philosophy

The hook system follows the npm-style `pre`/`post` convention: one primitive (`run`, `sync`) with optional `pre_` and `post_` counterparts that fire around it. Instead of building special-purpose features (secret injection, dependency installs, container management), Grove gives you generic hook points and gets out of the way.

| When | Hooks | Example use |
|------|-------|-------------|
| Worktree created | `setup` | `pnpm install`, inject secrets, seed DB |
| Worktree removed | `teardown` | `rm -rf node_modules`, revoke temp creds |
| Before/after rebase | `pre_sync`, `post_sync` | Type-check before rebase, reinstall after |
| Dev session | `pre_run`, `run`, `post_run` | Pull containers, start dev server, tear down |

### `gw run`

`gw run` launches a [Textual](https://github.com/Textualize/textual) TUI that manages `run` hooks across all repos. Each repo gets its own log pane with a sidebar showing status indicators (green = running, yellow = starting, red = exited with error).

| Key | Action |
|-----|--------|
| `j` / `k` / `↑` / `↓` | Navigate repos |
| `g` / `G` | Jump to first / last |
| `1`–`9` | Quick-select repo by number |
| `r` | Restart selected repo |
| `q` | Quit (terminates all processes) |

Pre-run hooks fire before the TUI launches, post-run hooks fire after it exits.

## Dashboard (`gw dash`)

A Textual TUI for monitoring Claude Code agents across all your workspaces. Agents are sorted into a kanban board with four columns — **Active**, **Attention**, **Idle**, **Done** — based on live status. Inspired by [Clorch](https://github.com/androsovm/clorch).

```bash
gw dash install   # install Claude Code hooks
gw dash           # launch the dashboard
gw dash uninstall # remove hooks
```

| Key | Action |
|-----|--------|
| `h` / `l` | Navigate columns |
| `j` / `k` | Navigate cards |
| `Enter` | Jump to agent's Zellij tab |
| `y` / `n` | Approve / deny permission request |
| `/` | Search / filter agents |
| `Escape` | Clear search |
| `r` | Refresh |
| `q` | Quit |

### How it works

Claude Code hooks write agent state to `~/.grove/status/<session_id>.json` on every event. The dashboard polls these files every 500ms and renders a real-time kanban view of all active agents.

**Tracked per agent:** status, working directory, git branch, dirty file count, last tool used, tool/error/subagent counts, activity sparkline, permission request details, and initial prompt.

### Zellij tab matching

When you press `Enter` to jump to an agent, the dashboard finds the right Zellij tab using a multi-step strategy:

| Priority | Strategy | Example |
|----------|----------|---------|
| 1 | Exact tab name = project name | `grove` → tab `grove` |
| 2 | Case-insensitive tab name | `Grove` → tab `grove` |
| 3 | Workspace name from CWD matched against tab names | CWD has `feat-rewrite` → tab matching |
| 4 | CWD path match via `zellij action dump-layout` | Agent CWD under tab CWD or vice versa |
| 5 | Project name substring in tab name | `api` → tab `public-api` |

## Terminal multiplexer integration

Grove integrates with [Zellij](https://zellij.dev/) for tab management. When running inside Zellij:

- **`gw go`** jumps to the workspace's Zellij tab, or opens a new one if it doesn't exist
- **`gw go --close-tab`** closes the current Zellij tab (useful when you're done with a workspace)
- **`gw dash`** lets you press `Enter` to jump directly to an agent's Zellij tab

Support for tmux and other terminal multiplexers is planned.

## Works with AI coding tools

Worktrees mean isolation. That makes Grove a natural fit for tools like [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — spin up a workspace, let your AI agent work across repos without touching anything else, clean up when done:

```bash
gw create -p backend -b fix/auth-bug
claude "fix the auth token expiry bug across svc-auth and api-gateway"
gw delete fix-auth-bug   # removes worktrees, branches, and workspace
```

Grove copies your `CLAUDE.md` into new workspaces, so your agent gets project context from the start.

## Requirements

Python 3.12+ (installed automatically by Homebrew)
