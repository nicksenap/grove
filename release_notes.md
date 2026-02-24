## What's New

### Per-repo `.grove.toml` config
Repos can now have a `.grove.toml` at their root to override defaults. Currently supports `base_branch` — useful when a repo's working branch isn't `main`:

```toml
# merchant-portal/.grove.toml
base_branch = "stage"
```

When `gw create` makes a new branch in this repo, it will branch from `origin/stage` instead of auto-detecting the default branch.

### `gw go` workspace switcher with back-to-source
The interactive picker now labels the current workspace `(current)` and adds a `← back to repos dir` option when invoked from inside a workspace.

### Fix: `gw go` interactive mode
Fixed stdout pollution that prevented the shell function from doing `cd`. Diagnostic output now goes to stderr; only the workspace path goes to stdout.
