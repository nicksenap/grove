"""Session list widget — DataTable of agent sessions."""

from __future__ import annotations

from rich.text import Text
from textual.widgets import DataTable

from grove.dash.constants import AgentStatus
from grove.dash.models import AgentState

# Gruvbox dark palette
_GREEN = "#b8bb26"
_AQUA = "#8ec07c"
_RED = "#fb4934"
_YELLOW = "#fabd2f"
_GREY = "#928374"
_ORANGE = "#fe8019"

_STATUS_DISPLAY: dict[AgentStatus, tuple[str, str, str]] = {
    AgentStatus.WORKING: (">>>", "WORK", _GREEN),
    AgentStatus.IDLE: ("---", "IDLE", _GREY),
    AgentStatus.WAITING_PERMISSION: ("[!]", "PERM", _RED),
    AgentStatus.WAITING_ANSWER: ("[?]", "WAIT", _YELLOW),
    AgentStatus.ERROR: ("[X]", "ERR", _ORANGE),
}


def _styled(text: str, color: str) -> Text:
    return Text(text, style=color)


def _status_cell(status: AgentStatus) -> Text:
    sym, _label, color = _STATUS_DISPLAY.get(status, ("???", "?", _GREY))
    return Text(sym, style=color)


def _label_cell(status: AgentStatus) -> Text:
    _sym, label, color = _STATUS_DISPLAY.get(status, ("???", "?", _GREY))
    return Text(label, style=color)


def _idle_str(seconds: float) -> str:
    if seconds < 5:
        return ""
    if seconds < 60:
        return f"{int(seconds)}s"
    if seconds < 3600:
        return f"{int(seconds // 60)}m"
    return f"{int(seconds // 3600)}h"


def _row_cells(agent: AgentState) -> tuple:
    """Build a tuple of Rich Text cells for a DataTable row."""
    name = agent.display_name or agent.session_id[:12]
    branch = agent.git_branch or ""
    tool = agent.last_tool or ""
    age = _idle_str(agent.idle_seconds)
    spark = agent.sparkline
    subs = f"+{agent.subagent_count}" if agent.subagent_count > 0 else ""

    return (
        _status_cell(agent.status),
        _label_cell(agent.status),
        Text(name),
        _styled(branch, _GREY),
        Text(tool),
        _styled(age, _GREY),
        _styled(spark, _GREEN),
        _styled(subs, _AQUA) if subs else Text(""),
    )


def matches_filter(agent: AgentState, query: str) -> bool:
    """Check if an agent matches a search query."""
    q = query.lower()
    return (
        q in (agent.display_name or agent.session_id).lower()
        or q in (agent.git_branch or "").lower()
        or q in (agent.last_tool or "").lower()
        or q in (agent.cwd or "").lower()
        or q in agent.status.value.lower()
    )


class SessionList(DataTable):
    """DataTable of agent sessions."""

    _agent_ids: list[str] = []

    def on_mount(self) -> None:
        self.cursor_type = "row"
        self.zebra_stripes = True
        self.add_columns("", "Status", "Workspace", "Branch", "Tool", "Age", "Activity", "Sub")

    def update_agents(self, agents: list[AgentState]) -> None:
        """Update the table, preserving selection when possible."""
        new_ids = [a.session_id for a in agents]

        if new_ids == self._agent_ids:
            # Same agents — update cells in-place
            for i, agent in enumerate(agents):
                row_key = self._agent_ids[i]
                cells = _row_cells(agent)
                col_keys = list(self.columns.keys())
                for j, cell in enumerate(cells):
                    self.update_cell(row_key, col_keys[j], cell)
        else:
            # Structural change — rebuild
            saved = self.cursor_row
            self.clear()
            self._agent_ids = new_ids
            for agent in agents:
                self.add_row(*_row_cells(agent), key=agent.session_id)
            if agents and saved is not None:
                self.move_cursor(row=min(saved, len(agents) - 1))

    @property
    def selected_index(self) -> int | None:
        """Return the currently highlighted row index, or None."""
        if self.row_count == 0:
            return None
        return self.cursor_row
