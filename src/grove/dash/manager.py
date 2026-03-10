"""State manager — scans and cleans agent state files."""

from __future__ import annotations

from pathlib import Path

from grove.config import GROVE_DIR
from grove.dash.constants import STALE_TIMEOUT, STATE_DIR_NAME, AgentStatus
from grove.dash.models import AgentState, StatusSummary, is_pid_alive

STATE_DIR = GROVE_DIR / STATE_DIR_NAME


def ensure_state_dir() -> Path:
    STATE_DIR.mkdir(parents=True, exist_ok=True)
    return STATE_DIR


def _resolve_workspace(agent: AgentState) -> None:
    """Enrich agent with Grove workspace info if CWD is inside a workspace."""
    if not agent.cwd:
        return
    try:
        from grove.state import find_workspace_by_path

        ws = find_workspace_by_path(Path(agent.cwd))
        if ws:
            agent.workspace_name = ws.name
            agent.workspace_branch = ws.branch
            agent.workspace_repos = [r.repo_name for r in ws.repos]
            # Use workspace name as display name instead of bare dirname
            agent.display_name = ws.name
    except Exception:
        pass


def scan() -> tuple[list[AgentState], StatusSummary]:
    """Scan all state files and return agents + summary."""
    if not STATE_DIR.exists():
        return [], StatusSummary()

    agents: list[AgentState] = []
    for path in STATE_DIR.glob("*.json"):
        agent = AgentState.from_json_file(path)
        if agent is not None:
            # Use project_name as display, fall back to session_id
            agent.display_name = agent.project_name or agent.session_id[:12]
            _resolve_workspace(agent)
            agents.append(agent)

    # Sort: needs_attention first, then by project name
    agents.sort(key=lambda a: (not a.needs_attention, (a.display_name or "").lower()))
    return agents, StatusSummary.from_agents(agents)


def cleanup_stale() -> int:
    """Remove state files for dead/stale sessions. Returns count removed."""
    if not STATE_DIR.exists():
        return 0

    removed = 0
    seen_pids: dict[int, Path] = {}

    for path in STATE_DIR.glob("*.json"):
        agent = AgentState.from_json_file(path)
        if agent is None:
            path.unlink(missing_ok=True)
            removed += 1
            continue

        # PID-based cleanup: remove if process is dead
        if agent.pid and not is_pid_alive(agent.pid):
            path.unlink(missing_ok=True)
            removed += 1
            continue

        # Time-based cleanup: remove if idle too long and no PID
        if not agent.pid and agent.idle_seconds > STALE_TIMEOUT:
            path.unlink(missing_ok=True)
            removed += 1
            continue

        # PID dedup: keep the one with higher tool_count
        if agent.pid and agent.pid in seen_pids:
            other_path = seen_pids[agent.pid]
            other = AgentState.from_json_file(other_path)
            if other and other.tool_count >= agent.tool_count:
                path.unlink(missing_ok=True)
                removed += 1
                continue
            else:
                other_path.unlink(missing_ok=True)
                removed += 1
        if agent.pid:
            seen_pids[agent.pid] = path

    return removed


def reset_stale_permissions() -> int:
    """Clear WAITING_PERMISSION for dead processes."""
    if not STATE_DIR.exists():
        return 0

    reset = 0
    for path in STATE_DIR.glob("*.json"):
        agent = AgentState.from_json_file(path)
        if agent is None:
            continue
        if (
            agent.status == AgentStatus.WAITING_PERMISSION
            and agent.pid
            and not is_pid_alive(agent.pid)
        ):
            path.unlink(missing_ok=True)
            reset += 1

    return reset
