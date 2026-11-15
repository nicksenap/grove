---
name: grove
description: Create, inspect, sync, and delete multi-repo git worktree workspaces with the Grove CLI (`gw`). Use when the user mentions gw or Grove, wants one branch across several repos, asks for an isolated workspace or worktree for a task or PR, or wants to set up Grove. Covers setup, non-interactive flags, JSON output, and which commands destroy work.
---

# Grove (`gw`)

Grove creates a **workspace**: a directory holding one git worktree per selected repo, all on the same branch. Workspaces live under `~/.grove/workspaces/<name>/<repo>` by default; source clones are untouched.

## Rules (apply to every command)

- **Always pass explicit names and flags.** Without a TTY, pickers exit 1 with `interactive selection requires a terminal`. Never rely on a picker or prompt.
- **stdout is data, stderr is chatter.** Parse only `-o json|jsonl|name|path` or `--json` output. Never parse tables or stderr.
- **`gw delete` does not ask.** It force-removes worktrees and runs `git branch -D` on the workspace branch. Uncommitted changes and unpushed commits are lost. Follow the cleanup step below.
- **Confirmation prompts silently decline without input.** `gw remove-repo -r X` (without `-f`) and `gw create --replace` (without `-f`) print a prompt, do nothing, and exit 0. Check the result; do not assume success from the exit code.
- **Do not pass `-n`/`--no-hooks`** unless the user asks. Hooks may enforce the user's policy (for example a `pre_delete` that blocks deletion).
- **Do not run `prune --yes`, `prune branches --yes`, `reset --discard`, `doctor --fix`, or `create --replace -f`** without the user's explicit go-ahead. Run the read-only form first and show the result.
- **Do not edit the user's shell rc or `~/.grove/config.toml`** unprompted. Print the exact line or snippet and let the user apply it.

## 1. Check Grove is ready

```bash
gw --version
gw repos -o name
```

Done when `gw repos` lists the repos the user cares about.

- `gw: command not found` → install (see Setup below).
- `Grove not initialized` → `gw init <dir-containing-repos>` (the dirs must exist), then rerun `gw repos -o name`.
- A repo is missing → `gw add-dir <dir>`; discovery scans each registered dir for git repos.

## 2. Create a workspace

Pick repos by name, preset, or `--all`:

```bash
gw preset list --json                      # optional: named repo groups
gw create fix-auth -b fix/auth-expiry -r api,web
gw create -b feat/login -p backend         # name derived from branch: feat-login
gw create pr-123 -b their-branch -r api --track   # check out an existing remote branch
```

Then read back what was created. A `resolve_branch` hook can change the branch name, so do not assume it:

```bash
gw ws show fix-auth -o json
```

Done when every repo in the output has the expected `branch` and a `worktree_path`.

## 3. Work in it

```bash
cd "$(gw ws show fix-auth -o path)"        # workspace root; each repo is a subdirectory
gw status fix-auth -o jsonl                # one object per repo: branch, changed, ahead, behind
gw add-repo fix-auth -r shared-lib         # add repos later; also accepts git URLs
gw sync fix-auth                           # rebase every repo onto its base branch
gw diff --workspace fix-auth --no-pager    # one git diff across all repos, paths prefixed <repo>/
```

- Inside a workspace directory, `NAME` can be omitted for `status`, `sync`, `reset`, `add-repo`, and `remove-repo`. Passing it is still safer.
- `gw diff` passes every other argument to `git diff` (e.g. `--staged`, `main...HEAD`, `-U10`), so pass the workspace as `--workspace NAME`, not positionally. Use `--no-pager` when reading the output yourself. `--stat` and `--name-only` style output is per repo and unprefixed.
- `gw sync` skips dirty repos and aborts a failed rebase. It rewrites local branch history, so warn the user first if the branch is shared or already pushed.
- `gw go NAME` also prints the path, but without a trailing newline and meant for the user's shell function. Prefer `gw ws show NAME -o path`.

## 4. Clean up

Before deleting, confirm nothing will be lost:

```bash
gw status fix-auth -o jsonl
```

Every repo must have `changed` equal to 0 and `ahead` equal to `"0"`. `ahead: "-"` means the branch has no upstream (never pushed), so its commits exist only locally: run `git -C <worktree_path> log --oneline <base>..HEAD` (base is usually `main`) and treat any output as unpushed work. If anything would be lost, stop and tell the user. Then:

```bash
gw delete fix-auth
```

For many workspaces: `gw prune --min-age 14d --json` lists old candidates. Show the list, and only after the user agrees run `gw prune --min-age 14d --yes`.

## Setup on a fresh machine

Install one way (the user may prefer one):

```bash
brew install nicksenap/grove/grove
curl -fsSL https://raw.githubusercontent.com/nicksenap/grove/master/scripts/install.sh | sh
go install github.com/nicksenap/grove/cmd/gw@latest
```

Then:

```bash
gw init ~/dev ~/work          # directories that contain git repos
gw repos -o name              # confirm discovery
gw preset add backend -r api,worker   # optional
gw doctor --json              # `null` means healthy
```

Ask the user to add shell integration to their rc file (needed only for `gw go` to change *their* shell's directory). The agent does not need it:

```bash
eval "$(gw shell-init)"       # bash/zsh; `gw shell-init --help` for others
```

## More

- Every flag, output format, and JSON field: [references/commands.md](references/commands.md)
- Lifecycle hooks (`resolve_branch`, `post_create`, `post_add_repo`, `pre_delete`, `on_close`): https://github.com/nicksenap/grove/blob/master/docs/hooks.md
- Per-repo `.grove.toml` (`base_branch`, `setup`) and the rest: `gw <command> --help`
