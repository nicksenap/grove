# Grove (`gw`)

**grove** /ɡrōv/ *noun* — a small group of trees growing together.

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
gw init ~/dev                                          # register repos directory
gw create my-feature -r svc-a,svc-b -b feat/login     # create workspace
gw list                                                # list workspaces
gw status my-feature                                   # git status across repos
gw go my-feature                                       # cd into workspace
gw delete my-feature                                   # clean up
```

All commands with selection use arrow-key navigation (single-select) or arrow + space (multi-select), with an `(all)` shortcut.

## Per-repo config

Drop a `.grove.toml` in any repo to override defaults:

```toml
# merchant-portal/.grove.toml
base_branch = "stage"              # branch from origin/stage instead of origin/main
setup = "pnpm install"             # run after worktree creation
```

`setup` accepts a string or a list of commands:

```toml
setup = ["uv sync", "uv run pre-commit install"]
```

## What it does

- Fetches latest from remotes before creating worktrees
- Creates new branches from the default remote branch (`origin/main`)
- Creates git worktrees from multiple repos into `~/.grove/workspaces/<name>/`
- Offers saved presets during interactive workspace creation
- Copies `CLAUDE.md` from your repos directory into new workspaces
- Auto-creates branches if they don't exist
- Rolls back on partial failure
- Prevents duplicate worktrees for the same branch
- Warns on startup when a newer version is available

## Requirements

Python 3.12+ (installed automatically by Homebrew)
