"""Workspace state management (~/.grove/state.json)."""

from __future__ import annotations

import json
from pathlib import Path

from grove.config import GROVE_DIR
from grove.models import Workspace

STATE_PATH = GROVE_DIR / "state.json"


def _load_raw() -> list[dict]:
    """Load raw state data."""
    if not STATE_PATH.exists():
        return []
    return json.loads(STATE_PATH.read_text())


def _save_raw(data: list[dict]) -> None:
    """Save raw state data."""
    GROVE_DIR.mkdir(parents=True, exist_ok=True)
    STATE_PATH.write_text(json.dumps(data, indent=2) + "\n")


def load_workspaces() -> list[Workspace]:
    """Load all workspaces from state."""
    return [Workspace.from_dict(d) for d in _load_raw()]


def save_workspaces(workspaces: list[Workspace]) -> None:
    """Save all workspaces to state."""
    _save_raw([w.to_dict() for w in workspaces])


def get_workspace(name: str) -> Workspace | None:
    """Get a workspace by name."""
    for ws in load_workspaces():
        if ws.name == name:
            return ws
    return None


def add_workspace(workspace: Workspace) -> None:
    """Add a workspace to state."""
    workspaces = load_workspaces()
    workspaces.append(workspace)
    save_workspaces(workspaces)


def remove_workspace(name: str) -> None:
    """Remove a workspace from state."""
    workspaces = [w for w in load_workspaces() if w.name != name]
    save_workspaces(workspaces)


def find_workspace_by_path(path: Path) -> Workspace | None:
    """Find a workspace that contains the given path."""
    resolved = path.resolve()
    for ws in load_workspaces():
        ws_resolved = ws.path.resolve()
        if resolved == ws_resolved or ws_resolved in resolved.parents:
            return ws
    return None
