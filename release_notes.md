## What's New

### `gw stats` — usage statistics with contribution heatmap
Track your workspace activity over time. Shows a GitHub-style 52-week heatmap, summary metrics (total created, active, avg lifetime), and most-used repos. Data is stored locally in `~/.grove/stats.json`.

### `gw list <name>` — workspace detail view
Pass a workspace name to `gw list` to see full details: branch, path, creation date, and a table of each repo with its worktree and source paths.

### Performance improvements
- Lazy `__version__` loading saves ~15ms on every invocation
- Branch-exists checks during `gw create` now run in parallel (saves 200-500ms with multiple repos)
- PR status fetch in `gw status --pr` now runs in parallel (saves seconds with multiple repos)

### Bug fixes
- Exceptions in non-critical paths (stats recording, PR fetch, version lookup) are now logged to `~/.grove/grove.log` instead of silently suppressed
