"""Header bar widget — shows summary counts and title."""

from __future__ import annotations

from textual.widgets import Static

from grove.dash.models import ClaudeUsage, StatusSummary

# Gruvbox dark palette
_GREEN = "#b8bb26"
_AQUA = "#8ec07c"
_RED = "#fb4934"
_YELLOW = "#fabd2f"
_GREY = "#928374"
_FG = "#fbf1c7"
_ORANGE = "#fe8019"


def _usage_color(pct: int) -> str:
    """Pick a color based on utilization percentage."""
    if pct < 50:
        return _GREEN
    if pct < 75:
        return _YELLOW
    if pct < 90:
        return _ORANGE
    return _RED


class HeaderBar(Static):
    """Top bar showing Grove branding and agent status summary."""

    def __init__(self, **kwargs: object) -> None:
        super().__init__("", **kwargs)
        self._summary = StatusSummary()

    def update_summary(self, summary: StatusSummary) -> None:
        self._summary = summary
        parts = [f"[bold {_FG}]\u26a1\ufe0e gw dash[/]  "]

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

        # Claude usage (from Usage Tracker cache)
        usage = ClaudeUsage.read_cache()
        if usage is not None:
            color = _usage_color(usage.utilization)
            stale = f" [{_GREY}]stale[/]" if usage.stale else ""
            reset = f" → {usage.reset_countdown}" if usage.reset_countdown else ""
            parts.append(
                f"  [{_GREY}]│[/]  "
                f"[{_GREY}]usage:[/] [{color}]{usage.utilization}% {usage.bar}{reset}[/]"
                f"{stale}"
            )

        self.update("".join(parts))
