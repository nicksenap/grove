"""SQLite persistence for cross-workspace announcements."""

from __future__ import annotations

import sqlite3
from pathlib import Path

from grove.config import GROVE_DIR

DB_PATH = GROVE_DIR / "messages.db"

VALID_CATEGORIES = frozenset({"breaking_change", "status", "warning", "info"})

_SCHEMA = """\
CREATE TABLE IF NOT EXISTS announcements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace_id TEXT NOT NULL,
    repo_url TEXT NOT NULL,
    category TEXT NOT NULL,
    message TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_repo_created ON announcements(repo_url, created_at);
"""


def open_db(path: Path | None = None) -> sqlite3.Connection:
    """Open (or create) the announcements database with WAL mode."""
    db_path = path or DB_PATH
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path), timeout=5.0)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.executescript(_SCHEMA)
    return conn


def close_db(conn: sqlite3.Connection) -> None:
    """Close the database connection."""
    conn.close()


def insert_announcement(
    conn: sqlite3.Connection,
    workspace_id: str,
    repo_url: str,
    category: str,
    message: str,
) -> int:
    """Insert an announcement and return its id."""
    if category not in VALID_CATEGORIES:
        raise ValueError(
            f"Invalid category {category!r} — must be one of {sorted(VALID_CATEGORIES)}"
        )
    cur = conn.execute(
        "INSERT INTO announcements (workspace_id, repo_url, category, message) VALUES (?, ?, ?, ?)",
        (workspace_id, repo_url, category, message),
    )
    conn.commit()
    return cur.lastrowid  # type: ignore[return-value]


def query_announcements(
    conn: sqlite3.Connection,
    repo_url: str,
    *,
    exclude_workspace: str | None = None,
    since: str | None = None,
    limit: int = 50,
) -> list[dict]:
    """Query announcements for a repo, optionally excluding own workspace."""
    clauses = ["repo_url = ?"]
    params: list[str | int] = [repo_url]

    if exclude_workspace:
        clauses.append("workspace_id != ?")
        params.append(exclude_workspace)

    if since:
        clauses.append("created_at >= ?")
        params.append(since)

    where = " AND ".join(clauses)
    params.append(limit)

    rows = conn.execute(
        f"SELECT id, workspace_id, repo_url, category, message, created_at "  # noqa: S608
        f"FROM announcements WHERE {where} ORDER BY created_at DESC LIMIT ?",
        params,
    ).fetchall()

    return [dict(row) for row in rows]
