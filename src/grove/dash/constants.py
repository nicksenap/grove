"""Dashboard constants — status enum, colors, sparkline chars."""

from __future__ import annotations

from enum import StrEnum


class AgentStatus(StrEnum):
    PROVISIONING = "PROVISIONING"
    IDLE = "IDLE"
    WORKING = "WORKING"
    WAITING_PERMISSION = "WAITING_PERMISSION"
    WAITING_ANSWER = "WAITING_ANSWER"
    ERROR = "ERROR"
    DONE = "DONE"


# Statuses that require user attention
ATTENTION_STATUSES = {
    AgentStatus.WAITING_PERMISSION,
    AgentStatus.WAITING_ANSWER,
    AgentStatus.ERROR,
}

# --- Gruvbox dark palette ---
GREEN = "#b8bb26"
AQUA = "#8ec07c"
RED = "#fb4934"
YELLOW = "#fabd2f"
GREY = "#928374"
FG = "#fbf1c7"
ORANGE = "#fe8019"
PURPLE = "#d3869b"
BG = "#282828"
BG_LIGHT = "#3c3836"

# Status display: (color, short_label)
BLUE = "#83a598"

STATUS_DISPLAY: dict[AgentStatus, tuple[str, str]] = {
    AgentStatus.PROVISIONING: (AQUA, "PROV"),
    AgentStatus.IDLE: (GREY, "IDLE"),
    AgentStatus.WORKING: (GREEN, "WORK"),
    AgentStatus.WAITING_PERMISSION: (RED, "PERM"),
    AgentStatus.WAITING_ANSWER: (YELLOW, "WAIT"),
    AgentStatus.ERROR: (ORANGE, "ERR"),
    AgentStatus.DONE: (GREEN, "DONE"),
}

# Legacy alias kept for existing code
STATUS_STYLES = {
    AgentStatus.IDLE: ("dim", "idle"),
    AgentStatus.WORKING: ("green", "working"),
    AgentStatus.WAITING_PERMISSION: ("yellow bold", "PERM"),
    AgentStatus.WAITING_ANSWER: ("cyan bold", "INPUT"),
    AgentStatus.ERROR: ("red bold", "ERROR"),
}

# Sparkline characters (braille-based, 0–8)
SPARK_CHARS = " ▁▂▃▄▅▆▇█"

# State directory
STATE_DIR_NAME = "status"

# Poll intervals (seconds)
STATE_POLL_INTERVAL = 0.5
CLEANUP_INTERVAL = 30.0
STALE_TIMEOUT = 1800  # 30 minutes

# Kanban column definitions: (column_id, title, statuses)
KANBAN_COLUMNS: list[tuple[str, str, set[AgentStatus]]] = [
    ("active", "Active", {AgentStatus.WORKING, AgentStatus.PROVISIONING}),
    (
        "attention",
        "Attention",
        {AgentStatus.WAITING_PERMISSION, AgentStatus.WAITING_ANSWER, AgentStatus.ERROR},
    ),
    ("idle", "Idle", {AgentStatus.IDLE}),
    ("done", "Done", {AgentStatus.DONE}),
]
