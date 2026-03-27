"""Tests for grove.mcp_server — MCP tool functions."""

from __future__ import annotations

from pathlib import Path
from unittest.mock import patch

import pytest

from grove import mcp_server, mcp_store, state
from grove.models import RepoWorktree, Workspace


@pytest.fixture()
def db(tmp_path):
    conn = mcp_store.open_db(tmp_path / "test.db")
    yield conn
    mcp_store.close_db(conn)


@pytest.fixture(autouse=True)
def _set_workspace_id():
    mcp_server._workspace_id = "test-ws"
    yield
    mcp_server._workspace_id = ""


class TestAnnounce:
    def test_publishes_announcement(self, db):
        with patch.object(mcp_server.mcp, "get_context") as mock_ctx:
            mock_ctx.return_value.request_context.lifespan_context.db = db
            result = mcp_server.announce("repo-url", "info", "test message")

        assert "published" in result
        rows = mcp_store.query_announcements(db, "repo-url")
        assert len(rows) == 1
        assert rows[0]["workspace_id"] == "test-ws"

    def test_invalid_category_returns_error(self, db):
        with patch.object(mcp_server.mcp, "get_context") as mock_ctx:
            mock_ctx.return_value.request_context.lifespan_context.db = db
            result = mcp_server.announce("repo-url", "bad", "msg")

        assert "Error" in result

    def test_all_valid_categories(self, db):
        with patch.object(mcp_server.mcp, "get_context") as mock_ctx:
            mock_ctx.return_value.request_context.lifespan_context.db = db
            for cat in mcp_store.VALID_CATEGORIES:
                result = mcp_server.announce("repo-url", cat, f"{cat} message")
                assert "published" in result


class TestGetAnnouncements:
    def test_excludes_own_workspace(self, db):
        mcp_store.insert_announcement(db, "test-ws", "repo-a", "info", "own msg")
        mcp_store.insert_announcement(db, "other-ws", "repo-a", "warning", "other msg")

        with patch.object(mcp_server.mcp, "get_context") as mock_ctx:
            mock_ctx.return_value.request_context.lifespan_context.db = db
            results = mcp_server.get_announcements("repo-a")

        assert len(results) == 1
        assert results[0]["workspace_id"] == "other-ws"

    def test_with_since_filter(self, db):
        db.execute(
            "INSERT INTO announcements (workspace_id, repo_url, category, message, created_at) "
            "VALUES (?, ?, ?, ?, ?)",
            ("other-ws", "repo-a", "info", "old", "2025-01-01T00:00:00"),
        )
        db.execute(
            "INSERT INTO announcements (workspace_id, repo_url, category, message, created_at) "
            "VALUES (?, ?, ?, ?, ?)",
            ("other-ws", "repo-a", "info", "new", "2025-06-01T00:00:00"),
        )
        db.commit()

        with patch.object(mcp_server.mcp, "get_context") as mock_ctx:
            mock_ctx.return_value.request_context.lifespan_context.db = db
            results = mcp_server.get_announcements("repo-a", since="2025-03-01T00:00:00")

        assert len(results) == 1
        assert results[0]["message"] == "new"


class TestListWorkspaces:
    def test_returns_workspace_info(self, tmp_grove):
        ws = Workspace(
            name="ws-1",
            path=Path("/tmp/ws-1"),
            branch="feat/x",
            repos=[
                RepoWorktree(
                    repo_name="repo-a",
                    source_repo=Path("/repos/repo-a"),
                    worktree_path=Path("/tmp/ws-1/repo-a"),
                    branch="feat/x",
                ),
            ],
        )
        state.add_workspace(ws)

        result = mcp_server.list_workspaces()
        assert len(result) == 1
        assert result[0]["name"] == "ws-1"
        assert result[0]["branch"] == "feat/x"
        assert len(result[0]["repos"]) == 1
        assert result[0]["repos"][0]["repo_name"] == "repo-a"

    def test_empty_when_no_workspaces(self, tmp_grove):
        result = mcp_server.list_workspaces()
        assert result == []
