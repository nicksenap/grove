# Grove → Go Rewrite

Full rewrite of the Grove CLI (`gw`) from Python to Go. Lives on the `rewrite/go` branch — Python `master` stays untouched until Go reaches feature parity.

## Why

- Single static binary (no Python/virtualenv)
- Faster startup (~5ms vs ~200ms)
- Simpler Homebrew distribution (no `pip` resources)

## Status

### Working (CLI core)

| Command | Status | Notes |
|---------|--------|-------|
| `gw init` | Done | Merges with existing config |
| `gw add-dir` / `remove-dir` | Done | |
| `gw explore` | Done | Styled output with bold dirs |
| `gw new` / `create` | Done | Presets, auto-name, GROVE_CD_FILE, CLAUDE.md copy |
| `gw list` / `ls` | Done | Rich table with borders, Created column |
| `gw delete` / `rm` | Done | Multi-select, batch confirmation |
| `gw rename` | Done | With worktree repair + Claude memory migration |
| `gw add-repo` / `remove-repo` | Done | Interactive pickers |
| `gw status` | Done | Colored drift (↑↓), status, PR column |
| `gw sync` | Done | Colored per-repo output (✓/warn/error) |
| `gw run` | Done | Bubbletea TUI with sidebar + log panel |
| `gw cd` / `go` | Done | `--back`, `--delete`, `← back to repos dir` |
| `gw doctor` | Done | Table output with suggested actions |
| `gw config show` | Done | |
| `gw config preset *` | Done | add/list/remove with tables |
| `gw shell-init` | Done | GROVE_CD_FILE-based shell function |
| `gw version` | Done | |

### Partial

| Feature | Status | What's missing |
|---------|--------|----------------|
| `gw dash` | Basic | Split-pane workspace viewer only. Missing: kanban board, agent cards, task management, hook system, Zellij integration, Claude usage display, search/filter |
| `gw dash install/uninstall` | Stub | Hook installer not yet ported |
| `gw dash status/list` | Stub | Agent state scanner not yet ported |
| `gw _hook` | Stub | Event handler not yet ported |
| Interactive pickers | Partial | `huh` library works but lacks the type-to-search filtering that `simple-term-menu` provides in Python |

### Not started

- **Dashboard kanban board** — 5-column layout (planned/active/attention/idle/done) with task cards and agent cards. This is the largest remaining piece (~2000 lines in Python).
- **Hook system** — Event handler that receives Claude Code events via stdin JSON and writes atomic state files to `~/.grove/status/`. Includes tool summary formatting, activity sparklines, and session lifecycle management.
- **Task store** — SQLite-backed task persistence with CRUD ops, kanban column ordering.
- **Agent state manager** — Scans `~/.grove/status/*.json`, enriches with workspace info, deduplicates by PID, cleans stale entries.
- **Dashboard widgets** — Header bar with Claude usage tracker, agent detail panel with sparklines, task/confirm modals, kanban columns with focus navigation.
- **Zellij integration** — Tab jumping, approve/deny permission requests, new tab creation with `claude` launch.
- **Tab completion** — Cobra completion is available but custom completers for workspace/repo/preset names not wired up.

## Architecture

```
grove-go/
├── cmd/gw/main.go              # Cobra commands (~1450 lines)
├── internal/
│   ├── models/models.go        # Config, Workspace, RepoWorktree structs
│   ├── config/config.go        # TOML config + legacy migration
│   ├── state/state.go          # JSON state with atomic writes
│   ├── git/git.go              # Git subprocess wrapper + .grove.toml cache
│   ├── discover/discover.go    # Repo discovery with remote URL caching
│   ├── workspace/workspace.go  # Worktree orchestration (create/delete/sync/diagnose)
│   ├── claude/claude.go        # Claude Code memory sync
│   ├── update/update.go        # Background version check (GitHub API)
│   ├── logging/logging.go      # File logging with lumberjack rotation
│   ├── console/console.go      # Lipgloss styles + bordered table renderer
│   ├── picker/picker.go        # Interactive menus (huh library)
│   ├── runner/runner.go        # Bubbletea TUI for `gw run`
│   └── dash/                   # Dashboard TUI (basic, needs major work)
│       ├── app.go
│       └── styles.go
├── go.mod / go.sum
├── justfile
└── .goreleaser.yml
```

## Development

```bash
# Build
just build

# Run tests
just test        # or: just test-v for verbose

# Full check (lint + format + tests)
just check

# Install globally (needs ~/go/bin in PATH)
just dev-global

# Remove Go version, restore Python/Homebrew gw
just undev
```

## Dependencies

| Purpose | Package |
|---------|---------|
| CLI framework | `github.com/spf13/cobra` |
| TOML config | `github.com/BurntSushi/toml` |
| Terminal styling | `github.com/charmbracelet/lipgloss` |
| TUI framework | `github.com/charmbracelet/bubbletea` |
| Interactive forms | `github.com/charmbracelet/huh` |
| Log rotation | `gopkg.in/natefinch/lumberjack.v2` |

## Estimated remaining work

| Area | Est. lines | Priority |
|------|-----------|----------|
| Dashboard kanban + widgets | ~2000 | High |
| Hook system + state manager | ~800 | High (blocks dashboard) |
| Task store (SQLite) | ~300 | High (blocks dashboard) |
| Zellij integration | ~200 | Medium |
| Picker type-to-search | ~100 | Medium |
| Tab completion | ~50 | Low |
| Tests for new code | ~1500 | Medium |
