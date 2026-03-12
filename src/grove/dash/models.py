"""Data models for agent state tracking."""

from __future__ import annotations

import json
import os
import time
from dataclasses import dataclass, field
from datetime import UTC, datetime
from pathlib import Path

from grove.dash.constants import ATTENTION_STATUSES, SPARK_CHARS, AgentStatus


@dataclass
class AgentState:
    """State of a single Claude Code agent session."""

    session_id: str
    status: AgentStatus = AgentStatus.IDLE
    cwd: str = ""
    project_name: str = ""
    model: str = ""
    started_at: str = ""
    last_event: str = ""
    last_event_time: str = ""
    last_tool: str = ""
    tool_count: int = 0
    error_count: int = 0
    subagent_count: int = 0
    compact_count: int = 0
    pid: int = 0
    git_branch: str = ""
    git_dirty_count: int = 0
    notification_message: str | None = None
    tool_request_summary: str | None = None
    activity_history: list[int] = field(default_factory=lambda: [0] * 10)
    # Zellij context
    zellij_session: str = ""
    # Extended fields
    permission_mode: str = ""
    session_source: str = ""
    last_message: str = ""
    last_error: str = ""
    initial_prompt: str = ""
    compact_trigger: str = ""
    active_subagents: list[str] = field(default_factory=list)
    # Resolved by manager (not persisted)
    display_name: str = ""
    workspace_name: str = ""
    workspace_branch: str = ""
    workspace_repos: list[str] = field(default_factory=list)

    @property
    def needs_attention(self) -> bool:
        return self.status in ATTENTION_STATUSES

    @property
    def uptime(self) -> str:
        if not self.started_at:
            return ""
        try:
            start = datetime.fromisoformat(self.started_at)
            delta = datetime.now(UTC) - start
            secs = int(delta.total_seconds())
            if secs < 60:
                return f"{secs}s"
            if secs < 3600:
                return f"{secs // 60}m"
            return f"{secs // 3600}h{(secs % 3600) // 60}m"
        except (ValueError, TypeError):
            return ""

    @property
    def idle_seconds(self) -> float:
        if not self.last_event_time:
            return 0
        try:
            last = datetime.fromisoformat(self.last_event_time)
            return (datetime.now(UTC) - last).total_seconds()
        except (ValueError, TypeError):
            return 0

    @property
    def sparkline(self) -> str:
        if not self.activity_history:
            return ""
        mx = max(self.activity_history) or 1
        return "".join(SPARK_CHARS[min(int(v / mx * 8), 8)] for v in self.activity_history)

    @classmethod
    def from_json_file(cls, path: Path) -> AgentState | None:
        try:
            data = json.loads(path.read_text())
        except (json.JSONDecodeError, OSError):
            return None
        status_raw = data.get("status", "IDLE")
        try:
            status = AgentStatus(status_raw)
        except ValueError:
            status = AgentStatus.IDLE
        return cls(
            session_id=data.get("session_id", path.stem),
            status=status,
            cwd=data.get("cwd", ""),
            project_name=data.get("project_name", ""),
            model=data.get("model", ""),
            started_at=data.get("started_at", ""),
            last_event=data.get("last_event", ""),
            last_event_time=data.get("last_event_time", ""),
            last_tool=data.get("last_tool", ""),
            tool_count=data.get("tool_count", 0),
            error_count=data.get("error_count", 0),
            subagent_count=data.get("subagent_count", 0),
            compact_count=data.get("compact_count", 0),
            pid=data.get("pid", 0),
            git_branch=data.get("git_branch", ""),
            git_dirty_count=data.get("git_dirty_count", 0),
            notification_message=data.get("notification_message"),
            tool_request_summary=data.get("tool_request_summary"),
            activity_history=data.get("activity_history", [0] * 10),
            zellij_session=data.get("zellij_session", ""),
            permission_mode=data.get("permission_mode", ""),
            session_source=data.get("session_source", ""),
            last_message=data.get("last_message", "") or "",
            last_error=data.get("last_error", "") or "",
            initial_prompt=data.get("initial_prompt", "") or "",
            compact_trigger=data.get("compact_trigger", ""),
            active_subagents=data.get("active_subagents", []),
        )


@dataclass
class StatusSummary:
    """Aggregate counts across all agents."""

    total: int = 0
    working: int = 0
    idle: int = 0
    waiting_perm: int = 0
    waiting_answer: int = 0
    error: int = 0

    @classmethod
    def from_agents(cls, agents: list[AgentState]) -> StatusSummary:
        s = cls(total=len(agents))
        for a in agents:
            match a.status:
                case AgentStatus.WORKING:
                    s.working += 1
                case AgentStatus.IDLE:
                    s.idle += 1
                case AgentStatus.WAITING_PERMISSION:
                    s.waiting_perm += 1
                case AgentStatus.WAITING_ANSWER:
                    s.waiting_answer += 1
                case AgentStatus.ERROR:
                    s.error += 1
        return s

    @property
    def status_line(self) -> str:
        parts: list[str] = []
        if self.working:
            parts.append(f"W:{self.working}")
        if self.waiting_perm:
            parts.append(f"[!]:{self.waiting_perm}")
        if self.waiting_answer:
            parts.append(f"?:{self.waiting_answer}")
        if self.error:
            parts.append(f"E:{self.error}")
        if self.idle:
            parts.append(f"I:{self.idle}")
        return " ".join(parts) if parts else "no agents"


@dataclass
class ClaudeUsage:
    """Claude usage data from the Usage Tracker cache."""

    utilization: int = 0  # 0–100%
    resets_at: str = ""  # ISO timestamp
    profile_name: str = ""
    stale: bool = False  # True if cache is older than 10 minutes

    _CACHE_PATH = Path.home() / ".claude" / ".statusline-usage-cache"
    _STALE_SECONDS = 600  # 10 minutes

    @classmethod
    def read_cache(cls) -> ClaudeUsage | None:
        """Read usage data from the Claude Usage Tracker cache file."""
        try:
            text = cls._CACHE_PATH.read_text()
        except OSError:
            return None

        vals: dict[str, str] = {}
        for line in text.strip().splitlines():
            if "=" in line:
                k, _, v = line.partition("=")
                vals[k.strip()] = v.strip()

        if "UTILIZATION" not in vals:
            return None

        ts = int(vals.get("TIMESTAMP", "0"))
        stale = (time.time() - ts) > cls._STALE_SECONDS if ts else True

        return cls(
            utilization=int(vals.get("UTILIZATION", "0")),
            resets_at=vals.get("RESETS_AT", ""),
            profile_name=vals.get("PROFILE_NAME", ""),
            stale=stale,
        )

    @property
    def reset_countdown(self) -> str:
        """Human-friendly countdown to reset, e.g. '1h32m'."""
        if not self.resets_at:
            return ""
        try:
            reset = datetime.fromisoformat(self.resets_at)
            now = datetime.now(UTC)
            delta = reset - now
            secs = int(delta.total_seconds())
            if secs <= 0:
                return "now"
            if secs < 60:
                return f"{secs}s"
            if secs < 3600:
                return f"{secs // 60}m"
            return f"{secs // 3600}h{(secs % 3600) // 60:02d}m"
        except (ValueError, TypeError):
            return ""

    @property
    def bar(self) -> str:
        """10-block progress bar using block characters."""
        filled = self.utilization // 10
        return "▓" * filled + "░" * (10 - filled)


def is_pid_alive(pid: int) -> bool:
    if pid <= 0:
        return False
    try:
        os.kill(pid, 0)
        return True
    except (OSError, ProcessLookupError):
        return False
