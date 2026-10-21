# `gw` command reference

Global flags: `--verbose` (debug log to `~/.grove/grove.log`), `-n`/`--no-hooks` (skip lifecycle hooks; avoid unless asked), `-v`/`--version`.

**Interactive?** column: what happens if you omit the listed args. "picker" means exit 1 without a TTY; "prompt" means it prints a question and, with no stdin, silently declines and exits 0.

## Workspaces

| Command | Non-interactive form | Interactive if omitted | Destroys work? |
|---|---|---|---|
| `gw create` | `gw create [NAME] -b BRANCH (-r a,b \| -p PRESET \| --all)` | `-r/-p/--all` → picker; `-b` → error | No |
| `gw create --replace` | `gw create NEW -b B -r a --replace -f` (run inside the workspace to replace) | prompt without `-f` | **Yes**: deletes the current workspace first, even if the create then fails |
| `gw ws show` | `gw ws show NAME -o json\|path` | — | No |
| `gw list` / `gw ws list` | `gw list -o jsonl [-s]` (`-s` adds git status) | — | No |
| `gw status` | `gw status [NAME] -o jsonl [-P]` (`-P` adds PR status via gh/glab) | — | No |
| `gw diff` | `gw diff [--workspace NAME] [--no-pager] [<git diff args>...]` | — (pages when stdout is a TTY) | No |
| `gw go` | `gw go NAME` (prints path, no newline) | >1 workspace → picker | `-d` deletes the workspace after navigating away |
| `gw add-repo` | `gw add-repo [NAME] -r a,b` (names or git URLs; URLs clone into the first repo dir) | `-r` → picker | No |
| `gw remove-repo` | `gw remove-repo [NAME] -r a -f`, or `echo y \| gw remove-repo [NAME] -r a` to keep the dirty-worktree check | `-r` → picker; always prompts without `-f` | **Yes**: removes the worktree and its branch; `-f` also skips the dirty check |
| `gw rename` | `gw rename OLD --to NEW` | `OLD` → picker | No |
| `gw sync` | `gw sync [NAME]` | — | Rebases local branches onto base (history rewrite); skips dirty repos |
| `gw reset` | `gw reset [NAME] [--discard]` | — | `--discard` drops tracked local changes |
| `gw delete` / `gw ws delete` | `gw delete NAME...`; or `... \| gw delete --stdin --yes` | no names → picker | **Yes, no prompt**: force-removes worktrees and `git branch -D` (branches that existed before the workspace are kept) |
| `gw prune` | `gw prune [--min-age 7d] --json` to list; add `--yes` to delete | — | `--yes`: deletes every listed workspace like `gw delete` |
| `gw prune branches` | `gw prune branches [--fetch] [--gone] --json`; `--yes` deletes | — | `--yes` deletes merged branches in source clones; `--gone` force-deletes, may drop unmerged commits |

`gw diff` runs `git diff` in every repo (sorted by name) and concatenates the output with paths prefixed `a/<repo>/` and `b/<repo>/`, so it pipes into delta or `git apply` like one monorepo diff. Exit code is non-zero if any repo fails, and with `--exit-code`/`--quiet` if any repo has differences. Limitations: `--stat`/`--shortstat` print one block per repo; `--name-only`, `--name-status`, `--numstat` and `--raw` paths lack the repo prefix; pathspecs go to every repo; no cross-repo rename detection, and SHAs resolve only in their own repo; external diff tools (`GIT_EXTERNAL_DIFF`) label files without the prefix.

`NAME` in brackets is detected from the current directory when you are inside a workspace.

## Setup and config

| Command | Form | Notes |
|---|---|---|
| `gw init` | `gw init DIR...` | Dirs must exist. Required before anything else. Idempotent. |
| `gw add-dir` / `gw remove-dir` | `gw add-dir PATH`, `gw remove-dir PATH` | `remove-dir` without PATH → picker |
| `gw repos` | `gw repos -o name\|json` | Discovered repos and remotes |
| `gw preset add` | `gw preset add NAME -r a,b` | No `-r` → picker |
| `gw preset list` / `show` / `remove` | `gw preset list --json`, `gw preset show NAME --json`, `gw preset remove NAME` | `remove` without NAME → picker |
| `gw doctor` | `gw doctor --json`; `--fix` repairs | `--json` prints `null` (not `[]`) when healthy. `--fix` removes stale state and trash; for a workspace whose directory is gone it also prunes worktree registrations and force-deletes the workspace branch, like `gw delete`. |
| `gw shell-init` | `gw shell-init [--shell nu]` | For the user's rc file only; agents never need it |
| `gw plugin` | `install OWNER/REPO`, `list --json`, `search [term] --json`, `upgrade [name]`, `remove NAME` | No prompts |
| `gw stats` | `gw stats` | Human-only output |
| `gw bug-report` | `gw bug-report --print` | Without a TTY it prints instead of opening a browser |

## Output formats

`-o`/`--output` on `list`, `ws list`, `ws show`, `status`, `repos`: `table` (default, do not parse), `json`, `jsonl`, `tsv`, `name`, `path`. `-j`/`--json` is an alias for `-o json`.

`--json` only (no `-o`): `doctor`, `prune`, `prune branches`, `preset list`, `preset show`, `plugin list`, `plugin search`.

Human messages (`ok:`, `warn:`, `error:`, progress) go to stderr. `gw create` writes nothing to stdout; read the result with `gw ws show NAME -o json`.

## JSON fields

Workspace (`ws show`, `list`):

```json
{"name": "fix-auth", "path": "...", "branch": "fix/auth-expiry", "created_at": "...",
 "repos": [{"repo_name": "api", "source_repo": "...", "worktree_path": "...", "branch": "fix/auth-expiry"}]}
```

Status (`status -o jsonl`, one line per repo):

```json
{"workspace": "fix-auth", "repo": "api", "path": "...", "branch": "fix/auth-expiry",
 "status": "clean", "changed": 0, "ahead": "-", "behind": "-"}
```

`changed` is a number. `ahead` and `behind` are **strings**; `"-"` means no upstream (the branch was never pushed). `pr` appears with `-P`.
