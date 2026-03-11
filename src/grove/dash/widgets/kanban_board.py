"""KanbanBoard widget — horizontal layout of KanbanColumns."""

from __future__ import annotations

import logging

from textual.containers import Horizontal

from grove.dash.constants import KANBAN_COLUMNS
from grove.dash.models import AgentState
from grove.dash.store import Task
from grove.dash.widgets.kanban_column import KanbanColumn
from grove.dash.widgets.task_card import TaskCard

log = logging.getLogger("grove.dash")


class KanbanBoard(Horizontal):
    """Horizontal layout of kanban columns, sorted by agent status."""

    DEFAULT_CSS = """
    KanbanBoard {
        width: 1fr;
        height: 1fr;
    }
    """

    def __init__(self, **kwargs: object) -> None:
        super().__init__(**kwargs)
        self._columns: dict[str, KanbanColumn] = {}

    def compose(self):
        for col_id, title, _statuses in KANBAN_COLUMNS:
            col = KanbanColumn(col_id, title)
            self._columns[col_id] = col
            yield col

    def update_board(
        self,
        agents: list[AgentState],
        tasks: list[Task] | None = None,
    ) -> None:
        """Distribute agents and tasks across columns by status."""
        tasks = tasks or []

        # Build linked set: session_ids that have a task
        linked_sessions = {t.session_id for t in tasks if t.session_id}

        # Bucket agents
        agent_buckets: dict[str, list[AgentState]] = {col_id: [] for col_id, _, _ in KANBAN_COLUMNS}
        for agent in agents:
            # Skip agents that are linked to a task (task card takes priority)
            if agent.session_id in linked_sessions:
                continue
            placed = False
            for col_id, _title, statuses in KANBAN_COLUMNS:
                if agent.status in statuses:
                    agent_buckets[col_id].append(agent)
                    placed = True
                    break
            if not placed:
                agent_buckets["idle"].append(agent)

        # Bucket tasks
        task_buckets: dict[str, list[Task]] = {col_id: [] for col_id, _, _ in KANBAN_COLUMNS}
        for task in tasks:
            placed = False
            # If task is linked to a running agent, use agent's status for column
            effective_status = task.status
            if task.session_id:
                for agent in agents:
                    if agent.session_id == task.session_id:
                        effective_status = agent.status
                        break

            for col_id, _title, statuses in KANBAN_COLUMNS:
                if effective_status in statuses:
                    task_buckets[col_id].append(task)
                    placed = True
                    break
            if not placed:
                task_buckets["idle"].append(task)

        # Update each column
        for col_id, col in self._columns.items():
            col.update_items(
                agents=agent_buckets[col_id],
                tasks=task_buckets[col_id],
            )

    # Backward compat
    def update_agents(self, agents: list[AgentState]) -> None:
        self.update_board(agents)

    @property
    def focused_card(self) -> TaskCard | None:
        """Return the currently focused TaskCard."""
        focused = self.screen.focused
        if isinstance(focused, TaskCard):
            return focused
        return None

    @property
    def focused_agent(self) -> AgentState | None:
        """Return the AgentState of the currently focused card."""
        card = self.focused_card
        if card:
            return card.agent
        return None

    @property
    def focused_task(self) -> Task | None:
        """Return the Task of the currently focused card."""
        card = self.focused_card
        if card:
            return card.task_data
        return None

    def focus_next_card(self) -> None:
        self._move_focus(1)

    def focus_prev_card(self) -> None:
        self._move_focus(-1)

    def focus_next_column(self) -> None:
        self._move_column(1)

    def focus_prev_column(self) -> None:
        self._move_column(-1)

    def _current_column_index(self) -> int | None:
        focused = self.screen.focused
        if not isinstance(focused, TaskCard):
            return None
        for i, (col_id, _, _) in enumerate(KANBAN_COLUMNS):
            col = self._columns[col_id]
            if focused in col.query(TaskCard):
                return i
        return None

    def _move_focus(self, direction: int) -> None:
        focused = self.screen.focused
        if not isinstance(focused, TaskCard):
            self._focus_first_available()
            return

        col_idx = self._current_column_index()
        if col_idx is None:
            return

        col_id = KANBAN_COLUMNS[col_idx][0]
        cards = list(self._columns[col_id].query(TaskCard))
        if not cards:
            return

        try:
            current = cards.index(focused)
        except ValueError:
            return

        new_idx = max(0, min(len(cards) - 1, current + direction))
        cards[new_idx].focus()

    def _move_column(self, direction: int) -> None:
        col_idx = self._current_column_index()
        if col_idx is None:
            col_idx = 0 if direction > 0 else len(KANBAN_COLUMNS) - 1
        else:
            col_idx += direction

        col_count = len(KANBAN_COLUMNS)
        col_idx = col_idx % col_count

        for _ in range(col_count):
            col_id = KANBAN_COLUMNS[col_idx][0]
            cards = list(self._columns[col_id].query(TaskCard))
            if cards:
                cards[0].focus()
                return
            col_idx = (col_idx + direction) % col_count

    def _focus_first_available(self) -> None:
        for col_id, _, _ in KANBAN_COLUMNS:
            cards = list(self._columns[col_id].query(TaskCard))
            if cards:
                cards[0].focus()
                return
