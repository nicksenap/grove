"""Workspace state management (~/.grove/state.json)."""

from __future__ import annotations

import contextlib
import json
import os
import tempfile
from pathlib import Path

from grove.config import GROVE_DIR
from grove.models import Workspace

STATE_PATH = GROVE_DIR / "state.json"


def _load_raw() -> list[dict]:
    """Load raw state data."""
    if not STATE_PATH.exists():
        return []
    try:
        return json.loads(STATE_PATH.read_text())
    except json.JSONDecodeError as e:
        raise SystemExit(
            f"State file is corrupt ({STATE_PATH}): {e}\n"
            "Delete the file to reset, or run: gw doctor --fix"
        ) from e


def _atomic_write(path: Path, content: str) -> None:
    """Write *content* to *path* atomically via temp file + rename."""
    fd, tmp = tempfile.mkstemp(dir=path.parent, suffix=".tmp")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(content)
        os.replace(tmp, path)
    except BaseException:
        with contextlib.suppress(OSError):
            os.unlink(tmp)
        raise


def _save_raw(data: list[dict]) -> None:
    """Save raw state data atomically (write to temp, then replace)."""
    GROVE_DIR.mkdir(parents=True, exist_ok=True)
    _atomic_write(STATE_PATH, json.dumps(data, indent=2) + "\n")


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


def update_workspace(workspace: Workspace, *, match_name: str | None = None) -> None:
    """Replace an existing workspace in state.

    Matches by *match_name* if given, otherwise by ``workspace.name``.
    This allows atomic renames (match old name, write new name) in one operation.
    """
    key = match_name or workspace.name
    workspaces = load_workspaces()
    workspaces = [workspace if w.name == key else w for w in workspaces]
    save_workspaces(workspaces)


def find_workspace_by_path(path: Path) -> Workspace | None:
    """Find a workspace that contains the given path."""
    resolved = path.resolve()
    for ws in load_workspaces():
        ws_resolved = ws.path.resolve()
        if resolved == ws_resolved or ws_resolved in resolved.parents:
            return ws
    return None
