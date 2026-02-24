# Grove (`gw`)

Git worktree workspace orchestrator. One command creates a workspace folder with worktrees from multiple repos on the same branch.

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

This enables `gw go` to change your working directory.

## Usage

```bash
gw init ~/dev                                          # register repos directory
gw create my-feature -r svc-a,svc-b -b feat/login     # create workspace
gw list                                                # list workspaces
gw status my-feature                                   # git status across repos
gw go my-feature                                       # cd into workspace
gw delete my-feature                                   # clean up
```

## What it does

- Creates git worktrees from multiple repos into `~/.grove/workspaces/<name>/`
- Auto-creates branches if they don't exist
- Rolls back on partial failure
- Prevents duplicate worktrees for the same branch

## Requirements

Python 3.12+ (installed automatically by Homebrew)
