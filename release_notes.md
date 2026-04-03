## What's New

### Graceful Ctrl+C during workspace creation

`gw create` now cleans up properly when interrupted with Ctrl+C:

- Partial worktrees are rolled back automatically
- Orphan branches (created but never attached to a worktree) are deleted
- The workspace directory is removed so it doesn't leave a mess behind
- Setup hooks phase is also covered — interrupt at any point during create and rollback kicks in

### Doctor detects orphaned workspace directories

`gw doctor` now catches workspace directories that exist on disk but aren't tracked in state — exactly the kind of debris left by a hard kill (SIGKILL, power loss) where cleanup can't run:

- `gw doctor` flags them as "orphaned workspace directory"
- `gw doctor --fix` removes them
- Hidden directories (`.`-prefixed) are ignored to avoid false positives
- Symlinks that resolve outside the workspace directory are safely skipped

### Safety improvements

- `shutil.rmtree` failures during rollback now log a warning instead of being silently ignored
- `doctor --fix` refuses to follow symlinks, preventing accidental deletion of targets outside the workspace directory
