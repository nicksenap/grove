## What's New

### Arrow-key interactive selection
Replaced number-entry prompts with arrow-key navigation menus (powered by `simple-term-menu`). Single-select uses arrow + enter; multi-select uses arrow + space + enter, with an `(all)` option.

### Preset picker in `gw create`
When presets exist, the interactive flow now offers them as choices before falling back to manual repo selection.

### Fresh fetch & branch from default branch
`gw create` now runs `git fetch --all` on each repo before creating worktrees, and new branches are based on the remote default branch (`origin/main` or `origin/master`) instead of whatever HEAD happens to be.

### CLAUDE.md workspace copy
If a `CLAUDE.md` exists in your repos directory, `gw create` offers to copy it into the new workspace (defaults to yes).

### Version update check
On startup, grove checks for newer releases via the GitHub API (cached for 24h, non-blocking background refresh). Shows a one-line warning when an update is available.

### Auto-generated Homebrew formula
The release workflow now auto-generates the full Homebrew formula with all Python resource blocks, so new dependencies are picked up automatically.
