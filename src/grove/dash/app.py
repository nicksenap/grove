"""Grove Dashboard TUI — Textual app for monitoring Claude Code agents."""

from __future__ import annotations

import logging
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical
from textual.theme import Theme
from textual.widgets import DataTable, Input, Static

from grove.dash import manager
from grove.dash.constants import CLEANUP_INTERVAL, STATE_POLL_INTERVAL
from grove.dash.models import AgentState
from grove.dash.widgets.agent_detail import AgentDetail
from grove.dash.widgets.header_bar import HeaderBar
from grove.dash.widgets.session_list import SessionList, matches_filter

log = logging.getLogger("grove.dash")

GRUVBOX_DARK = Theme(
    name="gruvbox-dark",
    primary="#85A598",
    secondary="#A89A85",
    accent="#fabd2f",
    foreground="#fbf1c7",
    background="#282828",
    surface="#3c3836",
    panel="#504945",
    success="#b8bb26",
    warning="#fabd2f",
    error="#fb4934",
    dark=True,
    variables={
        "block-cursor-foreground": "#fbf1c7",
        "input-selection-background": "#689d6a40",
    },
)


class DashboardApp(App):
    """Grove agent dashboard."""

    TITLE = "Grove Dashboard"

    CSS = """
    Screen {
        background: $background;
    }

    #header {
        dock: top;
        height: 3;
        padding: 1 1;
        color: $foreground;
        text-style: bold;
    }

    #status-line {
        dock: bottom;
        height: 1;
        padding: 0 1;
        color: $text-muted;
        background: $background;
    }

    #main-split {
        height: 1fr;
    }

    #list-pane {
        width: 2fr;
        height: 1fr;
        border: round $panel;
        border-title-color: $primary;
        border-title-style: bold;
        padding: 0;
    }

    #list-pane:focus-within {
        border: round $primary;
    }

    SessionList {
        height: 1fr;
        min-height: 4;
        background: $background;
    }

    #detail-pane {
        width: 1fr;
        height: 1fr;
        border: round $panel;
        border-title-color: $primary;
        border-title-style: bold;
        padding: 0 1;
    }

    #detail-pane:focus-within {
        border: round $primary;
    }

    AgentDetail {
        color: $foreground;
        background: $background;
    }

    #search-input {
        dock: bottom;
        height: 1;
        background: $surface;
        border: none;
        padding: 0 1;
    }
    """

    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("j", "cursor_down", "Down", show=False),
        Binding("k", "cursor_up", "Up", show=False),
        Binding("enter", "jump_to_agent", "Jump", priority=True),
        Binding("y", "approve", "Approve"),
        Binding("n", "deny", "Deny"),
        Binding("r", "refresh", "Refresh"),
        Binding("/", "start_search", "Search", show=False),
        Binding("escape", "stop_search", "Clear search", show=False),
    ]

    def __init__(self) -> None:
        super().__init__()
        self._agents: list[AgentState] = []
        self._filtered: list[AgentState] = []
        self._search_query: str = ""

    def on_mount(self) -> None:
        self.register_theme(GRUVBOX_DARK)
        self.theme = "gruvbox-dark"
        self.set_interval(STATE_POLL_INTERVAL, self._poll_state)
        self.set_interval(CLEANUP_INTERVAL, self._run_cleanup)
        self._poll_state()

    def compose(self) -> ComposeResult:
        yield HeaderBar(id="header")
        with Horizontal(id="main-split"):
            with Vertical(id="list-pane") as pane:
                pane.border_title = "Agents"
                yield SessionList(id="session-list")
            with Vertical(id="detail-pane") as pane:
                pane.border_title = "Detail"
                yield AgentDetail()
        yield Static(
            "[dim]q[/] quit  "
            "[dim]↑↓[/] select  "
            "[dim]enter[/] jump  "
            "[dim]y/n[/] approve/deny  "
            "[dim]r[/] refresh  "
            "[dim]/[/] search",
            id="status-line",
        )

    def _poll_state(self) -> None:
        agents, summary = manager.scan()
        self._agents = agents
        self._apply_filter()

        self.query_one(HeaderBar).update_summary(summary)
        self.query_one(SessionList).update_agents(self._filtered)
        self._update_detail()

    def _apply_filter(self) -> None:
        if self._search_query:
            self._filtered = [a for a in self._agents if matches_filter(a, self._search_query)]
        else:
            self._filtered = list(self._agents)

    def _run_cleanup(self) -> None:
        manager.cleanup_stale()
        manager.reset_stale_permissions()

    def _update_detail(self) -> None:
        detail = self.query_one(AgentDetail)
        session_list = self.query_one(SessionList)
        idx = session_list.selected_index

        if idx is not None and idx < len(self._filtered):
            detail.show_agent(self._filtered[idx])
        else:
            detail.show_agent(None)

    def on_key(self, event) -> None:
        log.info("KEY: key=%r char=%r", event.key, event.character)

    def on_data_table_row_highlighted(self, event: DataTable.RowHighlighted) -> None:
        self._update_detail()

    def action_cursor_down(self) -> None:
        self.query_one(SessionList).action_cursor_down()

    def action_cursor_up(self) -> None:
        self.query_one(SessionList).action_cursor_up()

    def action_jump_to_agent(self) -> None:
        """Jump to the selected agent's Zellij tab."""
        # If search input is focused, treat enter as submit, not jump
        results = self.query("#search-input")
        if results and results.first().has_focus:
            self._dismiss_search(clear=False)
            return

        from grove.dash import zellij

        session_list = self.query_one(SessionList)
        idx = session_list.selected_index
        log.info("JUMP: idx=%s, filtered_count=%d", idx, len(self._filtered))
        if idx is None or idx >= len(self._filtered):
            return

        agent = self._filtered[idx]
        log.info(
            "JUMP: project=%r cwd=%r display=%r",
            agent.project_name,
            agent.cwd,
            agent.display_name,
        )
        result = zellij.jump_to_agent(agent.project_name, agent.cwd or "")
        log.info("JUMP: result=%s", result)

    def action_approve(self) -> None:
        """Approve permission request for selected agent."""
        self._send_response(approve=True)

    def action_deny(self) -> None:
        """Deny permission request for selected agent."""
        self._send_response(approve=False)

    def _send_response(self, *, approve: bool) -> None:
        from grove.dash import zellij

        session_list = self.query_one(SessionList)
        idx = session_list.selected_index
        if idx is None or idx >= len(self._filtered):
            return

        agent = self._filtered[idx]
        if agent.status.value != "WAITING_PERMISSION":
            return

        if zellij.jump_to_agent(agent.project_name, agent.cwd or ""):
            if approve:
                zellij.approve()
            else:
                zellij.deny()
            zellij.jump_to_agent("grove")

    def action_start_search(self) -> None:
        """Mount a search input and focus it."""
        log.info("SEARCH: action_start_search fired")
        results = self.query("#search-input")
        if results:
            results.first().focus()
            return
        search = Input(placeholder="/search...", id="search-input")
        self.mount(search)
        search.focus()

    def _dismiss_search(self, *, clear: bool = False) -> None:
        """Remove the search input widget."""
        results = self.query("#search-input")
        if results:
            results.first().remove()
        if clear:
            self._search_query = ""
            self._apply_filter()
            self.query_one(SessionList).update_agents(self._filtered)
        self.query_one(SessionList).focus()

    def action_stop_search(self) -> None:
        """Hide search input, clear filter, refocus table."""
        self._dismiss_search(clear=True)

    def on_input_changed(self, event: Input.Changed) -> None:
        """Filter agents as user types in search."""
        self._search_query = event.value
        self._apply_filter()
        self.query_one(SessionList).update_agents(self._filtered)

    def on_input_submitted(self, event: Input.Submitted) -> None:
        """On Enter in search, dismiss input but keep filter active."""
        self._dismiss_search(clear=False)

    def action_refresh(self) -> None:
        self._poll_state()


def run_dashboard() -> None:
    """Launch the dashboard TUI."""
    log_path = Path.home() / ".grove" / "dash.log"
    logging.basicConfig(
        filename=str(log_path),
        level=logging.INFO,
        format="%(asctime)s %(name)s %(message)s",
        force=True,
    )
    log.info("Dashboard starting")

    app = DashboardApp()
    app.run()
