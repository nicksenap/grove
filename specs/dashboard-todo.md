# Dashboard TODO

## Future improvements

- **`write_chars_to_pane_id`** — Zellij plugin API supports targeting specific panes by ID. This would allow approve/deny without switching focus when multiple panes share a tab. Requires writing a Zellij plugin (Rust/WASM).

- **Session name resolution** — Resolve human-readable names from `~/.claude/projects/<hash>/<session_id>.jsonl` (custom titles from `/rename`) and `~/.claude/history.jsonl` (initial prompt).

- **Usage tracking** — Parse Claude's JSONL logs to show token costs and burn rate per agent.

- **Rules engine** — YAML config for auto-approve/deny rules (YOLO mode with deny overrides for dangerous commands).

- **tmux fallback** — Add tmux backend alongside Zellij for users who prefer it.

- ~~**Workspace correlation** — Match agent `cwd` to Grove worktrees to show workspace name alongside agent info.~~ ✅ Done

- **Rethink `gw run`** — Now that we have hooks and agent state via `~/.grove/status/`, `gw run` could become reactive: auto-start services when agents begin working, wind down on idle, restart on errors. The state files could serve as a shared bus between `gw run` (act) and `gw dash` (observe). Needs a proper design pass — the current `gw run` is a simple process runner, the future version could be a workspace runtime.

- **Event log widget** — RichLog panel showing timestamped status transitions and tool calls.

- **macOS notifications** — Optional `osascript` notifications for compaction events and agents needing attention.
