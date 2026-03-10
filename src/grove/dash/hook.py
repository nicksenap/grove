"""Claude Code hook handler — receives events on stdin, updates agent state files.

Usage as a hook command:
    python -m grove.dash.hook --event PreToolUse

Reads JSON from stdin (piped by Claude Code), writes/updates
~/.grove/status/<session_id>.json atomically.
"""

from __future__ import annotations

import contextlib
import json
import os
import re
import subprocess
import sys
import tempfile
from datetime import UTC, datetime
from pathlib import Path

from grove.dash.constants import STATE_DIR_NAME

STATE_DIR = Path.home() / ".grove" / STATE_DIR_NAME
_VALID_ID = re.compile(r"^[a-zA-Z0-9_-]+$")


def _now() -> str:
    return datetime.now(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def _atomic_write(path: Path, data: dict) -> None:
    fd, tmp = tempfile.mkstemp(dir=path.parent, suffix=".tmp")
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(data, f)
        os.replace(tmp, path)
    except BaseException:
        with contextlib.suppress(OSError):
            os.unlink(tmp)
        raise


def _git_info(cwd: str) -> tuple[str, int]:
    """Return (branch, dirty_count) for a directory, or empty defaults."""
    if not cwd or not os.path.isdir(cwd):
        return "", 0
    try:
        branch = subprocess.run(
            ["git", "rev-parse", "--abbrev-ref", "HEAD"],
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=5,
        ).stdout.strip()
        if not branch:
            return "", 0
        dirty = subprocess.run(
            ["git", "status", "--porcelain"],
            cwd=cwd,
            capture_output=True,
            text=True,
            timeout=5,
        ).stdout
        return branch, len([line for line in dirty.splitlines() if line.strip()])
    except (subprocess.TimeoutExpired, FileNotFoundError, OSError):
        return "", 0


def _zellij_tab() -> str:
    """Get current Zellij tab name if running inside Zellij."""
    if not os.environ.get("ZELLIJ_SESSION_NAME"):
        return ""
    try:
        subprocess.run(
            ["zellij", "action", "query-tab-names"],
            capture_output=True,
            text=True,
            timeout=3,
        )
        # query-tab-names doesn't indicate which tab is active,
        # so we store the session name for navigation.
        return os.environ.get("ZELLIJ_SESSION_NAME", "")
    except (subprocess.TimeoutExpired, FileNotFoundError, OSError):
        return ""


def _tool_summary(tool_name: str, tool_input: dict | None) -> str | None:
    """Build a human-readable summary of a tool request."""
    if not tool_input:
        return None
    match tool_name:
        case "Bash":
            cmd = tool_input.get("command", "")
            return f"$ {cmd[:300]}" if cmd else None
        case "Edit":
            fp = tool_input.get("file_path", "")
            old = tool_input.get("old_string", "").splitlines()[:3]
            new = tool_input.get("new_string", "").splitlines()[:3]
            parts = [fp]
            parts.extend(f"- {line}" for line in old)
            parts.extend(f"+ {line}" for line in new)
            return "\n".join(parts)
        case "Write":
            fp = tool_input.get("file_path", "")
            content = tool_input.get("content", "")
            lines = len(content.splitlines())
            return f"{fp} ({lines} lines)"
        case "Read":
            return tool_input.get("file_path")
        case "WebFetch":
            return tool_input.get("url")
        case "Grep":
            pat = tool_input.get("pattern", "")
            path = tool_input.get("path", "")
            return f"{pat} in {path}" if path else pat
        case "Glob":
            pat = tool_input.get("pattern", "")
            path = tool_input.get("path", "")
            return f"{pat} in {path}" if path else pat
        case _:
            s = json.dumps(tool_input)
            return s[:300] if len(s) > 300 else s


def _bootstrap(state: dict, session_id: str, cwd: str, now: str) -> dict:
    """Ensure identity fields are present (handles mid-session hook install)."""
    if not state.get("session_id"):
        state["session_id"] = session_id
    if not state.get("cwd") and cwd:
        state["cwd"] = cwd
    if not state.get("project_name") and cwd:
        state["project_name"] = os.path.basename(cwd)
    if not state.get("started_at"):
        state["started_at"] = now
    state.setdefault("status", "IDLE")
    state.setdefault("tool_count", 0)
    state.setdefault("error_count", 0)
    state.setdefault("subagent_count", 0)
    state.setdefault("activity_history", [0] * 10)
    state["pid"] = os.getppid()

    # Zellij context
    zellij_session = os.environ.get("ZELLIJ_SESSION_NAME", "")
    if zellij_session:
        state["zellij_session"] = zellij_session

    return state


def _shift_activity(history: list[int]) -> list[int]:
    """Shift activity window left and increment the last bucket."""
    h = (history or [0] * 10)[-10:]
    while len(h) < 10:
        h.insert(0, 0)
    return h[1:] + [h[-1] + 1]


def handle_event(event: str, input_data: dict) -> None:
    """Process a single Claude Code hook event."""
    STATE_DIR.mkdir(parents=True, exist_ok=True)

    session_id = input_data.get("session_id", "")
    if not session_id or not _VALID_ID.match(session_id):
        return

    state_file = STATE_DIR / f"{session_id}.json"
    now = _now()
    cwd = input_data.get("cwd", "")

    # Load existing state
    state: dict = {}
    if state_file.exists():
        try:
            state = json.loads(state_file.read_text())
        except (json.JSONDecodeError, OSError):
            state = {}

    # Git info
    effective_cwd = cwd or state.get("cwd", "")
    git_branch, git_dirty = _git_info(effective_cwd)

    # Bootstrap identity fields
    state = _bootstrap(state, session_id, cwd, now)
    if git_branch:
        state["git_branch"] = git_branch
        state["git_dirty_count"] = git_dirty

    # Track permission mode (available on every event)
    perm_mode = input_data.get("permission_mode", "")
    if perm_mode:
        state["permission_mode"] = perm_mode

    match event:
        case "SessionStart":
            state = {
                "session_id": session_id,
                "status": "IDLE",
                "cwd": cwd,
                "started_at": now,
                "project_name": os.path.basename(cwd) if cwd else "",
                "model": input_data.get("model", "unknown"),
                "session_source": input_data.get("source", "startup"),
                "permission_mode": input_data.get("permission_mode", ""),
                "last_event": "SessionStart",
                "last_event_time": now,
                "last_tool": None,
                "last_message": None,
                "last_error": None,
                "initial_prompt": None,
                "tool_count": 0,
                "error_count": 0,
                "subagent_count": 0,
                "activity_history": [0] * 10,
                "pid": os.getppid(),
                "git_branch": git_branch,
                "git_dirty_count": git_dirty,
            }
            zellij_session = os.environ.get("ZELLIJ_SESSION_NAME", "")
            if zellij_session:
                state["zellij_session"] = zellij_session

        case "PreToolUse":
            tool = input_data.get("tool_name", "unknown")
            state["status"] = "WORKING"
            state["last_tool"] = tool
            state["last_event"] = "PreToolUse"
            state["last_event_time"] = now
            state["notification_message"] = None
            state["tool_request_summary"] = None
            state["tool_count"] = state.get("tool_count", 0) + 1
            state["activity_history"] = _shift_activity(state.get("activity_history", [0] * 10))

        case "PostToolUse":
            state["status"] = "WORKING"
            state["last_event"] = "PostToolUse"
            state["last_event_time"] = now
            state["notification_message"] = None
            state["tool_request_summary"] = None

        case "PostToolUseFailure":
            state["status"] = "ERROR"
            state["last_event"] = "PostToolUseFailure"
            state["last_event_time"] = now
            state["error_count"] = state.get("error_count", 0) + 1
            state["tool_request_summary"] = None
            error_msg = input_data.get("error", "")
            if error_msg:
                state["last_error"] = error_msg[:500]

        case "Stop":
            # Preserve WAITING_ANSWER (user hasn't responded yet)
            if state.get("status") != "WAITING_ANSWER":
                state["status"] = "IDLE"
            state["last_event"] = "Stop"
            state["last_event_time"] = now
            last_msg = input_data.get("last_assistant_message", "")
            if last_msg:
                state["last_message"] = last_msg[:500]

        case "PermissionRequest":
            tool = input_data.get("tool_name", "unknown")
            state["status"] = "WAITING_PERMISSION"
            state["last_tool"] = tool
            state["last_event"] = "PermissionRequest"
            state["last_event_time"] = now
            state["tool_request_summary"] = _tool_summary(tool, input_data.get("tool_input"))

        case "Notification":
            msg = input_data.get("message", "")
            state["last_event"] = "Notification"
            state["last_event_time"] = now
            state["notification_message"] = msg
            msg_lower = msg.lower()
            if "permission" in msg_lower:
                state["status"] = "WAITING_PERMISSION"
            elif any(kw in msg_lower for kw in ("question", "input", "answer", "elicitation")):
                state["status"] = "WAITING_ANSWER"

        case "UserPromptSubmit":
            state["status"] = "WORKING"
            state["last_event"] = "UserPromptSubmit"
            state["last_event_time"] = now
            state["notification_message"] = None
            state["tool_request_summary"] = None
            prompt = input_data.get("prompt", "")
            if prompt and not state.get("initial_prompt"):
                state["initial_prompt"] = prompt[:300]

        case "SubagentStart":
            state["last_event"] = "SubagentStart"
            state["last_event_time"] = now
            state["subagent_count"] = state.get("subagent_count", 0) + 1
            agent_type = input_data.get("agent_type", "")
            if agent_type:
                active = state.get("active_subagents", [])
                active.append(agent_type)
                state["active_subagents"] = active[-5:]  # keep last 5

        case "SubagentStop":
            state["last_event"] = "SubagentStop"
            state["last_event_time"] = now
            state["subagent_count"] = max(0, state.get("subagent_count", 0) - 1)
            agent_type = input_data.get("agent_type", "")
            if agent_type:
                active = state.get("active_subagents", [])
                if agent_type in active:
                    active.remove(agent_type)
                state["active_subagents"] = active

        case "PreCompact":
            state["last_event"] = "PreCompact"
            state["last_event_time"] = now
            state["compact_count"] = state.get("compact_count", 0) + 1
            state["compact_trigger"] = input_data.get("trigger", "auto")

        case "TaskCompleted":
            state["last_event"] = "TaskCompleted"
            state["last_event_time"] = now

        case "SessionEnd":
            state_file.unlink(missing_ok=True)
            return

        case _:
            return

    _atomic_write(state_file, state)


def main() -> None:
    """CLI entry point: python -m grove.dash.hook --event <EventType>"""
    import argparse

    parser = argparse.ArgumentParser(description="Grove hook handler")
    parser.add_argument("--event", required=True, help="Hook event type")
    args = parser.parse_args()

    try:
        input_data = json.load(sys.stdin)
    except (json.JSONDecodeError, ValueError):
        return

    handle_event(args.event, input_data)


if __name__ == "__main__":
    main()
