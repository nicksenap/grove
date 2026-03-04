"""Textual TUI for running workspace processes with a sidebar + log pane."""

from __future__ import annotations

import subprocess
import threading
from dataclasses import dataclass, field
from enum import Enum, auto

from textual import work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical
from textual.css.query import NoMatches
from textual.widgets import Footer, Label, ListItem, ListView, RichLog, Static

# ---------------------------------------------------------------------------
# Data model
# ---------------------------------------------------------------------------


class ProcStatus(Enum):
    """Lifecycle status of a managed process."""

    STARTING = auto()
    RUNNING = auto()
    EXITED = auto()


@dataclass
class ProcessState:
    """Tracks a single repo's subprocess and metadata."""

    repo_name: str
    command: str
    cwd: str
    status: ProcStatus = ProcStatus.STARTING
    process: subprocess.Popen | None = field(default=None, repr=False)
    exit_code: int | None = None
    _cancel: threading.Event = field(default_factory=threading.Event, repr=False)

    @property
    def status_icon(self) -> str:
        match self.status:
            case ProcStatus.STARTING:
                return "[yellow]●[/]"
            case ProcStatus.RUNNING:
                return "[green]●[/]"
            case ProcStatus.EXITED:
                if self.exit_code == 0:
                    return "[dim]●[/]"
                return "[red]●[/]"


# ---------------------------------------------------------------------------
# Widgets
# ---------------------------------------------------------------------------


class AppHeader(Horizontal):
    """Top bar showing app name and selected repo info."""

    DEFAULT_CSS = """
    AppHeader {
        dock: top;
        height: 1;
        background: $accent;
        color: $text;
        padding: 0 1;
    }
    AppHeader #header-title {
        width: 1fr;
        color: $text;
        text-style: bold;
    }
    AppHeader #header-info {
        color: $text 70%;
    }
    """


class RepoItem(ListItem):
    """A sidebar entry for one repo."""

    DEFAULT_CSS = """
    RepoItem {
        padding: 0 1;
        height: 1;
    }
    """

    def __init__(self, index: int, repo_name: str) -> None:
        super().__init__()
        self.repo_index = index
        self.repo_name = repo_name

    def compose(self) -> ComposeResult:
        yield Label(f"[yellow]●[/] {self.repo_name}", id=f"label-{self.repo_index}")


# ---------------------------------------------------------------------------
# App
# ---------------------------------------------------------------------------


class RunApp(App):
    """TUI that manages multiple subprocess outputs in a sidebar + log layout."""

    SHOW_COMMAND_PALETTE = False
    theme = "gruvbox"

    CSS = """
    /* ── Sidebar ─────────────────────────────────── */
    #sidebar-panel {
        width: 32;
        dock: left;
        border-right: round $accent 40%;
        border-title-color: $text-accent 50%;
        border-title-style: bold;
        padding: 0;
    }
    #sidebar-panel:focus-within {
        border-right: round $accent;
        border-title-color: $text-accent;
    }
    #sidebar {
        width: 100%;
    }

    /* ── Log pane ────────────────────────────────── */
    #log-panel {
        border: round $accent 40%;
        border-title-color: $text-accent 50%;
        border-title-style: bold;
    }
    #log-panel:focus-within {
        border: round $accent;
        border-title-color: $text-accent;
    }
    RichLog {
        display: none;
        background: $surface;
        padding: 0 1;
    }
    RichLog.active {
        display: block;
    }
    """

    BINDINGS = [
        Binding("q", "quit_app", "Quit", show=True),
        Binding("r", "restart", "Restart", show=True),
        # Vim navigation
        Binding("j", "nav_down", "j/↓", show=True),
        Binding("k", "nav_up", "k/↑", show=True),
        Binding("g", "nav_first", "gg first", show=False),
        Binding("G", "nav_last", "G last", show=False),  # noqa: N815
        # Number keys
        Binding("1", "select_1", "1", show=False),
        Binding("2", "select_2", "2", show=False),
        Binding("3", "select_3", "3", show=False),
        Binding("4", "select_4", "4", show=False),
        Binding("5", "select_5", "5", show=False),
        Binding("6", "select_6", "6", show=False),
        Binding("7", "select_7", "7", show=False),
        Binding("8", "select_8", "8", show=False),
        Binding("9", "select_9", "9", show=False),
    ]

    def __init__(self, entries: list[tuple[str, str, str]]) -> None:
        """*entries*: list of ``(repo_name, command, cwd)``."""
        super().__init__()
        self.procs: list[ProcessState] = [
            ProcessState(repo_name=name, command=cmd, cwd=cwd) for name, cmd, cwd in entries
        ]
        self._selected: int = 0

    # ── Layout ────────────────────────────────────────────────────────────

    def compose(self) -> ComposeResult:
        yield AppHeader(
            Static("[b]grove[/b] run", id="header-title"),
            Label("", id="header-info"),
        )
        with Horizontal():
            with Vertical(id="sidebar-panel"):
                yield ListView(
                    *[RepoItem(i, p.repo_name) for i, p in enumerate(self.procs)],
                    id="sidebar",
                )
            with Vertical(id="log-panel"):
                for i, _ps in enumerate(self.procs):
                    classes = "active" if i == 0 else ""
                    yield RichLog(id=f"log-{i}", classes=classes, wrap=True, markup=True)
        yield Footer()

    # ── Lifecycle ─────────────────────────────────────────────────────────

    def on_mount(self) -> None:
        sidebar = self.query_one("#sidebar", ListView)
        sidebar.index = 0
        self._update_panel_titles()
        for i in range(len(self.procs)):
            self._start_process(i)

    # ── Process management ────────────────────────────────────────────────

    def _start_process(self, idx: int) -> None:
        ps = self.procs[idx]
        ps.status = ProcStatus.STARTING
        ps.exit_code = None
        ps._cancel = threading.Event()
        self._update_label(idx)
        self._run_subprocess(idx)

    @work(thread=True)
    def _run_subprocess(self, idx: int) -> None:
        ps = self.procs[idx]
        log = self.query_one(f"#log-{idx}", RichLog)

        try:
            proc = subprocess.Popen(
                ps.command,
                cwd=ps.cwd,
                shell=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.STDOUT,
                text=True,
                bufsize=1,
            )
        except Exception as exc:
            self.call_from_thread(log.write, f"[red]Failed to start: {exc}[/]")
            ps.status = ProcStatus.EXITED
            ps.exit_code = -1
            self.call_from_thread(self._update_label, idx)
            self.call_from_thread(self._update_header_info)
            return

        ps.process = proc
        ps.status = ProcStatus.RUNNING
        self.call_from_thread(self._update_label, idx)
        self.call_from_thread(self._update_header_info)

        assert proc.stdout is not None
        for line in proc.stdout:
            if ps._cancel.is_set():
                break
            self.call_from_thread(log.write, line.rstrip("\n"))

        proc.wait()
        ps.exit_code = proc.returncode
        ps.status = ProcStatus.EXITED
        self.call_from_thread(self._update_label, idx)
        self.call_from_thread(self._update_header_info)

    def _terminate_process(self, idx: int) -> None:
        ps = self.procs[idx]
        ps._cancel.set()
        if ps.process and ps.process.poll() is None:
            ps.process.terminate()
            try:
                ps.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                ps.process.kill()
                ps.process.wait()

    def _update_label(self, idx: int) -> None:
        try:
            ps = self.procs[idx]
            label = self.query_one(f"#label-{idx}", Label)
            label.update(f"{ps.status_icon} {ps.repo_name}")
        except NoMatches:
            pass  # widget removed during shutdown

    def _update_header_info(self) -> None:
        try:
            running = sum(1 for p in self.procs if p.status == ProcStatus.RUNNING)
            exited = sum(1 for p in self.procs if p.status == ProcStatus.EXITED)
            parts = []
            if running:
                parts.append(f"[green]{running} running[/]")
            if exited:
                failed = sum(
                    1 for p in self.procs if p.status == ProcStatus.EXITED and p.exit_code != 0
                )
                if failed:
                    parts.append(f"[red]{failed} failed[/]")
                ok = exited - failed
                if ok:
                    parts.append(f"[dim]{ok} exited[/]")
            info = self.query_one("#header-info", Label)
            info.update("  ".join(parts))
        except NoMatches:
            pass  # widget removed during shutdown

    def _update_panel_titles(self) -> None:
        n = len(self.procs)
        sidebar_panel = self.query_one("#sidebar-panel")
        sidebar_panel.border_title = f"Repos ({n})"
        self._update_log_panel_title()

    def _update_log_panel_title(self) -> None:
        try:
            ps = self.procs[self._selected]
            log_panel = self.query_one("#log-panel")
            log_panel.border_title = f"{ps.repo_name} — {ps.command}"
        except NoMatches:
            pass  # widget removed during shutdown

    # ── Selection ─────────────────────────────────────────────────────────

    def on_list_view_selected(self, event: ListView.Selected) -> None:
        if isinstance(event.item, RepoItem):
            self._switch_to(event.item.repo_index)

    def on_list_view_highlighted(self, event: ListView.Highlighted) -> None:
        if event.item and isinstance(event.item, RepoItem):
            self._switch_to(event.item.repo_index)

    def _switch_to(self, idx: int) -> None:
        if idx < 0 or idx >= len(self.procs) or idx == self._selected:
            return
        # Only touch the two logs that change
        old_log = self.query_one(f"#log-{self._selected}", RichLog)
        new_log = self.query_one(f"#log-{idx}", RichLog)
        old_log.remove_class("active")
        new_log.add_class("active")
        self._selected = idx
        self._update_log_panel_title()

    # ── Key actions ───────────────────────────────────────────────────────

    def action_quit_app(self) -> None:
        for i in range(len(self.procs)):
            self._terminate_process(i)
        self.exit()

    def action_nav_down(self) -> None:
        sidebar = self.query_one("#sidebar", ListView)
        sidebar.index = min(self._selected + 1, len(self.procs) - 1)

    def action_nav_up(self) -> None:
        sidebar = self.query_one("#sidebar", ListView)
        sidebar.index = max(self._selected - 1, 0)

    def action_nav_first(self) -> None:
        self.query_one("#sidebar", ListView).index = 0

    def action_nav_last(self) -> None:
        self.query_one("#sidebar", ListView).index = len(self.procs) - 1

    def action_restart(self) -> None:
        idx = self._selected
        self._terminate_process(idx)
        log = self.query_one(f"#log-{idx}", RichLog)
        log.clear()
        log.write("[dim]Restarting…[/]")
        self._start_process(idx)

    def _select_by_number(self, n: int) -> None:
        idx = n - 1
        if 0 <= idx < len(self.procs):
            self.query_one("#sidebar", ListView).index = idx

    def action_select_1(self) -> None:
        self._select_by_number(1)

    def action_select_2(self) -> None:
        self._select_by_number(2)

    def action_select_3(self) -> None:
        self._select_by_number(3)

    def action_select_4(self) -> None:
        self._select_by_number(4)

    def action_select_5(self) -> None:
        self._select_by_number(5)

    def action_select_6(self) -> None:
        self._select_by_number(6)

    def action_select_7(self) -> None:
        self._select_by_number(7)

    def action_select_8(self) -> None:
        self._select_by_number(8)

    def action_select_9(self) -> None:
        self._select_by_number(9)
