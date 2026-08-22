<p align="center">
  <img src="assets/logo-light.png" alt="Grove logo" width="120">
</p>

<h1 align="center">Grove (<code>gw</code>)</h1>

<p align="center"><b>grove</b> /ɡrōv/ <i>noun</i> — a small group of trees growing together.</p>

<p align="center">
  <a href="https://github.com/nicksenap/grove/actions/workflows/ci.yml"><img src="https://github.com/nicksenap/grove/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/nicksenap/grove/releases/latest"><img src="https://img.shields.io/github/v/release/nicksenap/grove" alt="Release"></a>
  <a href="https://github.com/nicksenap/grove/blob/master/LICENSE"><img src="https://img.shields.io/github/license/nicksenap/grove" alt="License"></a>
</p>

## Why?

Monorepos solve cross-project work, but not everyone has one. You've got separate repos, separate CI, separate deploys — and that's fine until you need to work across them.

One feature across three services means `git worktree add` three times, tracking three branches, jumping between three directories, cleaning up three worktrees when you're done. It's annoying.

Grove gives you the multi-repo worktree workflow that monorepos get for free. One command, one workspace, all repos on the same branch.

## Getting Started

### 1. Install Grove

Pick **any one** of the following methods:

**Homebrew**

```bash
brew install nicksenap/grove/grove
```

**Go install**

```bash
go install github.com/nicksenap/grove/cmd/gw@latest
```

**From source**

```bash
git clone https://github.com/nicksenap/grove.git
cd grove && go build -o gw ./cmd/gw
mv gw /usr/local/bin/
```

### 2. Add shell integration

This enables `gw go` to change your working directory and auto-cds into new workspaces after `gw create`.

**Bash / Zsh** — add to `.zshrc` or `.bashrc`:

```bash
eval "$(gw shell-init)"
```

**Nushell** — generate and source the init file:

```nu
gw shell-init --shell nu | save -f ~/.config/nushell/grove.nu
# then add to config.nu:
source grove.nu
```

## Upgrading

If you installed with Homebrew:

```bash
brew update && brew upgrade grove
```

## Usage

```bash
# Register your repo directories (one-time)
gw init ~/dev ~/work/microservices

# Create a workspace: name the branch, pull repos from a preset or a list
gw create -b feat/login -p backend        # repos from a saved preset
gw create -b feat/login -r svc-a,svc-b    # …or an ad-hoc repo list

# Work across the whole workspace
gw go my-feature       # cd into the workspace
gw status my-feature   # git status across every repo
gw sync my-feature     # rebase all repos onto their base branch
gw run my-feature      # run dev processes (TUI)

# Clean up when done (destructive; use a pre_delete hook to enforce policy)
gw delete my-feature   # removes worktrees, branches, and workspace files
```

Interactive menus support **type-to-search** filtering, arrow-key navigation (single-select), or arrow + tab (multi-select) with an `(all)` shortcut.

Presets, plugins, hooks, and the full command reference are covered in [Workflows](openwiki/workflows.md) and [Operations](openwiki/operations.md).

## Documentation

Full documentation lives in the [OpenWiki](openwiki/quickstart.md) — start with the quickstart, then dive into the area you need:

- [Quickstart](openwiki/quickstart.md) — install, first commands, key concepts, and a source map
- [Architecture](openwiki/architecture.md) — layered design, data model, concurrency, and key decisions
- [Workflows](openwiki/workflows.md) — how each command maps to code (create, sync, run, delete, presets…)
- [Operations](openwiki/operations.md) — configuration, hooks, state, troubleshooting, and release process
- [Integrations](openwiki/integrations.md) — plugins and workspace source provenance

### Focused topic guides

- [Hooks](docs/hooks.md) — global hooks (terminal integration) & per-repo hooks (`.grove.toml`, `gw run`)
- [Plugins](docs/plugins.md) — extend gw with external commands
- [Recipes](docs/recipe-v1.md) — strict schema, [workspace creation](docs/recipe-execution.md), and the [prepared-claim spike](docs/prepared-workspace-claim-spike.md)
- [AI coding tools](docs/ai-tools.md) — vendor-neutral agent workflows

## Requirements

No dependencies — single static binary. Requires `git` on PATH.
