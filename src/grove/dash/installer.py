"""Install/uninstall Grove hooks into Claude Code's settings.json."""

from __future__ import annotations

import json
import shutil
import sys
from datetime import datetime
from pathlib import Path

CLAUDE_SETTINGS = Path.home() / ".claude" / "settings.json"

# All hook event types Claude Code supports
HOOK_EVENTS = [
    "PreToolUse",
    "PostToolUse",
    "PostToolUseFailure",
    "Stop",
    "SessionStart",
    "SessionEnd",
    "Notification",
    "PermissionRequest",
    "UserPromptSubmit",
    "SubagentStart",
    "SubagentStop",
    "PreCompact",
    "TaskCompleted",
]

# Marker to identify our hooks
_GROVE_MARKER = "gw _hook"
# Legacy marker from pre-v0.13 (used sys.executable + python -m grove.dash).
# Needed so uninstall can clean up old hooks. Safe to remove in v0.14+.
_LEGACY_MARKER = "grove.dash"


def _hook_command(event: str) -> str:
    """Build the hook command string for a given event.

    Uses the ``gw`` console-script entry point (stable across upgrades)
    rather than the versioned Python interpreter path, which breaks when
    Homebrew or uv upgrades Grove to a new version.
    """
    gw = _resolve_gw()
    return f"GROVE_EVENT={event} {gw} _hook --event {event}"


def _resolve_gw() -> str:
    """Find the stable ``gw`` binary path.

    Prefers ``shutil.which`` so we get the canonical PATH entry (e.g.
    /opt/homebrew/bin/gw) rather than a version-specific Cellar path.
    Falls back to ``sys.executable -m grove.dash`` for editable/dev installs
    where ``gw`` might not be on PATH.
    """
    gw_path = shutil.which("gw")
    if gw_path:
        return gw_path
    # Fallback for dev installs — multi-token string, interpreted by shell
    return f"{sys.executable} -m grove.dash"


def _is_grove_hook(hook: dict) -> bool:
    """Check if a hook entry belongs to Grove."""
    cmd = hook.get("command", "")
    return _GROVE_MARKER in cmd or _LEGACY_MARKER in cmd


def install_hooks(dry_run: bool = False) -> dict[str, list[str]]:
    """Install Grove hooks into Claude Code settings.

    Returns a dict of {event: [action]} describing what was done.
    """
    actions: dict[str, list[str]] = {}

    # Load existing settings
    settings: dict = {}
    if CLAUDE_SETTINGS.exists():
        try:
            settings = json.loads(CLAUDE_SETTINGS.read_text())
        except (json.JSONDecodeError, OSError):
            settings = {}

    hooks = settings.setdefault("hooks", {})

    for event in HOOK_EVENTS:
        command = _hook_command(event)
        # Claude Code expects: {"matcher": "", "hooks": [{"type": "command", ...}]}
        rule_entry = {
            "matcher": "",
            "hooks": [{"type": "command", "command": command}],
        }

        event_hooks = hooks.get(event, [])
        if not isinstance(event_hooks, list):
            event_hooks = []

        # Find existing grove rule
        grove_idx = None
        for i, rule in enumerate(event_hooks):
            if not isinstance(rule, dict):
                continue
            for inner in rule.get("hooks", []):
                if isinstance(inner, dict) and _is_grove_hook(inner):
                    grove_idx = i
                    break
            if grove_idx is not None:
                break

        if grove_idx is not None:
            event_hooks[grove_idx] = rule_entry
            actions.setdefault(event, []).append("updated")
        else:
            event_hooks.append(rule_entry)
            actions.setdefault(event, []).append("added")

        hooks[event] = event_hooks

    settings["hooks"] = hooks

    if not dry_run:
        # Backup existing settings
        if CLAUDE_SETTINGS.exists():
            ts = datetime.now().strftime("%Y%m%d_%H%M%S")
            backup = CLAUDE_SETTINGS.with_suffix(f".bak.{ts}")
            shutil.copy2(CLAUDE_SETTINGS, backup)

        CLAUDE_SETTINGS.parent.mkdir(parents=True, exist_ok=True)
        CLAUDE_SETTINGS.write_text(json.dumps(settings, indent=2) + "\n")

    return actions


def uninstall_hooks() -> int:
    """Remove all Grove hooks from Claude Code settings. Returns count removed."""
    if not CLAUDE_SETTINGS.exists():
        return 0

    try:
        settings = json.loads(CLAUDE_SETTINGS.read_text())
    except (json.JSONDecodeError, OSError):
        return 0

    hooks = settings.get("hooks", {})
    removed = 0

    for event in list(hooks.keys()):
        event_hooks = hooks[event]
        if not isinstance(event_hooks, list):
            continue

        def _is_grove_rule(rule: dict) -> bool:
            # Nested format: {"matcher": "", "hooks": [...]}
            if any(_is_grove_hook(h) for h in rule.get("hooks", [])):
                return True
            # Legacy flat format: {"type": "command", "command": "..."}
            return "command" in rule and "hooks" not in rule and _is_grove_hook(rule)

        filtered = [
            rule for rule in event_hooks if not (isinstance(rule, dict) and _is_grove_rule(rule))
        ]
        removed += len(event_hooks) - len(filtered)

        if filtered:
            hooks[event] = filtered
        else:
            del hooks[event]

    if removed > 0:
        if hooks:
            settings["hooks"] = hooks
        else:
            del settings["hooks"]
        CLAUDE_SETTINGS.write_text(json.dumps(settings, indent=2) + "\n")

    return removed


def is_installed() -> bool:
    """Check if Grove hooks are installed."""
    if not CLAUDE_SETTINGS.exists():
        return False
    try:
        settings = json.loads(CLAUDE_SETTINGS.read_text())
    except (json.JSONDecodeError, OSError):
        return False

    hooks = settings.get("hooks", {})
    for event_hooks in hooks.values():
        if isinstance(event_hooks, list):
            for rule in event_hooks:
                if isinstance(rule, dict):
                    for h in rule.get("hooks", []):
                        if isinstance(h, dict) and _is_grove_hook(h):
                            return True
    return False
