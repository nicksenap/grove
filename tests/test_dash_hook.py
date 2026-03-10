"""Tests for the dashboard hook handler."""

from __future__ import annotations

import json
from pathlib import Path

import pytest

from grove.dash.hook import handle_event


@pytest.fixture
def state_dir(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> Path:
    """Override STATE_DIR to use a temp directory."""
    import grove.dash.hook as hook_mod

    monkeypatch.setattr(hook_mod, "STATE_DIR", tmp_path)
    return tmp_path


def _read_state(state_dir: Path, session_id: str) -> dict:
    return json.loads((state_dir / f"{session_id}.json").read_text())


class TestSessionLifecycle:
    def test_session_start_creates_state_file(self, state_dir: Path) -> None:
        handle_event(
            "SessionStart",
            {
                "session_id": "s1",
                "cwd": "/tmp/proj",
                "model": "claude-sonnet-4-6",
            },
        )
        state = _read_state(state_dir, "s1")
        assert state["status"] == "IDLE"
        assert state["project_name"] == "proj"
        assert state["model"] == "claude-sonnet-4-6"
        assert state["tool_count"] == 0

    def test_session_end_removes_state_file(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/proj"})
        assert (state_dir / "s1.json").exists()

        handle_event("SessionEnd", {"session_id": "s1"})
        assert not (state_dir / "s1.json").exists()

    def test_invalid_session_id_ignored(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "../escape", "cwd": "/tmp"})
        assert not list(state_dir.glob("*.json"))

    def test_empty_session_id_ignored(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "", "cwd": "/tmp"})
        assert not list(state_dir.glob("*.json"))


class TestToolEvents:
    def test_pre_tool_use_sets_working(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event("PreToolUse", {"session_id": "s1", "tool_name": "Bash"})

        state = _read_state(state_dir, "s1")
        assert state["status"] == "WORKING"
        assert state["last_tool"] == "Bash"
        assert state["tool_count"] == 1

    def test_pre_tool_use_increments_count(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event("PreToolUse", {"session_id": "s1", "tool_name": "Bash"})
        handle_event("PreToolUse", {"session_id": "s1", "tool_name": "Read"})
        handle_event("PreToolUse", {"session_id": "s1", "tool_name": "Edit"})

        state = _read_state(state_dir, "s1")
        assert state["tool_count"] == 3
        assert state["last_tool"] == "Edit"

    def test_post_tool_use_failure_sets_error(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event("PostToolUseFailure", {"session_id": "s1"})

        state = _read_state(state_dir, "s1")
        assert state["status"] == "ERROR"
        assert state["error_count"] == 1

    def test_activity_history_shifts(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event("PreToolUse", {"session_id": "s1", "tool_name": "Bash"})

        state = _read_state(state_dir, "s1")
        history = state["activity_history"]
        assert len(history) == 10
        assert history[-1] == 1  # Incremented
        assert history[0] == 0  # Shifted out


class TestPermissions:
    def test_permission_request(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event(
            "PermissionRequest",
            {
                "session_id": "s1",
                "tool_name": "Bash",
                "tool_input": {"command": "rm -rf /"},
            },
        )

        state = _read_state(state_dir, "s1")
        assert state["status"] == "WAITING_PERMISSION"
        assert state["last_tool"] == "Bash"
        assert state["tool_request_summary"] == "$ rm -rf /"

    def test_stop_preserves_waiting_answer(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event(
            "Notification",
            {
                "session_id": "s1",
                "message": "Please answer this question",
            },
        )
        state = _read_state(state_dir, "s1")
        assert state["status"] == "WAITING_ANSWER"

        handle_event("Stop", {"session_id": "s1"})
        state = _read_state(state_dir, "s1")
        assert state["status"] == "WAITING_ANSWER"

    def test_stop_resets_idle_when_not_waiting(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event("PreToolUse", {"session_id": "s1", "tool_name": "Bash"})
        handle_event("Stop", {"session_id": "s1"})

        state = _read_state(state_dir, "s1")
        assert state["status"] == "IDLE"


class TestNotification:
    def test_permission_keyword(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event(
            "Notification",
            {
                "session_id": "s1",
                "message": "Needs permission to proceed",
            },
        )
        state = _read_state(state_dir, "s1")
        assert state["status"] == "WAITING_PERMISSION"

    def test_question_keyword(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event(
            "Notification",
            {
                "session_id": "s1",
                "message": "I have a question for you",
            },
        )
        state = _read_state(state_dir, "s1")
        assert state["status"] == "WAITING_ANSWER"


class TestSubagents:
    def test_subagent_counting(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event("SubagentStart", {"session_id": "s1"})
        handle_event("SubagentStart", {"session_id": "s1"})

        state = _read_state(state_dir, "s1")
        assert state["subagent_count"] == 2

        handle_event("SubagentStop", {"session_id": "s1"})
        state = _read_state(state_dir, "s1")
        assert state["subagent_count"] == 1

    def test_subagent_stop_floors_at_zero(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event("SubagentStop", {"session_id": "s1"})

        state = _read_state(state_dir, "s1")
        assert state["subagent_count"] == 0


class TestToolSummary:
    def test_bash_summary(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event(
            "PermissionRequest",
            {
                "session_id": "s1",
                "tool_name": "Bash",
                "tool_input": {"command": "echo hello"},
            },
        )
        state = _read_state(state_dir, "s1")
        assert state["tool_request_summary"] == "$ echo hello"

    def test_edit_summary(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event(
            "PermissionRequest",
            {
                "session_id": "s1",
                "tool_name": "Edit",
                "tool_input": {
                    "file_path": "/foo/bar.py",
                    "old_string": "x = 1",
                    "new_string": "x = 2",
                },
            },
        )
        state = _read_state(state_dir, "s1")
        summary = state["tool_request_summary"]
        assert "/foo/bar.py" in summary
        assert "- x = 1" in summary
        assert "+ x = 2" in summary

    def test_grep_summary(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event(
            "PermissionRequest",
            {
                "session_id": "s1",
                "tool_name": "Grep",
                "tool_input": {"pattern": "TODO", "path": "/src"},
            },
        )
        state = _read_state(state_dir, "s1")
        assert state["tool_request_summary"] == "TODO in /src"

    def test_no_tool_input(self, state_dir: Path) -> None:
        handle_event("SessionStart", {"session_id": "s1", "cwd": "/tmp/p"})
        handle_event(
            "PermissionRequest",
            {
                "session_id": "s1",
                "tool_name": "Bash",
            },
        )
        state = _read_state(state_dir, "s1")
        assert state["tool_request_summary"] is None


class TestBootstrap:
    def test_mid_session_hook_install(self, state_dir: Path) -> None:
        """Hooks installed on a running session — first event is PreToolUse."""
        handle_event(
            "PreToolUse",
            {
                "session_id": "s1",
                "cwd": "/tmp/proj",
                "tool_name": "Read",
            },
        )
        state = _read_state(state_dir, "s1")
        assert state["session_id"] == "s1"
        assert state["project_name"] == "proj"
        assert state["status"] == "WORKING"
        assert state["tool_count"] == 1
