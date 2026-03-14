## What's New

### Claude Code memory sync for worktrees

Grove can now sync Claude Code's per-project memory between source repos and their worktrees. Context built during a worktree session is preserved when the worktree is deleted, and rehydrated when a new worktree is created.

**Opt-in** — add to `~/.grove/config.toml`:

```toml
claude_memory_sync = true
```

- **Rehydrate**: On `gw new` / `gw add-repo`, memory files are copied from the source repo's Claude memory into the new worktree
- **Harvest**: On `gw delete` / `gw remove-repo`, new or updated memory files are merged back into the source repo
- **Rename**: On `gw rename`, Claude memory directories are migrated to the new path
- **Doctor**: `gw doctor` detects orphaned Claude memory directories for worktrees that no longer exist; `--fix` cleans them up
