"""Tests for grove.mcp_store — SQLite announcement persistence."""

from __future__ import annotations

import pytest

from grove import mcp_store


@pytest.fixture()
def db(tmp_path):
    """Open a temporary announcements database."""
    conn = mcp_store.open_db(tmp_path / "test.db")
    yield conn
    mcp_store.close_db(conn)


class TestOpenDb:
    def test_creates_table(self, db):
        tables = db.execute(
            "SELECT name FROM sqlite_master WHERE type='table' AND name='announcements'"
        ).fetchall()
        assert len(tables) == 1

    def test_wal_mode(self, db):
        mode = db.execute("PRAGMA journal_mode").fetchone()[0]
        assert mode == "wal"


class TestInsertAnnouncement:
    def test_insert_and_retrieve(self, db):
        row_id = mcp_store.insert_announcement(
            db, "ws-1", "git@github.com:org/repo.git", "info", "hello world"
        )
        assert row_id >= 1

        rows = mcp_store.query_announcements(db, "git@github.com:org/repo.git")
        assert len(rows) == 1
        assert rows[0]["message"] == "hello world"
        assert rows[0]["workspace_id"] == "ws-1"
        assert rows[0]["category"] == "info"

    def test_invalid_category_raises(self, db):
        with pytest.raises(ValueError, match="Invalid category"):
            mcp_store.insert_announcement(db, "ws-1", "url", "bad_category", "msg")


class TestQueryAnnouncements:
    def test_excludes_own_workspace(self, db):
        mcp_store.insert_announcement(db, "ws-1", "repo-a", "info", "from ws-1")
        mcp_store.insert_announcement(db, "ws-2", "repo-a", "info", "from ws-2")

        rows = mcp_store.query_announcements(db, "repo-a", exclude_workspace="ws-1")
        assert len(rows) == 1
        assert rows[0]["workspace_id"] == "ws-2"

    def test_filters_by_repo(self, db):
        mcp_store.insert_announcement(db, "ws-1", "repo-a", "info", "msg-a")
        mcp_store.insert_announcement(db, "ws-1", "repo-b", "info", "msg-b")

        rows = mcp_store.query_announcements(db, "repo-a")
        assert len(rows) == 1
        assert rows[0]["message"] == "msg-a"

    def test_since_filter(self, db):
        # Insert two announcements with explicit timestamps
        db.execute(
            "INSERT INTO announcements (workspace_id, repo_url, category, message, created_at) "
            "VALUES (?, ?, ?, ?, ?)",
            ("ws-1", "repo-a", "info", "old", "2025-01-01T00:00:00"),
        )
        db.execute(
            "INSERT INTO announcements (workspace_id, repo_url, category, message, created_at) "
            "VALUES (?, ?, ?, ?, ?)",
            ("ws-1", "repo-a", "info", "new", "2025-06-01T00:00:00"),
        )
        db.commit()

        rows = mcp_store.query_announcements(db, "repo-a", since="2025-03-01T00:00:00")
        assert len(rows) == 1
        assert rows[0]["message"] == "new"

    def test_limit(self, db):
        for i in range(10):
            mcp_store.insert_announcement(db, "ws-1", "repo-a", "info", f"msg-{i}")

        rows = mcp_store.query_announcements(db, "repo-a", limit=3)
        assert len(rows) == 3

    def test_ordered_newest_first(self, db):
        db.execute(
            "INSERT INTO announcements (workspace_id, repo_url, category, message, created_at) "
            "VALUES (?, ?, ?, ?, ?)",
            ("ws-1", "repo-a", "info", "first", "2025-01-01T00:00:00"),
        )
        db.execute(
            "INSERT INTO announcements (workspace_id, repo_url, category, message, created_at) "
            "VALUES (?, ?, ?, ?, ?)",
            ("ws-1", "repo-a", "info", "second", "2025-06-01T00:00:00"),
        )
        db.commit()

        rows = mcp_store.query_announcements(db, "repo-a")
        assert rows[0]["message"] == "second"
        assert rows[1]["message"] == "first"
