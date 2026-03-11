"""SQLite task store for persistent kanban cards."""

from __future__ import annotations

import json
import sqlite3
import uuid
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path

from grove.dash.constants import AgentStatus

DB_PATH = Path.home() / ".grove" / "tasks.db"

_SCHEMA = """
CREATE TABLE IF NOT EXISTS tasks (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    description  TEXT DEFAULT '',
    status       TEXT DEFAULT 'PLANNED',
    branch       TEXT DEFAULT '',
    repos        TEXT DEFAULT '[]',
    workspace    TEXT DEFAULT '',
    session_id   TEXT DEFAULT '',
    column_order INTEGER DEFAULT 0,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    launched_at  TEXT,
    completed_at TEXT,
    config       TEXT DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_session ON tasks(session_id);
"""


def _now() -> str:
    return datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


@dataclass
class Task:
    """A persistent kanban task card."""

    id: str
    title: str
    description: str = ""
    status: AgentStatus = AgentStatus.PLANNED
    branch: str = ""
    repos: list[str] = field(default_factory=list)
    workspace: str = ""
    session_id: str = ""
    column_order: int = 0
    created_at: str = ""
    updated_at: str = ""
    launched_at: str = ""
    completed_at: str = ""
    config: dict = field(default_factory=dict)

    @classmethod
    def create(
        cls,
        title: str,
        description: str = "",
        branch: str = "",
        repos: list[str] | None = None,
        config: dict | None = None,
    ) -> Task:
        """Create a new task with generated ID and timestamps."""
        now = _now()
        return cls(
            id=uuid.uuid4().hex[:12],
            title=title,
            description=description,
            branch=branch,
            repos=repos or [],
            config=config or {},
            created_at=now,
            updated_at=now,
        )


class TaskStore:
    """SQLite-backed task persistence."""

    def __init__(self, db_path: Path | None = None) -> None:
        self._db_path = db_path or DB_PATH
        self._db_path.parent.mkdir(parents=True, exist_ok=True)
        self._conn: sqlite3.Connection | None = None

    def _get_conn(self) -> sqlite3.Connection:
        if self._conn is None:
            self._conn = sqlite3.connect(str(self._db_path))
            self._conn.row_factory = sqlite3.Row
            self._conn.executescript(_SCHEMA)
        return self._conn

    def close(self) -> None:
        if self._conn:
            self._conn.close()
            self._conn = None

    def _row_to_task(self, row: sqlite3.Row) -> Task:
        status_raw = row["status"]
        try:
            status = AgentStatus(status_raw)
        except ValueError:
            status = AgentStatus.PLANNED
        return Task(
            id=row["id"],
            title=row["title"],
            description=row["description"] or "",
            status=status,
            branch=row["branch"] or "",
            repos=json.loads(row["repos"] or "[]"),
            workspace=row["workspace"] or "",
            session_id=row["session_id"] or "",
            column_order=row["column_order"] or 0,
            created_at=row["created_at"] or "",
            updated_at=row["updated_at"] or "",
            launched_at=row["launched_at"] or "",
            completed_at=row["completed_at"] or "",
            config=json.loads(row["config"] or "{}"),
        )

    def add(self, task: Task) -> None:
        """Insert a new task."""
        conn = self._get_conn()
        conn.execute(
            """INSERT INTO tasks
               (id, title, description, status, branch, repos, workspace,
                session_id, column_order, created_at, updated_at,
                launched_at, completed_at, config)
               VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)""",
            (
                task.id,
                task.title,
                task.description,
                task.status.value,
                task.branch,
                json.dumps(task.repos),
                task.workspace,
                task.session_id,
                task.column_order,
                task.created_at,
                task.updated_at,
                task.launched_at,
                task.completed_at,
                json.dumps(task.config),
            ),
        )
        conn.commit()

    def get(self, task_id: str) -> Task | None:
        """Get a task by ID."""
        conn = self._get_conn()
        row = conn.execute("SELECT * FROM tasks WHERE id = ?", (task_id,)).fetchone()
        if row is None:
            return None
        return self._row_to_task(row)

    def list_active(self) -> list[Task]:
        """List all non-archived tasks, ordered by column_order."""
        conn = self._get_conn()
        rows = conn.execute(
            "SELECT * FROM tasks WHERE status != 'ARCHIVED' ORDER BY column_order, created_at",
        ).fetchall()
        return [self._row_to_task(r) for r in rows]

    def list_by_status(self, status: AgentStatus) -> list[Task]:
        """List tasks with a specific status."""
        conn = self._get_conn()
        rows = conn.execute(
            "SELECT * FROM tasks WHERE status = ? ORDER BY column_order, created_at",
            (status.value,),
        ).fetchall()
        return [self._row_to_task(r) for r in rows]

    def update_status(self, task_id: str, status: AgentStatus) -> None:
        """Update a task's status."""
        conn = self._get_conn()
        now = _now()
        extra = {}
        if status == AgentStatus.DONE:
            extra["completed_at"] = now
        conn.execute(
            "UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?",
            (status.value, now, task_id),
        )
        if extra:
            for col, val in extra.items():
                conn.execute(
                    f"UPDATE tasks SET {col} = ? WHERE id = ?",  # noqa: S608
                    (val, task_id),
                )
        conn.commit()

    def update_field(self, task_id: str, **fields: str) -> None:
        """Update arbitrary fields on a task."""
        conn = self._get_conn()
        now = _now()
        fields["updated_at"] = now
        sets = ", ".join(f"{k} = ?" for k in fields)
        vals = list(fields.values()) + [task_id]
        conn.execute(f"UPDATE tasks SET {sets} WHERE id = ?", vals)  # noqa: S608
        conn.commit()

    def link_session(self, task_id: str, session_id: str) -> None:
        """Link an agent session to a task."""
        self.update_field(task_id, session_id=session_id)

    def find_by_session(self, session_id: str) -> Task | None:
        """Find a task linked to a specific agent session."""
        conn = self._get_conn()
        row = conn.execute("SELECT * FROM tasks WHERE session_id = ?", (session_id,)).fetchone()
        if row is None:
            return None
        return self._row_to_task(row)

    def find_by_workspace(self, workspace: str) -> Task | None:
        """Find a task linked to a specific workspace name."""
        conn = self._get_conn()
        row = conn.execute(
            "SELECT * FROM tasks WHERE workspace = ? AND status NOT IN ('DONE', 'ARCHIVED')",
            (workspace,),
        ).fetchone()
        if row is None:
            return None
        return self._row_to_task(row)

    def delete(self, task_id: str) -> None:
        """Delete a task permanently."""
        conn = self._get_conn()
        conn.execute("DELETE FROM tasks WHERE id = ?", (task_id,))
        conn.commit()

    def archive(self, task_id: str) -> None:
        """Archive a task (soft delete)."""
        self.update_status(task_id, AgentStatus.ARCHIVED)
