"""Tests for dashboard data models."""

from __future__ import annotations

import json
from pathlib import Path

from grove.dash.constants import AgentStatus
from grove.dash.models import AgentState, StatusSummary, is_pid_alive


class TestAgentState:
    def test_needs_attention(self) -> None:
        agent = AgentState(session_id="s1", status=AgentStatus.WAITING_PERMISSION)
        assert agent.needs_attention is True

        agent = AgentState(session_id="s1", status=AgentStatus.WORKING)
        assert agent.needs_attention is False

    def test_sparkline(self) -> None:
        agent = AgentState(session_id="s1", activity_history=[0, 1, 2, 3, 4, 5, 6, 7, 8, 8])
        spark = agent.sparkline
        assert len(spark) == 10
        assert spark[0] == " "  # 0 maps to space

    def test_sparkline_empty(self) -> None:
        agent = AgentState(session_id="s1", activity_history=[])
        assert agent.sparkline == ""

    def test_from_json_file(self, tmp_path: Path) -> None:
        state = {
            "session_id": "abc",
            "status": "WORKING",
            "cwd": "/tmp/proj",
            "project_name": "proj",
            "tool_count": 5,
            "error_count": 1,
            "activity_history": [0, 0, 0, 0, 0, 0, 0, 0, 1, 2],
        }
        path = tmp_path / "abc.json"
        path.write_text(json.dumps(state))

        agent = AgentState.from_json_file(path)
        assert agent is not None
        assert agent.session_id == "abc"
        assert agent.status == AgentStatus.WORKING
        assert agent.tool_count == 5

    def test_from_json_file_invalid(self, tmp_path: Path) -> None:
        path = tmp_path / "bad.json"
        path.write_text("not json{{{")
        assert AgentState.from_json_file(path) is None

    def test_from_json_file_unknown_status(self, tmp_path: Path) -> None:
        path = tmp_path / "s1.json"
        path.write_text(json.dumps({"session_id": "s1", "status": "BANANA"}))
        agent = AgentState.from_json_file(path)
        assert agent is not None
        assert agent.status == AgentStatus.IDLE


class TestStatusSummary:
    def test_from_agents(self) -> None:
        agents = [
            AgentState(session_id="1", status=AgentStatus.WORKING),
            AgentState(session_id="2", status=AgentStatus.WORKING),
            AgentState(session_id="3", status=AgentStatus.IDLE),
            AgentState(session_id="4", status=AgentStatus.WAITING_PERMISSION),
            AgentState(session_id="5", status=AgentStatus.ERROR),
        ]
        s = StatusSummary.from_agents(agents)
        assert s.total == 5
        assert s.working == 2
        assert s.idle == 1
        assert s.waiting_perm == 1
        assert s.error == 1

    def test_status_line_empty(self) -> None:
        s = StatusSummary()
        assert s.status_line == "no agents"

    def test_status_line(self) -> None:
        s = StatusSummary(total=3, working=2, idle=1)
        assert "W:2" in s.status_line
        assert "I:1" in s.status_line


class TestIsPidAlive:
    def test_zero_pid(self) -> None:
        assert is_pid_alive(0) is False

    def test_negative_pid(self) -> None:
        assert is_pid_alive(-1) is False

    def test_bogus_pid(self) -> None:
        assert is_pid_alive(999999999) is False
