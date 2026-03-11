"""Task create/edit modal — input form for task cards."""

from __future__ import annotations

import logging

from textual.app import ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical
from textual.screen import ModalScreen
from textual.widgets import Input, Label, SelectionList, Static, TextArea

from grove.dash.store import Task

log = logging.getLogger("grove.dash")


def _load_repo_choices() -> tuple[list[str], dict[str, list[str]]]:
    """Load available repos and presets from Grove config.

    Returns (repo_names, presets_dict).
    """
    try:
        from grove import config, discover

        cfg = config.load_config()
        available = discover.find_all_repos(cfg.repo_dirs)
        repo_names = sorted(available.keys())
        presets = dict(cfg.presets) if cfg.presets else {}
        return repo_names, presets
    except Exception:
        log.exception("Failed to load repo config")
        return [], {}


class TaskModal(ModalScreen[Task | None]):
    """Modal dialog for creating or editing a task card."""

    DEFAULT_CSS = """
    TaskModal {
        align: center middle;
    }

    #task-modal-container {
        width: 70;
        height: auto;
        max-height: 30;
        border: round $primary;
        background: $surface;
        padding: 1 2;
    }

    .field-label {
        width: 100%;
        height: 1;
        color: $text-muted;
        margin: 1 0 0 0;
    }

    .field-label.first {
        margin: 0;
    }

    #task-title {
        width: 100%;
        height: 1;
        border: none;
        background: $background;
        padding: 0 1;
    }

    #task-branch {
        width: 100%;
        height: 1;
        border: none;
        background: $background;
        padding: 0 1;
    }

    #task-description {
        width: 100%;
        height: 5;
        border: none;
        background: $background;
    }

    #preset-bar {
        width: 100%;
        height: 1;
        margin: 0;
    }

    .preset-btn {
        min-width: 8;
        height: 1;
        border: none;
        background: $panel;
        color: $text;
        padding: 0 1;
        margin: 0 1 0 0;
    }

    .preset-btn:hover {
        background: $primary;
    }

    #repo-list {
        width: 100%;
        height: 8;
        border: none;
        background: $background;
    }

    #task-modal-hint {
        width: 100%;
        height: 1;
        color: $text-muted;
        margin: 1 0 0 0;
    }
    """

    BINDINGS = [
        Binding("escape", "cancel", "Cancel"),
    ]

    def __init__(self, existing: Task | None = None) -> None:
        super().__init__()
        self._existing = existing
        self._repo_names, self._presets = _load_repo_choices()

    def compose(self) -> ComposeResult:
        e = self._existing
        title = "Edit Task" if e else "Create Task"
        selected_repos = set(e.repos) if e else set()

        with Vertical(id="task-modal-container"):
            yield Label(f"[bold]{title}[/]", classes="field-label first")

            yield Label("Title", classes="field-label")
            yield Input(
                value=e.title if e else "",
                placeholder="e.g. add-bulk-import",
                id="task-title",
            )

            yield Label("Branch", classes="field-label")
            yield Input(
                value=e.branch if e else "",
                placeholder="defaults to feat/<title>",
                id="task-branch",
            )

            yield Label("Description / Prompt", classes="field-label")
            yield TextArea(
                e.description if e else "",
                id="task-description",
            )

            if self._repo_names:
                yield Label("Repos", classes="field-label")

                # Preset buttons
                if self._presets:
                    with Horizontal(id="preset-bar"):
                        for name in self._presets:
                            yield Static(
                                f"[dim]({name})[/]",
                                classes="preset-btn",
                                id=f"preset-{name}",
                            )

                # Repo selection list
                selections = [(repo, repo, repo in selected_repos) for repo in self._repo_names]
                yield SelectionList(*selections, id="repo-list")

            yield Label(
                "[dim]tab[/] next  "
                "[dim]space[/] toggle repo  "
                "[dim]ctrl+s[/] save  "
                "[dim]esc[/] cancel",
                id="task-modal-hint",
            )

    def on_mount(self) -> None:
        self.query_one("#task-title", Input).focus()

    def on_static_click(self, event: Static.Click) -> None:
        """Handle preset button clicks."""
        widget = event.widget
        if not widget.id or not widget.id.startswith("preset-"):
            return
        preset_name = widget.id[len("preset-") :]
        repos = self._presets.get(preset_name, [])
        if not repos:
            return

        # Toggle the preset repos in the selection list
        try:
            repo_list = self.query_one("#repo-list", SelectionList)
        except Exception:
            return

        repo_set = set(repos)
        for repo_name in self._repo_names:
            if repo_name in repo_set:
                repo_list.select(repo_name)
            else:
                repo_list.deselect(repo_name)

    def key_ctrl_s(self) -> None:
        """Save the task."""
        self._save()

    def _save(self) -> None:
        title = self.query_one("#task-title", Input).value.strip()
        if not title:
            self.dismiss(None)
            return

        branch = self.query_one("#task-branch", Input).value.strip()
        if not branch:
            branch = f"feat/{title}" if not title.startswith("feat/") else title
        description = self.query_one("#task-description", TextArea).text.strip()

        # Collect selected repos
        repos: list[str] = []
        try:
            repo_list = self.query_one("#repo-list", SelectionList)
            repos = list(repo_list.selected)
        except Exception:
            pass

        if self._existing:
            task = self._existing
            task.title = title
            task.branch = branch
            task.description = description
            task.repos = repos
        else:
            task = Task.create(
                title=title,
                branch=branch,
                description=description,
                repos=repos,
            )

        self.dismiss(task)

    def action_cancel(self) -> None:
        self.dismiss(None)
