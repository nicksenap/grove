"""TaskCard widget — a kanban card for an agent session or a planned task."""

from __future__ import annotations

from textual.widgets import Static

from grove.dash.constants import (
    AQUA,
    FG,
    GREEN,
    GREY,
    ORANGE,
    PURPLE,
    RED,
    STATUS_DISPLAY,
    AgentStatus,
)
from grove.dash.models import AgentState
from grove.dash.store import Task


def _idle_ago(seconds: float) -> str:
    if seconds < 5:
        return ""
    if seconds < 60:
        return f"{int(seconds)}s"
    if seconds < 3600:
        return f"{int(seconds // 60)}m"
    return f"{int(seconds // 3600)}h"


class TaskCard(Static, can_focus=True):
    """A kanban card for a single agent session or planned task."""

    DEFAULT_CSS = """
    TaskCard {
        width: 100%;
        height: auto;
        min-height: 3;
        padding: 0 1;
        margin: 0;
        border: round $panel;
    }

    TaskCard:focus {
        border: round $primary;
    }

    TaskCard.status-working {
        border-left: outer #b8bb26;
    }
    TaskCard.status-waiting-permission {
        border-left: outer #fb4934;
    }
    TaskCard.status-waiting-answer {
        border-left: outer #fabd2f;
    }
    TaskCard.status-error {
        border-left: outer #fe8019;
    }
    TaskCard.status-idle {
        border-left: outer #928374;
    }
    TaskCard.status-planned {
        border-left: outer #83a598;
    }
    TaskCard.status-provisioning {
        border-left: outer #8ec07c;
    }
    TaskCard.status-done {
        border-left: outer #b8bb26;
    }

    TaskCard:focus.status-working {
        border: round #b8bb26;
        border-left: outer #b8bb26;
    }
    TaskCard:focus.status-waiting-permission {
        border: round #fb4934;
        border-left: outer #fb4934;
    }
    TaskCard:focus.status-waiting-answer {
        border: round #fabd2f;
        border-left: outer #fabd2f;
    }
    TaskCard:focus.status-error {
        border: round #fe8019;
        border-left: outer #fe8019;
    }
    TaskCard:focus.status-planned {
        border: round #83a598;
        border-left: outer #83a598;
    }
    """

    def __init__(
        self,
        agent: AgentState | None = None,
        task_data: Task | None = None,
        **kwargs: object,
    ) -> None:
        super().__init__(**kwargs)
        self.agent = agent
        self.task_data = task_data
        self._apply_status_class()

    @property
    def status(self) -> AgentStatus:
        if self.agent:
            return self.agent.status
        if self.task_data:
            return self.task_data.status
        return AgentStatus.IDLE

    @property
    def card_id(self) -> str:
        """Unique ID for this card (agent session_id or task id)."""
        if self.agent:
            return self.agent.session_id
        if self.task_data:
            return self.task_data.id
        return ""

    def _apply_status_class(self) -> None:
        """Set CSS class based on status."""
        for cls in list(self.classes):
            if cls.startswith("status-"):
                self.remove_class(cls)
        css_status = self.status.value.lower().replace("_", "-")
        self.add_class(f"status-{css_status}")

    def update_agent(self, agent: AgentState) -> None:
        """Update the card with new agent state."""
        self.agent = agent
        self._apply_status_class()
        self.update(self._render_card())

    def update_task(self, task_data: Task) -> None:
        """Update the card with new task data."""
        self.task_data = task_data
        self._apply_status_class()
        self.update(self._render_card())

    def on_mount(self) -> None:
        self.update(self._render_card())

    def _render_card(self) -> str:
        if self.agent:
            return self._render_agent_card()
        if self.task_data:
            return self._render_task_card()
        return f"[{GREY}]Empty card[/]"

    def _render_agent_card(self) -> str:
        a = self.agent
        assert a is not None
        color, label = STATUS_DISPLAY.get(a.status, (GREY, "?"))

        # Line 1: Name + status badge
        name = a.display_name or a.session_id[:12]
        lines = [f"[bold {FG}]{name}[/]  [{color}]{label}[/]"]

        # Line 2: Branch + tool info
        parts: list[str] = []
        if a.git_branch:
            parts.append(f"[{AQUA}]{a.git_branch}[/]")
        if a.last_tool:
            ago = _idle_ago(a.idle_seconds)
            ago_str = f" [{GREY}]{ago}[/]" if ago else ""
            parts.append(f"{a.last_tool}{ago_str}")
        if parts:
            lines.append("  ".join(parts))

        # Line 3: Counts + sparkline
        meta: list[str] = []
        if a.tool_count:
            meta.append(f"[{GREY}]{a.tool_count} tools[/]")
        if a.error_count:
            meta.append(f"[{ORANGE}]{a.error_count} err[/]")
        if a.subagent_count:
            meta.append(f"[{AQUA}]+{a.subagent_count} sub[/]")
        if a.uptime:
            meta.append(f"[{GREY}]{a.uptime}[/]")
        if a.sparkline:
            meta.append(f"[{GREEN}]{a.sparkline}[/]")
        if meta:
            lines.append("  ".join(meta))

        # Line 4: Prompt snippet
        if a.initial_prompt:
            prompt = a.initial_prompt.replace("\n", " ")[:60]
            lines.append(f"[{GREY}]{prompt}[/]")

        # Special states
        if a.status == AgentStatus.WAITING_PERMISSION and a.tool_request_summary:
            summary = a.tool_request_summary.splitlines()[0][:60]
            lines.append(f"[{RED}]PERM: {a.last_tool}[/] [{GREY}]{summary}[/]")

        if a.status == AgentStatus.ERROR and a.last_error:
            err = a.last_error.replace("\n", " ")[:60]
            lines.append(f"[{ORANGE}]{err}[/]")

        if a.notification_message:
            lines.append(f"[{PURPLE}]{a.notification_message[:60]}[/]")

        return "\n".join(lines)

    def _render_task_card(self) -> str:
        t = self.task_data
        assert t is not None
        color, label = STATUS_DISPLAY.get(t.status, (GREY, "?"))

        # Line 1: Title + status
        lines = [f"[bold {FG}]{t.title}[/]  [{color}]{label}[/]"]

        # Line 2: Branch
        if t.branch:
            lines.append(f"[{AQUA}]{t.branch}[/]")

        # Line 3: Repos
        if t.repos:
            repos = ", ".join(t.repos[:3])
            lines.append(f"[{GREY}]{repos}[/]")

        # Line 4: Description snippet
        if t.description:
            desc = t.description.replace("\n", " ")[:80]
            lines.append(f"[{GREY}]{desc}[/]")

        return "\n".join(lines)
