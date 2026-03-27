"""SQLite persistence for cross-workspace announcements."""

from __future__ import annotations

import sqlite3
from pathlib import Path

from grove.config import GROVE_DIR
from grove.git import parse_remote_name

DB_PATH = GROVE_DIR / "messages.db"

VALID_CATEGORIES = frozenset({"breaking_change", "status", "warning", "info"})

# Announcements older than this are pruned on startup.
_RETENTION_DAYS = 30

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


def normalize_repo_url(url: str) -> str:
    """Normalize a git remote URL to ``owner/repo`` form.

    Handles SSH (``git@host:owner/repo.git``) and HTTPS
    (``https://host/owner/repo.git``) formats.  Falls back to the
    original string if parsing fails.
    """
    parsed = parse_remote_name(url)
    return parsed if parsed else url


def open_db(path: Path | None = None) -> sqlite3.Connection:
    """Open (or create) the announcements database with WAL mode.

    Prunes announcements older than ``_RETENTION_DAYS`` on each open.
    """
    db_path = path or DB_PATH
    db_path.parent.mkdir(parents=True, exist_ok=True)
    conn = sqlite3.connect(str(db_path), timeout=5.0)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.executescript(_SCHEMA)
    conn.execute(
        "DELETE FROM announcements WHERE created_at < datetime('now', ?)",
        (f"-{_RETENTION_DAYS} days",),
    )
    conn.commit()
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
    """Insert an announcement and return its id.

    The *repo_url* is normalized to ``owner/repo`` form so that SSH and
    HTTPS URLs for the same repo match.
    """
    if category not in VALID_CATEGORIES:
        raise ValueError(
            f"Invalid category {category!r} — must be one of {sorted(VALID_CATEGORIES)}"
        )
    repo_key = normalize_repo_url(repo_url)
    cur = conn.execute(
        "INSERT INTO announcements (workspace_id, repo_url, category, message) VALUES (?, ?, ?, ?)",
        (workspace_id, repo_key, category, message),
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
    """Query announcements for a repo, optionally excluding own workspace.

    The *repo_url* is normalized before querying so that different URL
    forms (SSH vs HTTPS) for the same repo match.
    """
    repo_key = normalize_repo_url(repo_url)
    clauses = ["repo_url = ?"]
    params: list[str | int] = [repo_key]

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
