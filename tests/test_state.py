"""Tests for grove.state — JSON state management."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from grove import state
from grove.models import Workspace


class TestStateOperations:
    def test_load_empty(self, tmp_grove):
        workspaces = state.load_workspaces()
        assert workspaces == []

    def test_add_and_get(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        ws = state.get_workspace("test-ws")
        assert ws is not None
        assert ws.name == "test-ws"
        assert ws.branch == "feat/test"

    def test_get_nonexistent(self, tmp_grove):
        assert state.get_workspace("nope") is None

    def test_remove(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        state.remove_workspace("test-ws")
        assert state.get_workspace("test-ws") is None

    def test_remove_nonexistent_is_noop(self, tmp_grove):
        state.remove_workspace("nope")  # Should not raise
        assert state.load_workspaces() == []

    def test_find_by_path_exact(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        ws = state.find_workspace_by_path(sample_workspace.path)
        assert ws is not None
        assert ws.name == "test-ws"

    def test_find_by_path_subdir(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        subdir = sample_workspace.path / "svc-auth"
        subdir.mkdir(exist_ok=True)
        ws = state.find_workspace_by_path(subdir)
        assert ws is not None
        assert ws.name == "test-ws"

    def test_find_by_path_not_found(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        ws = state.find_workspace_by_path(Path("/completely/different"))
        assert ws is None

    def test_multiple_workspaces(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)

        ws2_path = tmp_grove["workspace_dir"] / "second"
        ws2_path.mkdir()
        ws2 = Workspace(
            name="second",
            path=ws2_path,
            branch="feat/other",
            repos=[],
        )
        state.add_workspace(ws2)

        all_ws = state.load_workspaces()
        assert len(all_ws) == 2
        assert {w.name for w in all_ws} == {"test-ws", "second"}

    def test_state_persists_as_json(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        raw = json.loads(tmp_grove["state_path"].read_text())
        assert len(raw) == 1
        assert raw[0]["name"] == "test-ws"

    def test_corrupt_json_gives_helpful_error(self, tmp_grove):
        tmp_grove["state_path"].write_text("{not valid json")
        with pytest.raises(SystemExit, match="corrupt"):
            state.load_workspaces()

    def test_atomic_write_produces_valid_json(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        # Verify the file is valid JSON (not truncated)
        raw = json.loads(tmp_grove["state_path"].read_text())
        assert isinstance(raw, list)
        assert len(raw) == 1
