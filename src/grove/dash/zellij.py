"""Zellij terminal multiplexer integration — jump to tabs, send keys."""

from __future__ import annotations

import logging
import os
import re
import subprocess

log = logging.getLogger("grove.dash")


def is_available() -> bool:
    """Check if we're running inside Zellij."""
    return bool(os.environ.get("ZELLIJ_SESSION_NAME"))


def list_tab_names() -> list[str]:
    """Return all tab names in the current Zellij session."""
    try:
        result = subprocess.run(
            ["zellij", "action", "query-tab-names"],
            capture_output=True,
            text=True,
            timeout=3,
        )
        if result.returncode != 0:
            return []
        return [line.strip() for line in result.stdout.splitlines() if line.strip()]
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return []


def go_to_tab_name(name: str) -> bool:
    """Switch to a Zellij tab by name. Returns True on success."""
    try:
        result = subprocess.run(
            ["zellij", "action", "go-to-tab-name", name],
            capture_output=True,
            timeout=3,
        )
        return result.returncode == 0
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return False


def go_to_tab_index(index: int) -> bool:
    """Switch to a Zellij tab by 1-based index."""
    try:
        result = subprocess.run(
            ["zellij", "action", "go-to-tab", str(index)],
            capture_output=True,
            timeout=3,
        )
        return result.returncode == 0
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return False


def write_chars(text: str) -> bool:
    """Send text to the focused pane."""
    try:
        result = subprocess.run(
            ["zellij", "action", "write-chars", text],
            capture_output=True,
            timeout=3,
        )
        return result.returncode == 0
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return False


def send_enter() -> bool:
    """Send Enter key (carriage return) to the focused pane."""
    try:
        result = subprocess.run(
            ["zellij", "action", "write", "13"],
            capture_output=True,
            timeout=3,
        )
        return result.returncode == 0
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return False


def approve() -> bool:
    """Send 'y' + Enter to the focused pane (approve a permission request)."""
    return write_chars("y") and send_enter()


def deny() -> bool:
    """Send 'n' + Enter to the focused pane (deny a permission request)."""
    return write_chars("n") and send_enter()


def _extract_workspace_name(cwd: str) -> str:
    """Extract Grove workspace name from a CWD path.

    For paths like ~/.grove/workspaces/feat-purchase-bulk-import/some-repo,
    returns 'feat-purchase-bulk-import'.
    """
    parts = cwd.split("/")
    try:
        idx = parts.index("workspaces")
        if idx + 1 < len(parts):
            return parts[idx + 1]
    except ValueError:
        pass
    return ""


def _tab_cwd_map() -> dict[str, str]:
    """Parse Zellij layout to build {tab_name: cwd} mapping.

    Uses dump-layout and extracts tab names + CWDs from the KDL output.
    Resolves relative CWDs against the layout-level base CWD.
    """
    try:
        result = subprocess.run(
            ["zellij", "action", "dump-layout"],
            capture_output=True,
            text=True,
            timeout=5,
        )
        if result.returncode != 0:
            return {}
    except (FileNotFoundError, subprocess.TimeoutExpired, OSError):
        return {}

    mapping: dict[str, str] = {}
    current_tab = ""
    base_cwd = ""
    # Simple KDL parsing: find tab names and first cwd per tab
    for line in result.stdout.splitlines():
        # Top-level layout cwd (before any tab)
        if not current_tab and not base_cwd:
            base_match = re.search(r'^\s+cwd\s+"([^"]+)"', line)
            if base_match:
                base_cwd = base_match.group(1)
                continue
        tab_match = re.search(r'tab\s+name="([^"]+)"', line)
        if tab_match:
            current_tab = tab_match.group(1)
            continue
        if current_tab and current_tab not in mapping:
            cwd_match = re.search(r'cwd="([^"]+)"', line)
            if cwd_match:
                cwd = cwd_match.group(1)
                # Resolve relative CWDs against base
                if not os.path.isabs(cwd) and base_cwd:
                    cwd = os.path.join(base_cwd, cwd)
                mapping[current_tab] = cwd

    return mapping


def jump_to_agent(project_name: str, cwd: str = "") -> bool:
    """Jump to an agent's Zellij tab.

    Matching strategy (in order):
    1. Exact tab name match on project_name
    2. Case-insensitive tab name match
    3. Tab name contains workspace name from CWD
    4. Tab CWD contains project_name (via dump-layout)
    5. Substring match on project_name
    """
    log.info("ZELLIJ jump: project=%r cwd=%r", project_name, cwd)

    tabs = list_tab_names()
    log.info("ZELLIJ tabs: %s", tabs)
    if not tabs:
        log.info("ZELLIJ: no tabs found")
        return False

    # 1. Exact match
    if project_name in tabs:
        log.info("ZELLIJ: exact match on %r", project_name)
        return go_to_tab_name(project_name)

    # 2. Case-insensitive match
    lower = project_name.lower()
    for tab in tabs:
        if tab.lower() == lower:
            log.info("ZELLIJ: case-insensitive match %r", tab)
            return go_to_tab_name(tab)

    # 3. Match workspace name from CWD against tab names
    if cwd:
        ws_name = _extract_workspace_name(cwd)
        log.info("ZELLIJ: workspace_name=%r from cwd=%r", ws_name, cwd)
        if ws_name:
            ws_lower = ws_name.lower()
            for tab in tabs:
                if tab.lower() == ws_lower:
                    log.info("ZELLIJ: ws exact match %r", tab)
                    return go_to_tab_name(tab)
            # Also try substring
            for tab in tabs:
                if ws_lower in tab.lower() or tab.lower() in ws_lower:
                    log.info("ZELLIJ: ws substring match %r", tab)
                    return go_to_tab_name(tab)

    # 4. Parse layout for CWD matching
    tab_cwds = _tab_cwd_map()
    log.info("ZELLIJ: tab_cwds=%s", tab_cwds)
    # 4a. Direct CWD path match (agent cwd is under tab cwd, or vice versa)
    for tab_name, tab_cwd in tab_cwds.items():
        if cwd and (
            cwd.startswith(tab_cwd + "/") or cwd == tab_cwd or tab_cwd.startswith(cwd + "/")
        ):
            log.info("ZELLIJ: cwd match tab=%r tab_cwd=%r", tab_name, tab_cwd)
            return go_to_tab_name(tab_name)
    # 4b. Project name matches a path component in the tab's CWD
    for tab_name, tab_cwd in tab_cwds.items():
        tab_parts = tab_cwd.lower().split("/")
        if project_name.lower() in tab_parts:
            log.info("ZELLIJ: project component match tab=%r", tab_name)
            return go_to_tab_name(tab_name)

    # 5. Substring match
    for tab in tabs:
        if lower in tab.lower():
            log.info("ZELLIJ: substring match %r", tab)
            return go_to_tab_name(tab)

    log.info("ZELLIJ: no match found")
    return False
