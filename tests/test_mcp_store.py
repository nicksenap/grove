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
        # URL is stored normalized
        assert rows[0]["repo_url"] == "org/repo"

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


class TestNormalizeRepoUrl:
    def test_ssh_url(self):
        assert mcp_store.normalize_repo_url("git@github.com:org/repo.git") == "org/repo"

    def test_https_url(self):
        assert mcp_store.normalize_repo_url("https://github.com/org/repo.git") == "org/repo"

    def test_https_no_git_suffix(self):
        assert mcp_store.normalize_repo_url("https://github.com/org/repo") == "org/repo"

    def test_unparseable_falls_back(self):
        assert mcp_store.normalize_repo_url("just-a-string") == "just-a-string"

    def test_ssh_and_https_match(self, db):
        """Announcements via SSH URL are visible when querying via HTTPS URL."""
        mcp_store.insert_announcement(db, "ws-1", "git@github.com:org/repo.git", "info", "ssh msg")
        rows = mcp_store.query_announcements(db, "https://github.com/org/repo.git")
        assert len(rows) == 1
        assert rows[0]["message"] == "ssh msg"

    def test_https_and_ssh_match(self, db):
        """Announcements via HTTPS URL are visible when querying via SSH URL."""
        mcp_store.insert_announcement(
            db, "ws-1", "https://github.com/org/repo", "warning", "https msg"
        )
        rows = mcp_store.query_announcements(db, "git@github.com:org/repo.git")
        assert len(rows) == 1
        assert rows[0]["message"] == "https msg"


class TestRetention:
    def test_prunes_old_announcements_on_open(self, tmp_path):
        """Announcements older than _RETENTION_DAYS are pruned on open_db."""
        db_path = tmp_path / "prune.db"
        conn = mcp_store.open_db(db_path)

        # Insert an announcement dated 60 days ago
        conn.execute(
            "INSERT INTO announcements (workspace_id, repo_url, category, message, created_at) "
            "VALUES (?, ?, ?, ?, datetime('now', '-60 days'))",
            ("ws-1", "repo-a", "info", "ancient"),
        )
        # Insert a recent one
        mcp_store.insert_announcement(conn, "ws-1", "repo-a", "info", "recent")
        conn.close()

        # Re-open — should prune the old one
        conn2 = mcp_store.open_db(db_path)
        rows = mcp_store.query_announcements(conn2, "repo-a")
        assert len(rows) == 1
        assert rows[0]["message"] == "recent"
        conn2.close()

    def test_keeps_recent_announcements(self, tmp_path):
        db_path = tmp_path / "keep.db"
        conn = mcp_store.open_db(db_path)

        conn.execute(
            "INSERT INTO announcements (workspace_id, repo_url, category, message, created_at) "
            "VALUES (?, ?, ?, ?, datetime('now', '-5 days'))",
            ("ws-1", "repo-a", "info", "five days ago"),
        )
        conn.commit()
        conn.close()

        conn2 = mcp_store.open_db(db_path)
        rows = mcp_store.query_announcements(conn2, "repo-a")
        assert len(rows) == 1
        conn2.close()
