# Go Rewrite — Remaining Work

## High Priority

### Discovery remote URL cache + dedup
The deep scan (`gw explore`) doesn't resolve remote URLs or deduplicate by them.
Python's `discover_repos` has a 3-phase process:
1. Filesystem scan (done)
2. Batch remote URL resolution with 16-thread parallelism + disk cache at
   `~/.grove/cache/remotes.json` (mtime + 24h TTL invalidation)
3. Dedup by remote URL, preferring direct children over nested repos

This matters when the same repo is cloned in multiple configured directories.
The shallow scan (`FindRepos`) handles this with first-occurrence-wins by folder
name, which covers workspace operations. But `gw explore` will show duplicates.

Implementing properly requires:
- Goroutine pool for parallel `git remote get-url`
- Disk cache with mtime-based invalidation (`.git/config` changed = miss)
- 24h TTL per entry
- Atomic cache file writes
- Preference logic (direct children win over nested)

~200 lines of concurrent code. Worth doing right.

### ~~`gw go --delete` should spawn a detached subprocess~~ DONE
Fixed: uses `exec.Command` with `Setpgid: true` + `cmd.Start()` (no Wait).

## Medium Priority

### `gw dash` TUI
The Kanban agent monitoring dashboard. Planned as a separate plugin/binary
to keep the core lean. Needs:
- Bubble Tea TUI with Kanban columns (Active, Attention, Idle, Done)
- State scanner (glob `~/.grove/status/*.json`)
- Stale session cleanup (dead PID detection)
- Zellij tab jumping + approve/deny

### `gw run` split-pane TUI
Currently runs hooks inline with prefixed output. Python version has a
split-pane Textual TUI with sidebar (repo list + status dots) and log pane.
Current inline behavior is functional. TUI is nice-to-have.

## Low Priority

### Zellij integration module
Tab navigation, text input, approve/deny. Only needed for `gw dash` and
`gw go --close-tab` (which has a basic implementation now).

### Generic parallel helper
Each operation (sync, status, create, delete) reimplements its own
`sync.WaitGroup` + index-based result array pattern. Could extract a shared
`parallel[T]()` generic helper. Not blocking — current code works.

### Spinner/progress indicator
No visual feedback during long parallel operations (multi-repo fetch, sync).
Python shows a Rich spinner. Could use a simple stderr spinner.
