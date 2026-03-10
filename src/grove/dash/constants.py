"""Dashboard constants — status enum, colors, sparkline chars."""

from __future__ import annotations

from enum import StrEnum


class AgentStatus(StrEnum):
    IDLE = "IDLE"
    WORKING = "WORKING"
    WAITING_PERMISSION = "WAITING_PERMISSION"
    WAITING_ANSWER = "WAITING_ANSWER"
    ERROR = "ERROR"


# Statuses that require user attention
ATTENTION_STATUSES = {
    AgentStatus.WAITING_PERMISSION,
    AgentStatus.WAITING_ANSWER,
    AgentStatus.ERROR,
}

# Status display
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
