## What's New

### `--json` flag for `list`, `status`, and `doctor`
Machine-readable JSON output via `-j`/`--json` on the commands that produce tabular data:
- `gw list -j` — all workspaces as JSON array
- `gw list <name> -j` — single workspace detail
- `gw list -sj` — workspace summaries with git status
- `gw status -j` — repo status for current workspace
- `gw doctor -j` — health issues as JSON array
