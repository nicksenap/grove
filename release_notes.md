## What's New

### Dashboard focused on monitoring

The dashboard (`gw dash`) has been stripped down to a pure agent monitor. Removed the SQLite task store, task cards, launch flow, and planning features. The kanban board now has 4 columns — Active, Attention, Idle, Done — driven entirely by live agent state files.

Also removed `gw dash list` and `gw dash status` subcommands. Use `gw dash` for the full TUI.

### Automatic branch cleanup on workspace deletion

`gw delete` now automatically deletes the local branches it created when removing workspaces. Uses safe mode (`git branch -d`) so branches with unmerged commits are preserved with a warning.

#### Cleaning up branches from older versions

Branches created by Grove versions before 0.12.5 won't be cleaned up automatically. To find and remove them manually:

```sh
# Preview stale branches across your repos (dry run)
for repo in ~/dev/repos/*/; do
  echo "=== $(basename $repo) ==="
  git -C "$repo" branch --merged | grep -v '^\*\|main\|master\|stage'
done

# Then delete them (remove --dry-run when ready)
for repo in ~/dev/repos/*/; do
  git -C "$repo" branch --merged | grep -v '^\*\|main\|master\|stage' | xargs -r git -C "$repo" branch -d
done
```
