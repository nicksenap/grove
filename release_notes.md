## What's New

### `gw go` workspace switcher with back-to-source
The interactive picker now shows the current workspace labeled `(current)` and adds a `← back to repos dir` option when you're inside a workspace.

### Fix: `gw go` interactive mode now works
Fixed stdout pollution that prevented the shell function from capturing the workspace path. Diagnostic output (errors, warnings, prompts) now goes to stderr; only the path goes to stdout. The `simple-term-menu` title renders directly to the terminal, bypassing stdout entirely.

### All previous 0.4.x features
Arrow-key selection, preset picker in `gw create`, fetch & branch from default branch, CLAUDE.md workspace copy, version update check, and auto-generated Homebrew formula.
