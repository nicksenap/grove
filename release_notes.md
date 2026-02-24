## What's New

### Per-repo `.grove.toml` config
Repos can now have a `.grove.toml` at their root to control per-repo behavior:

```toml
# merchant-portal/.grove.toml
base_branch = "stage"
setup = "pnpm install"
```

- **`base_branch`** — branch new worktrees from `origin/<base_branch>` instead of auto-detecting
- **`setup`** — command(s) to run after worktree creation. Accepts a string or list of strings. Failures warn but don't block workspace creation.

### `gw go` workspace switcher with back-to-source
The interactive picker now labels the current workspace `(current)` and adds a `← back to repos dir` option when invoked from inside a workspace.

### Fix: `gw go` interactive mode
Fixed stdout pollution that prevented the shell function from doing `cd`. Diagnostic output now goes to stderr; only the workspace path goes to stdout.
