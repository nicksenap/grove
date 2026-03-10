"""Header bar widget — shows summary counts and title."""

from __future__ import annotations

from textual.widgets import Static

from grove.dash.models import StatusSummary

# Gruvbox dark palette
_GREEN = "#b8bb26"
_AQUA = "#8ec07c"
_RED = "#fb4934"
_YELLOW = "#fabd2f"
_GREY = "#928374"
_FG = "#fbf1c7"
_ORANGE = "#fe8019"


class HeaderBar(Static):
    """Top bar showing Grove branding and agent status summary."""

    def __init__(self, **kwargs: object) -> None:
        super().__init__("", **kwargs)
        self._summary = StatusSummary()

    def update_summary(self, summary: StatusSummary) -> None:
        self._summary = summary
        parts = [f"[bold {_FG}]Grove Dashboard[/]  "]

        if summary.total == 0:
            parts.append(f"[{_GREY}]No agents[/]")
        else:
            parts.append(f"[{_GREY}]agents:[/] {summary.total}  ")
            if summary.working:
                parts.append(f"[{_GREEN}]>>>{summary.working}[/]  ")
            if summary.waiting_perm:
                parts.append(f"[bold {_RED}][!]{summary.waiting_perm}[/]  ")
            if summary.waiting_answer:
                parts.append(f"[bold {_YELLOW}][?]{summary.waiting_answer}[/]  ")
            if summary.error:
                parts.append(f"[{_ORANGE}][X]{summary.error}[/]  ")
            if summary.idle:
                parts.append(f"[{_GREY}]---{summary.idle}[/]")

        self.update("".join(parts))
