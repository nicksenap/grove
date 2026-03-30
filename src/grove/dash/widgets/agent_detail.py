"""Agent detail panel — shows selected agent info or permission request."""

from __future__ import annotations

from rich.markup import escape
from textual.widgets import Static

from grove.dash.constants import AgentStatus
from grove.dash.models import AgentState

# Gruvbox dark palette
_GREEN = "#b8bb26"
_AQUA = "#8ec07c"
_RED = "#fb4934"
_YELLOW = "#fabd2f"
_GREY = "#928374"
_FG = "#fbf1c7"
_ORANGE = "#fe8019"
_PURPLE = "#d3869b"

_STATUS_COLORS = {
    AgentStatus.IDLE: (_GREY, "IDLE"),
    AgentStatus.WORKING: (_GREEN, "WORK"),
    AgentStatus.WAITING_PERMISSION: (_RED, "PERM"),
    AgentStatus.WAITING_ANSWER: (_YELLOW, "WAIT"),
    AgentStatus.ERROR: (_ORANGE, "ERR"),
}


class AgentDetail(Static):
    """Detail view for the selected agent."""

    def __init__(self, **kwargs: object) -> None:
        super().__init__("", **kwargs)
        self._agent: AgentState | None = None

    def show_agent(self, agent: AgentState | None) -> None:
        self._agent = agent
        if agent is None:
            self.update(f"[{_GREY}]No agent selected[/]")
            return

        color, label = _STATUS_COLORS.get(agent.status, (_GREY, "?"))

        lines: list[str] = []
        lines.append(f"[bold {_FG}]{escape(agent.display_name)}[/]  [{color}]{label}[/]")
        lines.append("")

        if agent.workspace_name:
            repos = (
                ", ".join(escape(r) for r in agent.workspace_repos) if agent.workspace_repos else ""
            )
            lines.append(f"[{_GREY}]workspace:[/] [{_AQUA}]{escape(agent.workspace_name)}[/]")
            if repos:
                lines.append(f"[{_GREY}]repos:[/]     {repos}")
        elif agent.cwd:
            lines.append(f"[{_GREY}]cwd:[/]    {escape(agent.cwd)}")
        if agent.git_branch:
            dirty = f" ({agent.git_dirty_count} dirty)" if agent.git_dirty_count else ""
            lines.append(f"[{_GREY}]branch:[/] [{_AQUA}]{escape(agent.git_branch)}[/]{dirty}")
        if agent.model:
            model_str = escape(agent.model)
            if agent.permission_mode and agent.permission_mode != "default":
                model_str += f"  [{_YELLOW}]{escape(agent.permission_mode)}[/]"
            lines.append(f"[{_GREY}]model:[/]  {model_str}")
        if agent.uptime:
            source_tag = ""
            if agent.session_source and agent.session_source != "startup":
                source_tag = f" [{_AQUA}]({escape(agent.session_source)})[/]"
            lines.append(f"[{_GREY}]uptime:[/] {agent.uptime}{source_tag}")

        lines.append("")
        lines.append(
            f"[{_GREY}]tools:[/]  {agent.tool_count}    "
            f"[{_GREY}]errors:[/] {agent.error_count}    "
            f"[{_GREY}]subs:[/] {agent.subagent_count}"
        )

        if agent.last_tool:
            idle = agent.idle_seconds
            if idle < 60:
                ago = f"{int(idle)}s ago"
            elif idle < 3600:
                ago = f"{int(idle // 60)}m ago"
            else:
                ago = f"{int(idle // 3600)}h ago"
            lines.append(f"[{_GREY}]last:[/]   {escape(agent.last_tool)} ({ago})")

        if agent.sparkline:
            lines.append(f"[{_GREY}]activity:[/] [{_GREEN}]{agent.sparkline}[/]")

        if agent.compact_count:
            trigger = f" ({escape(agent.compact_trigger)})" if agent.compact_trigger else ""
            lines.append(f"[{_YELLOW}]compacted:[/] {agent.compact_count}x{trigger}")

        if agent.active_subagents:
            subs = ", ".join(escape(s) for s in agent.active_subagents)
            lines.append(f"[{_GREY}]agents:[/]  [{_AQUA}]{subs}[/]")

        # Initial prompt
        if agent.initial_prompt:
            lines.append("")
            prompt_display = agent.initial_prompt.replace("\n", " ")[:120]
            lines.append(f"[{_GREY}]prompt:[/] {escape(prompt_display)}")

        # Last message from agent
        if agent.last_message and agent.status == AgentStatus.IDLE:
            lines.append("")
            msg = agent.last_message.replace("\n", " ")[:200]
            lines.append(f"[{_GREY}]last reply:[/] {escape(msg)}")

        # Last error
        if agent.last_error and agent.status == AgentStatus.ERROR:
            lines.append("")
            lines.append(f"[{_RED}]error:[/] {escape(agent.last_error[:200])}")

        # Permission request detail
        if agent.status == AgentStatus.WAITING_PERMISSION and agent.tool_request_summary:
            lines.append("")
            lines.append(f"[bold {_RED}]Permission Request[/]")
            lines.append(f"[bold]Tool:[/] {escape(agent.last_tool)}")
            lines.append("")
            for line in agent.tool_request_summary.splitlines()[:10]:
                esc_line = escape(line)
                if line.startswith("+ "):
                    lines.append(f"[{_GREEN}]{esc_line}[/]")
                elif line.startswith("- "):
                    lines.append(f"[{_RED}]{esc_line}[/]")
                elif line.startswith("$ "):
                    lines.append(f"[{_YELLOW}]{esc_line}[/]")
                else:
                    lines.append(esc_line)

        # Notification message
        if agent.notification_message:
            lines.append("")
            lines.append(f"[{_PURPLE}]Notification:[/] {escape(agent.notification_message)}")

        self.update("\n".join(lines))
