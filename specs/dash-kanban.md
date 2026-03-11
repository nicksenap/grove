# Grove Dashboard v2 — Kanban Orchestration Board

## Vision

Transform `gw dash` from a read-only monitoring table into an interactive
kanban board that can **create tasks, launch workspaces, start Claude agents,
and monitor them** — all from one screen.

The CLI (`gw create`, `gw ws`, `gw list`) remains terminal-agnostic.
The dashboard is the opinionated "Zellij power-user" orchestration layer on top.

---

## Card Lifecycle

```
┌──────────┐     ┌──────────────┐     ┌─────────┐     ┌──────────┐     ┌──────┐
│ PLANNED  │────▸│ PROVISIONING │────▸│ WORKING │────▸│  IDLE    │────▸│ DONE │
└──────────┘     └──────────────┘     └─────────┘     └──────────┘     └──────┘
     │                                     │               │
     │                                     ▼               │
     │                              ┌─────────────┐       │
     │                              │   WAITING    │       │
     │                              │ PERM/ANSWER  │       │
     │                              └─────────────┘       │
     │                                     │               │
     │                                     ▼               │
     │                              ┌─────────────┐       │
     │                              │    ERROR     │───────┘
     │                              └─────────────┘
     │
     ▼
 ┌──────────┐
 │ ARCHIVED │  (user dismisses without launching)
 └──────────┘
```

**PLANNED** → User creates a card with a prompt/description.
**PROVISIONING** → Dashboard calls `gw ws create` + launches Claude in a Zellij tab.
**WORKING/WAITING/ERROR** → Hooks feed status back; card updates automatically.
**IDLE** → Agent finished its turn, waiting for user input.
**DONE** → User marks card as done (or auto-detect from SessionEnd hook).
**ARCHIVED** → User dismisses a planned card without launching.

---

## Kanban Columns

Default columns (status-based, auto-sorted):

| Column | Statuses | Description |
|--------|----------|-------------|
| Planned | PLANNED | Tasks waiting to be launched |
| Active | WORKING, PROVISIONING | Agents currently doing work |
| Needs Attention | WAITING_PERMISSION, WAITING_ANSWER, ERROR | Requires user action |
| Idle | IDLE | Finished their turn |
| Done | DONE, ARCHIVED | Completed or dismissed |

Cards flow between columns automatically based on hook events.
Users can manually move cards to DONE or ARCHIVED.

---

## Card Design

```
╭─ feat/purchase-api ──────────── ⚡ WORKING ─╮
│ Add bulk import endpoint for purchases       │
│                                              │
│ 🔧 Edit  ·  tools: 23  ·  3m                │
│ ▁▂▃▅▇▅▃▂▁▃  main ← feat/purchase-api       │
╰──────────────────────────────────────────────╯
```

Card shows:
- **Title**: workspace/branch name
- **Status badge**: colored by status
- **Prompt snippet**: first line of the task description
- **Last tool + counts**: what the agent is doing
- **Sparkline + branch**: activity and git context

Focused card expands to show more detail (or detail pane on the right).

---

## Launch Flow

### User creates a card

1. Press `c` (create) on the dashboard
2. Input dialog: branch name, prompt/description, repo selection
3. Card appears in PLANNED column

### User launches a card

1. Focus a PLANNED card, press `enter` (launch)
2. Dashboard executes:
   ```
   gw ws create <branch> --repos <repos>
   ```
3. Dashboard generates a temp Zellij layout file:
   ```kdl
   layout {
     tab name="<branch>" {
       pane command="claude" args="--dangerously-skip-permissions" {
         // or without --dangerously-skip-permissions based on config
       }
     }
   }
   ```
4. Dashboard executes:
   ```
   zellij action new-tab --layout /tmp/grove-launch-<id>.kdl --cwd <workspace-path>
   ```
5. Claude starts, hooks fire, card moves to WORKING

### Permissions config

In `~/.grove/config.toml`:
```toml
[dash]
# Permission mode for launched agents
# Options: "default", "dangerously-skip-permissions"
claude_permissions = "default"

# Default model
claude_model = ""

# Extra CLI flags
claude_extra_args = ""
```

---

## Data Storage

### Hybrid approach: Files + SQLite

| Data | Storage | Why |
|------|---------|-----|
| Agent hook state | JSON files (`~/.grove/status/`) | Hot path, written every second by hooks, atomic writes |
| Task cards | SQLite (`~/.grove/tasks.db`) | Persistent, queryable, survives agent restarts |
| Config | TOML (`~/.grove/config.toml`) | Existing Grove config |

### SQLite Schema

```sql
CREATE TABLE tasks (
    id          TEXT PRIMARY KEY,  -- uuid
    title       TEXT NOT NULL,     -- branch name or user title
    description TEXT DEFAULT '',   -- prompt / task description
    status      TEXT DEFAULT 'PLANNED',
    branch      TEXT DEFAULT '',
    repos       TEXT DEFAULT '[]', -- JSON array of repo names
    workspace   TEXT DEFAULT '',   -- workspace name (after creation)
    session_id  TEXT DEFAULT '',   -- linked Claude session (from hooks)
    column_order INTEGER DEFAULT 0, -- position within column
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL,
    launched_at TEXT,
    completed_at TEXT,
    config      TEXT DEFAULT '{}' -- JSON: permissions, model, extra args
);

CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_session ON tasks(session_id);
```

### Linking tasks to agents

When a card is launched:
1. `gw ws create` returns the workspace path
2. Claude starts in that workspace
3. The SessionStart hook fires with the workspace's `cwd`
4. Dashboard matches the card's workspace path to the agent's `cwd`
5. Card's `session_id` is set, linking task → agent

---

## Keyboard Bindings

| Key | Action |
|-----|--------|
| `h` / `l` | Move focus between columns |
| `j` / `k` | Move focus between cards in column |
| `c` | Create new card |
| `enter` | Launch planned card / Jump to agent tab |
| `y` / `n` | Approve / Deny permission (on WAITING cards) |
| `d` | Mark as done |
| `x` | Archive / Delete card |
| `e` | Edit card description |
| `/` | Search / filter cards |
| `r` | Refresh |
| `q` | Quit |

---

## Textual Architecture

```
DashboardApp
├── HeaderBar                    (existing, updated)
├── HorizontalScroll             (kanban board)
│   ├── KanbanColumn "Planned"
│   │   └── VerticalScroll
│   │       ├── TaskCard
│   │       └── TaskCard
│   ├── KanbanColumn "Active"
│   │   └── VerticalScroll
│   │       ├── TaskCard (linked to AgentState)
│   │       └── TaskCard
│   ├── KanbanColumn "Attention"
│   │   └── VerticalScroll
│   │       └── TaskCard
│   ├── KanbanColumn "Idle"
│   │   └── VerticalScroll
│   │       └── TaskCard
│   └── KanbanColumn "Done"
│       └── VerticalScroll
│           └── TaskCard
├── AgentDetail                  (existing, shown for focused card)
└── StatusLine                   (existing)
```

### Key widgets

- **TaskCard** — extends `Static`, renders card content with Rich markup.
  Has a `task_id` and optionally a linked `AgentState`.
- **KanbanColumn** — `Vertical` container with a title and `VerticalScroll` body.
  Independent scrolling per column.
- **CardEditor** — modal dialog for creating/editing cards.

### Data flow

```
SQLite (tasks) ──┐
                 ├──▸ merge ──▸ TaskCards ──▸ KanbanColumns
JSON files (agents) ─┘
```

On each poll tick:
1. Read all tasks from SQLite
2. Read all agent states from JSON files (existing `manager.scan()`)
3. Match tasks to agents by `session_id` or workspace path
4. Unlinked agents (started outside dash) appear as "ad-hoc" cards in Active
5. Update card widgets in-place (mount new, remove completed)

---

## Zellij Integration

### Capabilities (verified)

| Operation | Zellij Support | Command |
|-----------|---------------|---------|
| Create tab with name + cwd | ✅ Full | `zellij action new-tab --name X --cwd Y` |
| Run command in new tab | ⚠️ Via layout file | Temp `.kdl` with `pane command=...` |
| Switch to tab by name | ✅ Full | `zellij action go-to-tab-name X` |
| Send keys to pane | ✅ Full | `zellij action write-chars --pane-id N` |
| Close focused tab | ✅ Full | `zellij action close-tab` |
| Target session externally | ✅ Full | `zellij -s <session> action ...` |
| Query focused tab | ⚠️ Indirect | `dump-layout` parsing |
| Close tab by name | ❌ Not yet | Must focus first, then close |

### Launch implementation

```python
def launch_card(task: Task, workspace_path: str) -> bool:
    """Launch a Claude agent in a new Zellij tab."""
    # Generate temp layout
    layout = f'''layout {{
  tab name="{task.branch}" {{
    pane command="claude" args="{_claude_args(task)}"
  }}
}}'''
    layout_path = Path(tempfile.mktemp(suffix=".kdl", prefix="grove-"))
    layout_path.write_text(layout)

    try:
        result = subprocess.run(
            ["zellij", "action", "new-tab",
             "--layout", str(layout_path),
             "--cwd", workspace_path],
            capture_output=True, timeout=10,
        )
        return result.returncode == 0
    finally:
        layout_path.unlink(missing_ok=True)
```

---

## Migration from v1

The current table-based dash becomes a "compact view" toggle (`t` key).
Default view is the kanban board. Users who prefer the table can switch.

Existing features preserved:
- Hook system (unchanged)
- Agent state files (unchanged)
- Search/filter
- Approve/deny
- Jump to tab

New features:
- Task cards with persistence
- Launch from dashboard
- Card lifecycle tracking

---

## Implementation Phases

### Phase 1 — Kanban layout (visual only)
- Replace DataTable with KanbanColumn + TaskCard widgets
- Auto-sort existing agents into columns by status
- Keyboard navigation (h/j/k/l)
- Keep detail panel

### Phase 2 — Task persistence (SQLite)
- Add `tasks.db` with schema
- Create/edit/delete cards
- Cards persist across dashboard restarts
- Link cards to agents when launched externally

### Phase 3 — Launch from dashboard
- `gw ws create` integration (shell out)
- Zellij tab creation with temp layout
- Claude auto-start with configurable permissions
- Full PLANNED → WORKING lifecycle

### Phase 4 — Polish
- Compact table view toggle
- Card drag reordering (mouse)
- Notifications (desktop or Zellij)
- Multi-session support (multiple Zellij sessions)

---

## Open Questions

1. **Should ad-hoc agents (started outside dash) auto-create task cards?**
   Or stay as ephemeral entries that disappear on SessionEnd?

2. **How to handle `gw ws create` failure?**
   Show error on card? Move to ERROR status?

3. **Should DONE cards auto-archive after N hours?**
   Or require manual cleanup?

4. **Claude prompt injection**: When launching Claude with a prompt from
   the card, how to pass it? Pipe to stdin? `--prompt` flag?
   Need to investigate Claude Code CLI flags.

5. **Multiple agents per workspace**: A card could spawn multiple agents
   (e.g., one per repo). How to represent this? Stacked cards?
