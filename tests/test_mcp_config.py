"""Tests for .mcp.json auto-configuration in workspace operations."""

from __future__ import annotations

import json

import pytest

from grove.models import RepoWorktree, Workspace
from grove.workspace import _mcp_server_entry, _remove_mcp_config, _write_mcp_config


@pytest.fixture()
def ws(tmp_path) -> Workspace:
    """Create a workspace with two repo worktrees."""
    ws_path = tmp_path / "ws"
    ws_path.mkdir()
    repo_a = ws_path / "repo-a"
    repo_a.mkdir()
    repo_b = ws_path / "repo-b"
    repo_b.mkdir()

    return Workspace(
        name="test-ws",
        path=ws_path,
        branch="feat/test",
        repos=[
            RepoWorktree(
                repo_name="repo-a",
                source_repo=tmp_path / "src" / "repo-a",
                worktree_path=repo_a,
                branch="feat/test",
            ),
            RepoWorktree(
                repo_name="repo-b",
                source_repo=tmp_path / "src" / "repo-b",
                worktree_path=repo_b,
                branch="feat/test",
            ),
        ],
    )


class TestWriteMcpConfig:
    def test_creates_mcp_json_in_all_paths(self, ws):
        _write_mcp_config(ws)

        for path in [ws.path, ws.repos[0].worktree_path, ws.repos[1].worktree_path]:
            mcp_file = path / ".mcp.json"
            assert mcp_file.exists()
            data = json.loads(mcp_file.read_text())
            assert "grove" in data["mcpServers"]
            assert data["mcpServers"]["grove"]["args"] == [
                "mcp-serve",
                "--workspace",
                "test-ws",
            ]

    def test_merges_into_existing_mcp_json(self, ws):
        # Pre-existing .mcp.json with another server
        existing = {"mcpServers": {"other-server": {"command": "other", "args": []}}}
        mcp_file = ws.path / ".mcp.json"
        mcp_file.write_text(json.dumps(existing))

        _write_mcp_config(ws)

        data = json.loads(mcp_file.read_text())
        assert "other-server" in data["mcpServers"]
        assert "grove" in data["mcpServers"]

    def test_overwrites_existing_grove_entry(self, ws):
        # Pre-existing grove entry with old config
        existing = {"mcpServers": {"grove": {"command": "old", "args": ["old"]}}}
        mcp_file = ws.path / ".mcp.json"
        mcp_file.write_text(json.dumps(existing))

        _write_mcp_config(ws)

        data = json.loads(mcp_file.read_text())
        assert data["mcpServers"]["grove"]["command"] == "gw"


class TestRemoveMcpConfig:
    def test_removes_grove_entry(self, ws):
        _write_mcp_config(ws)
        _remove_mcp_config(ws)

        for path in [ws.path, ws.repos[0].worktree_path]:
            mcp_file = path / ".mcp.json"
            assert not mcp_file.exists()

    def test_preserves_other_servers(self, ws):
        existing = {
            "mcpServers": {
                "other-server": {"command": "other", "args": []},
                "grove": _mcp_server_entry(ws.name),
            }
        }
        mcp_file = ws.path / ".mcp.json"
        mcp_file.write_text(json.dumps(existing))

        _remove_mcp_config(ws)

        data = json.loads(mcp_file.read_text())
        assert "grove" not in data["mcpServers"]
        assert "other-server" in data["mcpServers"]

    def test_no_error_when_no_mcp_json(self, ws):
        # Should not raise
        _remove_mcp_config(ws)
