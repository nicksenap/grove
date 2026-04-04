<p align="center">
  <img src="assets/logo-light.png" alt="Grove logo" width="120">
</p>

<h1 align="center">Grove (<code>gw</code>)</h1>

<p align="center"><b>grove</b> /ɡrōv/ <i>noun</i> — a small group of trees growing together.</p>

<p align="center">
  <a href="https://github.com/nicksenap/grove/actions/workflows/ci.yml"><img src="https://github.com/nicksenap/grove/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/nicksenap/grove/releases/latest"><img src="https://img.shields.io/github/v/release/nicksenap/grove" alt="Release"></a>
  <a href="https://github.com/nicksenap/grove/blob/master/LICENSE"><img src="https://img.shields.io/github/license/nicksenap/grove" alt="License"></a>
  <a href="https://goreportcard.com/report/github.com/nicksenap/grove"><img src="https://goreportcard.com/badge/github.com/nicksenap/grove" alt="Go Report Card"></a>
</p>

## Why?

Monorepos solve cross-project work, but not everyone has one. You've got separate repos, separate CI, separate deploys — and that's fine until you need to work across them.

One feature across three services means `git worktree add` three times, tracking three branches, jumping between three directories, cleaning up three worktrees when you're done. It's annoying.

Grove gives you the multi-repo worktree workflow that monorepos get for free. One command, one workspace, all repos on the same branch.

## Install

### Homebrew

```bash
brew install nicksenap/grove/grove
```

### Go install

```bash
go install github.com/nicksenap/grove/cmd/gw@latest
```

### From source

```bash
git clone https://github.com/nicksenap/grove.git
cd grove && go build -o gw ./cmd/gw
mv gw /usr/local/bin/
```

### Upgrading

```bash
brew update && brew upgrade grove
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
gw list                                                # list workspaces (compact)
gw list -s                                             # list with git status summary
gw ws show my-feature                                  # show workspace details
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
gw preset list                                         # list presets (compact)
gw preset show backend                                 # show preset details
gw preset remove backend
gw create my-feature -p backend                        # use a preset instead of -r

# Plugins — extend gw with external commands
gw plugin install nicksenap/gw-claude                  # Claude Code integration
gw plugin install nicksenap/gw-zellij                  # Zellij terminal integration
gw plugin install nicksenap/gw-dash                    # agent monitoring dashboard
gw plugin list                                         # list installed plugins
gw plugin upgrade                                      # upgrade all plugins
gw plugin remove dash                                  # uninstall a plugin
gw wizard                                              # interactive plugin + hook setup
```

All interactive menus support **type-to-search** filtering, arrow-key navigation (single-select), or arrow + tab (multi-select) with an `(all)` shortcut.

## Documentation

- [Hooks](docs/hooks.md) — global hooks (terminal integration) & per-repo hooks (`.grove.toml`, `gw run`)
- [Plugins](docs/plugins.md) — extend gw with external commands
- [AI coding tools](docs/ai-tools.md) — Claude Code workflows, MCP server

## Requirements

No dependencies — single static binary. Requires `git` on PATH.
