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

<p align="center">
  <a href="assets/grove-brag.mp4"><img src="assets/grove-brag.gif" alt="Grove turns one feature across three repositories into one worktree workspace" width="900"></a>
</p>

## Why?

Monorepos solve cross-project work, but not everyone has one. You've got separate repos, separate CI, separate deploys — and that's fine until you need to work across them.

One feature across three services means `git worktree add` three times, tracking three branches, jumping between three directories, cleaning up three worktrees when you're done. It's annoying.

Grove gives you the multi-repo worktree workflow that monorepos get for free. One command creates a workspace with every repo on the same branch, and `gw status` shows all of them in one table.

## Getting Started

Grove is a single static binary with no dependencies. It requires `git` on `PATH`.

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

### 3. Create your first workspace

```bash
gw init ~/dev                          # register the directory that holds your repos
gw create -b feat/login -r svc-a,svc-b # create the feat-login workspace across both repos
gw status feat-login                   # see every repo's branch and git status in one table
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
gw reset my-feature    # switch every repo back to the workspace branch, then sync

# Clean up when done (destructive; use a pre_delete hook to enforce policy)
gw delete my-feature   # removes worktrees, branches, and workspace files
gw prune               # list workspaces older than 7 days
gw prune --yes         # delete them (same two-phase cleanup as gw delete)
```

Interactive menus support **type-to-search** filtering, arrow-key navigation (single-select), or arrow + tab (multi-select) with an `(all)` shortcut.

Repos can carry a `.grove.toml` so every new worktree is ready to work in:

```toml
# svc-a/.grove.toml
base_branch = "stage"   # branch from origin/stage instead of origin/main
setup = "pnpm install"  # run after the worktree is created
```

Presets, plugins, hooks, and the full command reference are covered in [Workflows](openwiki/workflows.md) and [Operations](openwiki/operations.md).

## Upgrading

If you installed with Homebrew:

```bash
brew update && brew upgrade grove
```

## Documentation

- [Quickstart](openwiki/quickstart.md) — install, first commands, and key concepts
- [Workflows](openwiki/workflows.md) — creating, syncing, resetting, and deleting workspaces
- [Operations](openwiki/operations.md) — configuration, hooks, state, and troubleshooting
- [Hooks](docs/hooks.md) — global hooks and per-repo `.grove.toml` hooks
- [Plugins](docs/plugins.md) — extend `gw` with external commands
- [Recipes](docs/recipe-v1.md) — declarative workspace creation from YAML
- [AI coding tools](docs/ai-tools.md) — vendor-neutral agent workflows

Contributors can start with the [Architecture](openwiki/architecture.md) and [Integrations](openwiki/integrations.md) notes.
