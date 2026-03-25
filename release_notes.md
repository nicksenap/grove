## What's New

### `gw go -c` — close Zellij pane/tab when leaving
Close the current Zellij pane when navigating away from a workspace. If it's the last pane in the tab, the tab closes too. Combine with `-d` to delete the workspace: `gw go -dc`.

### Internal
- Moved `zellij` module from `grove.dash` to top-level `grove.zellij` since it's used outside the dashboard
