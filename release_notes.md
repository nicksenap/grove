## Kanban Dashboard

`gw dash` is now an interactive kanban board with task cards, replacing the read-only agent table.

### New features
- **Kanban board** with 5 columns: Planned, Active, Attention, Idle, Done
- **Task cards**: create (`c`), edit (`e`), mark done (`d`), delete (`x`) with SQLite persistence
- **Task launch** (alpha): press `s` on a planned card to provision a workspace, open a Zellij tab, and start Claude with the task prompt
- **Workspace cleanup**: deleting a started task also removes the linked workspace
- **Search**: `/` filters both live agents and task cards by title, branch, description, status
- **Navigation**: `h/l` switches columns, `j/k` navigates cards

### Known issues
The launch flow (`s`) is experimental alpha — trust prompt timing, tab focus, and setup script output still need polish.

## Upgrading

```bash
brew upgrade grove
```
