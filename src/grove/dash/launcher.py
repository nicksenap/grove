"""Launch flow — provision workspace, open Zellij tab, start Claude agent."""

from __future__ import annotations

import logging
from pathlib import Path

from grove.dash.store import Task

log = logging.getLogger("grove.dash")


def _sanitize_name(branch: str) -> str:
    """Derive workspace name from branch (matches cli.py logic)."""
    name = branch.replace("/", "-").replace("\\", "-")
    # Strip leading dashes
    return name.lstrip("-") or "workspace"


def launch_task(task: Task) -> tuple[bool, str]:
    """Provision a workspace and open a Zellij tab for a planned task.

    Creates its own TaskStore since this runs in a background thread.
    Returns (success, message).
    """
    from grove import config, discover, workspace
    from grove.dash import zellij
    from grove.dash.constants import AgentStatus
    from grove.dash.store import TaskStore

    store = TaskStore()

    if not task.repos:
        return False, "No repos selected"

    if not task.branch:
        return False, "No branch set"

    # Load config and resolve repo paths
    try:
        cfg = config.load_config()
    except Exception:
        log.exception("Failed to load config")
        return False, "Failed to load Grove config"

    available = discover.find_all_repos(cfg.repo_dirs)
    repo_paths: dict[str, Path] = {}
    missing: list[str] = []
    for repo_name in task.repos:
        if repo_name in available:
            repo_paths[repo_name] = available[repo_name]
        else:
            missing.append(repo_name)

    if missing:
        return False, f"Repos not found: {', '.join(missing)}"

    if not repo_paths:
        return False, "No valid repos to provision"

    # Derive workspace name
    ws_name = _sanitize_name(task.branch)

    # Update task status to provisioning
    store.update_status(task.id, AgentStatus.PROVISIONING)

    # Create workspace — suppress Rich console output from bleeding into the TUI
    import contextlib
    import io

    try:
        with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
            ws = workspace.create_workspace(ws_name, repo_paths, task.branch, cfg)
    except Exception:
        log.exception("Failed to create workspace %s", ws_name)
        store.update_status(task.id, AgentStatus.PLANNED)
        return False, f"Failed to create workspace '{ws_name}'"

    if ws is None:
        store.update_status(task.id, AgentStatus.PLANNED)
        return False, f"Workspace '{ws_name}' already exists or failed"

    # Link workspace to task
    store.update_field(task.id, workspace=ws_name)

    # Open Zellij tab
    ws_path = str(ws.path)
    if zellij.is_available():
        # Build claude command with prompt as positional arg (interactive mode)
        claude_cmd = None
        claude_args = None
        if task.description:
            claude_cmd = "claude"
            claude_args = [task.description]

        if zellij.new_tab(ws_name, ws_path, claude_cmd, claude_args):
            log.info("LAUNCH: opened Zellij tab %r at %s", ws_name, ws_path)
            # Jump back to dashboard tab
            zellij.jump_to_agent("gw dash")
        else:
            log.warning("LAUNCH: failed to open Zellij tab")
    else:
        log.info("LAUNCH: not in Zellij, workspace created at %s", ws_path)

    store.update_status(task.id, AgentStatus.WORKING)
    log.info("LAUNCH: task %r launched as workspace %r", task.title, ws_name)
    return True, f"Launched workspace '{ws_name}'"
