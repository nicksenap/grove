"""KanbanColumn widget — a scrollable column holding TaskCards."""

from __future__ import annotations

import logging

from textual.containers import VerticalScroll

from grove.dash.models import AgentState
from grove.dash.store import Task
from grove.dash.widgets.task_card import TaskCard

log = logging.getLogger("grove.dash")


class KanbanColumn(VerticalScroll):
    """A single kanban column with a header and scrollable card list."""

    DEFAULT_CSS = """
    KanbanColumn {
        width: 1fr;
        height: 1fr;
        border: round $panel;
        border-title-color: $primary;
        border-title-style: bold;
        padding: 0;
    }

    KanbanColumn:focus-within {
        border: round $primary;
    }
    """

    def __init__(
        self,
        column_id: str,
        title: str,
        **kwargs: object,
    ) -> None:
        super().__init__(id=f"col-{column_id}", **kwargs)
        self.column_id = column_id
        self.column_title = title
        self._item_keys: list[str] = []
        self.border_title = title

    def update_items(
        self,
        agents: list[AgentState] | None = None,
        tasks: list[Task] | None = None,
    ) -> None:
        """Sync cards to match given agents and tasks."""
        agents = agents or []
        tasks = tasks or []

        # Build ordered list of (key, agent_or_none, task_or_none)
        items: list[tuple[str, AgentState | None, Task | None]] = []
        for t in tasks:
            items.append((f"task-{t.id}", None, t))
        for a in agents:
            items.append((f"agent-{a.session_id}", a, None))

        new_keys = [item[0] for item in items]
        cards = list(self.query(TaskCard))

        if new_keys == self._item_keys and len(cards) == len(items):
            # Same items — update in place
            for i, (_key, agent, task) in enumerate(items):
                try:
                    if agent:
                        cards[i].update_agent(agent)
                    elif task:
                        cards[i].update_task(task)
                except Exception:
                    log.exception("Failed to update card %d in %s", i, self.column_id)
        else:
            # Structural change — remove all, then mount fresh (no IDs)
            self._item_keys = new_keys
            for card in cards:
                try:
                    card.remove()
                except Exception:
                    log.exception("Failed to remove card in %s", self.column_id)
            for _key, agent, task in items:
                self.mount(TaskCard(agent=agent, task_data=task))

        # Update column title with count
        count = len(items)
        self.border_title = f"{self.column_title} ({count})" if count else self.column_title

    # Keep backward compat
    def update_agents(self, agents: list[AgentState]) -> None:
        self.update_items(agents=agents)

    @property
    def card_count(self) -> int:
        return len(self._item_keys)

    @property
    def focused_card(self) -> TaskCard | None:
        """Return the currently focused card, if any."""
        for card in self.query(TaskCard):
            if card.has_focus:
                return card
        return None
