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

### Homebrew (Go — recommended)

```bash
brew install --cask nicksenap/grove/grove-go
```

### Homebrew (Python)

```bash
brew install nicksenap/grove/grove
```

### PyPI

```bash
pipx install gw-cli
# or
pip install gw-cli
```

### From source (Go)

```bash
cd go && go build -o gw . && mv gw /usr/local/bin/
```

### From source (Python)

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

## Documentation

- [Per-repo config & hooks](docs/hooks.md) — `.grove.toml`, lifecycle hooks, `gw run`
- [Agent dashboard](docs/dashboard.md) — `gw dash`, Zellij integration
- [AI coding tools](docs/ai-tools.md) — Claude Code workflows, MCP server

## Go rewrite

Grove is being rewritten in Go. The Python version works but has real distribution pain — Python version conflicts, `pipx`/`uv` packaging quirks, and slow startup (~300ms import overhead). The Go binary is a single static executable with instant startup and zero dependencies.

### Status

The Go version covers the core workflow. What's missing are the TUI features.

| Command | Go | Notes |
|---|---|---|
| `init`, `add-dir`, `remove-dir` | Done | |
| `explore` | Done | Deep scan with remote URL caching |
| `create` | Done | Parallel fetch, rollback, hooks |
| `delete` | Done | Parallel teardown, hooks |
| `list`, `status` | Done | Including `-s` summary and PR status |
| `sync` | Done | Parallel rebase, conflict handling |
| `go` | Done | Including `--close-tab` for Zellij |
| `add-repo`, `remove-repo` | Done | |
| `rename`, `doctor`, `stats` | Done | |
| `preset` | Done | |
| `shell-init` | Done | |
| `mcp-serve` | Done | JSON-RPC server for Claude Code |
| `hook` | Done | Claude Code hook handler |
| `run` | Partial | Inline prefix output, no split-pane TUI |
| `dash` | Not yet | Planned as separate plugin/binary |

285 tests passing (226 unit + 59 e2e).

## Requirements

- **Go version:** No dependencies — single static binary
- **Python version:** Python 3.12+ (installed automatically by Homebrew)
