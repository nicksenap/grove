"""Grove Dashboard TUI — Textual app for monitoring Claude Code agents."""

from __future__ import annotations

import logging
from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical
from textual.theme import Theme
from textual.widgets import Input, Static

from grove.dash import manager
from grove.dash.constants import CLEANUP_INTERVAL, STATE_POLL_INTERVAL
from grove.dash.models import AgentState
from grove.dash.widgets.agent_detail import AgentDetail
from grove.dash.widgets.header_bar import HeaderBar
from grove.dash.widgets.kanban_board import KanbanBoard
from grove.dash.widgets.session_list import matches_filter
from grove.dash.widgets.task_card import TaskCard

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

    TITLE = "\u26a1\ufe0e gw dash"

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

    #board-pane {
        width: 2fr;
        height: 1fr;
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
        Binding("h", "cursor_left", "Left", show=False),
        Binding("l", "cursor_right", "Right", show=False),
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
            with Vertical(id="board-pane"):
                yield KanbanBoard(id="kanban-board")
            with Vertical(id="detail-pane") as pane:
                pane.border_title = "Detail"
                yield AgentDetail()
        yield Static(
            "[dim]q[/] quit  "
            "[dim]h/l[/] columns  "
            "[dim]j/k[/] cards  "
            "[dim]enter[/] jump  "
            "[dim]y/n[/] approve/deny  "
            "[dim]r[/] refresh  "
            "[dim]/[/] search",
            id="status-line",
        )

    # --- Polling ---

    def _poll_state(self) -> None:
        agents, summary = manager.scan()
        self._agents = agents
        self._apply_filter()

        self.query_one(HeaderBar).update_summary(summary)
        board = self.query_one(KanbanBoard)
        board.update_board(self._filtered)
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
        board = self.query_one(KanbanBoard)
        agent = board.focused_agent
        detail.show_agent(agent)

    def on_key(self, event) -> None:
        log.info("KEY: key=%r char=%r", event.key, event.character)

    def on_descendant_focus(self, event) -> None:
        if isinstance(event.widget, TaskCard):
            self._update_detail()

    # --- Navigation ---

    def action_cursor_down(self) -> None:
        self.query_one(KanbanBoard).focus_next_card()

    def action_cursor_up(self) -> None:
        self.query_one(KanbanBoard).focus_prev_card()

    def action_cursor_left(self) -> None:
        self.query_one(KanbanBoard).focus_prev_column()

    def action_cursor_right(self) -> None:
        self.query_one(KanbanBoard).focus_next_column()

    # --- Agent actions ---

    def action_jump_to_agent(self) -> None:
        """Jump to the selected agent's Zellij tab."""
        results = self.query("#search-input")
        if results and results.first().has_focus:
            self._handle_search_submit()
            return

        from grove import zellij

        board = self.query_one(KanbanBoard)
        agent = board.focused_agent
        if agent is None:
            return

        log.info(
            "JUMP: project=%r cwd=%r display=%r",
            agent.project_name,
            agent.cwd,
            agent.display_name,
        )
        result = zellij.jump_to_agent(agent.project_name, agent.cwd or "")
        log.info("JUMP: result=%s", result)

    def action_approve(self) -> None:
        self._send_response(approve=True)

    def action_deny(self) -> None:
        self._send_response(approve=False)

    def _send_response(self, *, approve: bool) -> None:
        from grove import zellij

        board = self.query_one(KanbanBoard)
        agent = board.focused_agent
        if agent is None or agent.status.value != "WAITING_PERMISSION":
            return

        if zellij.jump_to_agent(agent.project_name, agent.cwd or ""):
            if approve:
                zellij.approve()
            else:
                zellij.deny()
            zellij.jump_to_agent("grove")

    # --- Search ---

    def action_start_search(self) -> None:
        log.info("SEARCH: action_start_search fired")
        results = self.query("#search-input")
        if results:
            results.first().focus()
            return
        search = Input(placeholder="/search...", id="search-input")
        self.mount(search)
        search.focus()

    def _handle_search_submit(self) -> None:
        self._dismiss_search(clear=False)

    def _dismiss_search(self, *, clear: bool = False) -> None:
        results = self.query("#search-input")
        if results:
            results.first().remove()
        if clear:
            self._search_query = ""
            self._apply_filter()
            board = self.query_one(KanbanBoard)
            board.update_board(self._filtered)

    def action_stop_search(self) -> None:
        self._dismiss_search(clear=True)

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id == "search-input":
            self._search_query = event.value
            self._apply_filter()
            self.query_one(KanbanBoard).update_board(self._filtered)

    def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id == "search-input":
            self._handle_search_submit()

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
