"""Task create/edit modal — input form for task cards."""

from __future__ import annotations

import logging

from textual.app import ComposeResult
from textual.binding import Binding
from textual.containers import Vertical
from textual.screen import ModalScreen
from textual.widgets import Input, Label, OptionList, SelectionList, TextArea

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
    """Modal dialog for creating or editing a task card.

    Single-screen form with inline preset picker and repo selection.
    Tab flows naturally through all fields.
    """

    DEFAULT_CSS = """
    TaskModal {
        align: center middle;
    }

    #task-modal-container {
        width: 70;
        height: auto;
        max-height: 38;
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
        height: 4;
        border: none;
        background: $background;
    }

    #preset-list {
        width: 100%;
        height: auto;
        max-height: 5;
        border: none;
        background: $background;
    }

    #repo-search {
        width: 100%;
        height: 1;
        border: none;
        background: $background;
        padding: 0 1;
        display: none;
    }

    #repo-search.visible {
        display: block;
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
        Binding("escape", "cancel_or_search", "Cancel"),
        Binding("slash", "start_search", "Search", show=False),
    ]

    def __init__(self, existing: Task | None = None) -> None:
        super().__init__()
        self._existing = existing
        self._repo_names, self._presets = _load_repo_choices()
        self._searching = False

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
                tab_behavior="focus",
            )

            if self._repo_names:
                # Preset picker (tier 1)
                if self._presets:
                    yield Label("Presets", classes="field-label")
                    yield OptionList(
                        *list(self._presets.keys()),
                        id="preset-list",
                    )

                # Repo multi-select (tier 2) with search
                yield Label("Repos [dim](/[/] search)", classes="field-label")
                yield Input(placeholder="filter repos...", id="repo-search")
                selections = [(repo, repo, repo in selected_repos) for repo in self._repo_names]
                yield SelectionList(*selections, id="repo-list")

            yield Label(
                "[dim]tab[/] next  "
                "[dim]enter[/] preset  "
                "[dim]space[/] toggle  "
                "[dim]/[/] filter  "
                "[dim]ctrl+s[/] save  "
                "[dim]esc[/] cancel",
                id="task-modal-hint",
            )

    def on_mount(self) -> None:
        self.query_one("#task-title", Input).focus()

    # --- Preset selection ---

    def on_option_list_option_selected(self, event: OptionList.OptionSelected) -> None:
        preset_name = str(event.option.prompt)
        repos = self._presets.get(preset_name, [])
        if not repos:
            return

        log.info("MODAL: preset selected %r -> %r", preset_name, repos)

        try:
            sel = self.query_one("#repo-list", SelectionList)
        except Exception:
            log.exception("Failed to find repo list")
            return

        repo_set = set(repos)
        for repo_name in self._repo_names:
            if repo_name in repo_set:
                sel.select(repo_name)
            else:
                sel.deselect(repo_name)

    # --- Repo search ---

    def action_start_search(self) -> None:
        # Only activate when repo-list is focused
        focused = self.app.focused
        try:
            repo_list = self.query_one("#repo-list", SelectionList)
        except Exception:
            return
        if focused is not repo_list:
            return

        search = self.query_one("#repo-search", Input)
        search.add_class("visible")
        search.value = ""
        search.focus()
        self._searching = True
        log.info("MODAL: search started")

    def on_input_changed(self, event: Input.Changed) -> None:
        if event.input.id != "repo-search":
            return
        self._filter_repos(event.value)

    def on_input_submitted(self, event: Input.Submitted) -> None:
        if event.input.id != "repo-search":
            return
        self._dismiss_search()

    def _dismiss_search(self) -> None:
        search = self.query_one("#repo-search", Input)
        search.remove_class("visible")
        self._searching = False
        self.query_one("#repo-list", SelectionList).focus()
        log.info("MODAL: search dismissed")

    def _filter_repos(self, query: str) -> None:
        sel = self.query_one("#repo-list", SelectionList)
        q = query.lower().strip()

        # Remember currently selected values before clearing
        currently_selected = set(sel.selected)

        sel.clear_options()
        for repo in self._repo_names:
            if not q or q in repo.lower():
                sel.add_option((repo, repo, repo in currently_selected))

    # --- Save / cancel ---

    def key_ctrl_s(self) -> None:
        """Save the task."""
        self._save()

    def action_cancel_or_search(self) -> None:
        if self._searching:
            self._dismiss_search()
        else:
            self.dismiss(None)

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
