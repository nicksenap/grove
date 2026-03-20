"""Workspace usage statistics (~/.grove/stats.json).

Append-only event log that records workspace lifecycle events.
Stats are non-critical — failures never block workspace operations.
"""

from __future__ import annotations

import contextlib
import json
import os
import tempfile
from collections import Counter
from datetime import date, datetime, timedelta
from pathlib import Path

from grove.config import GROVE_DIR

STATS_PATH = GROVE_DIR / "stats.json"


def _load_events() -> list[dict]:
    """Load the event log. Returns [] on missing or corrupt file."""
    if not STATS_PATH.exists():
        return []
    try:
        return json.loads(STATS_PATH.read_text())
    except (json.JSONDecodeError, OSError):
        return []


def _atomic_write(path: Path, content: str) -> None:
    """Write content atomically via temp file + rename."""
    fd, tmp = tempfile.mkstemp(dir=path.parent, suffix=".tmp")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(content)
        os.replace(tmp, path)
    except BaseException:
        with contextlib.suppress(OSError):
            os.unlink(tmp)
        raise


def _append_event(event: dict) -> None:
    """Append an event to the log."""
    GROVE_DIR.mkdir(parents=True, exist_ok=True)
    events = _load_events()
    events.append(event)
    _atomic_write(STATS_PATH, json.dumps(events, indent=2) + "\n")


def record_created(name: str, branch: str, repo_names: list[str]) -> None:
    """Record a workspace creation event."""
    _append_event(
        {
            "event": "workspace_created",
            "timestamp": datetime.now().isoformat(),
            "workspace_name": name,
            "branch": branch,
            "repo_names": repo_names,
            "repo_count": len(repo_names),
        }
    )


def record_deleted(name: str, branch: str, repo_names: list[str]) -> None:
    """Record a workspace deletion event."""
    _append_event(
        {
            "event": "workspace_deleted",
            "timestamp": datetime.now().isoformat(),
            "workspace_name": name,
            "branch": branch,
            "repo_names": repo_names,
            "repo_count": len(repo_names),
        }
    )


def _format_duration(seconds: float) -> str:
    """Format seconds into a human-readable duration string."""
    td = timedelta(seconds=seconds)
    days = td.days
    hours, remainder = divmod(td.seconds, 3600)
    minutes = remainder // 60

    if days > 0:
        return f"{days}d {hours}h"
    if hours > 0:
        return f"{hours}h {minutes}m"
    return f"{minutes}m"


def compute_stats() -> dict:
    """Compute aggregate statistics from the event log."""
    from grove import state

    events = _load_events()

    created_events = [e for e in events if e["event"] == "workspace_created"]
    deleted_events = [e for e in events if e["event"] == "workspace_deleted"]

    # Active count from live state
    active_count = len(state.load_workspaces())

    # Average lifetime: match each delete with its most recent preceding create
    lifetimes: list[float] = []
    # Build a map of name -> list of create timestamps (chronological)
    creates_by_name: dict[str, list[str]] = {}
    for e in created_events:
        creates_by_name.setdefault(e["workspace_name"], []).append(e["timestamp"])

    for e in deleted_events:
        ws_name = e["workspace_name"]
        if ws_name not in creates_by_name or not creates_by_name[ws_name]:
            continue
        # Find the most recent create before this delete
        delete_ts = datetime.fromisoformat(e["timestamp"])
        best_create: datetime | None = None
        best_idx: int | None = None
        for i, ts in enumerate(creates_by_name[ws_name]):
            create_ts = datetime.fromisoformat(ts)
            if create_ts <= delete_ts:
                best_create = create_ts
                best_idx = i
        if best_create is not None and best_idx is not None:
            lifetimes.append((delete_ts - best_create).total_seconds())
            creates_by_name[ws_name].pop(best_idx)

    avg_lifetime_seconds = sum(lifetimes) / len(lifetimes) if lifetimes else None

    # Top repos
    repo_counter: Counter[str] = Counter()
    for e in created_events:
        for repo in e.get("repo_names", []):
            repo_counter[repo] += 1
    top_repos = repo_counter.most_common(5)

    # Created this week / month
    now = datetime.now()
    week_start = now - timedelta(days=now.weekday())
    week_start = week_start.replace(hour=0, minute=0, second=0, microsecond=0)
    month_start = now.replace(day=1, hour=0, minute=0, second=0, microsecond=0)

    created_this_week = 0
    created_this_month = 0
    for e in created_events:
        ts = datetime.fromisoformat(e["timestamp"])
        if ts >= week_start:
            created_this_week += 1
        if ts >= month_start:
            created_this_month += 1

    return {
        "total_created": len(created_events),
        "total_deleted": len(deleted_events),
        "active_count": active_count,
        "avg_lifetime_seconds": avg_lifetime_seconds,
        "avg_lifetime_human": (
            _format_duration(avg_lifetime_seconds) if avg_lifetime_seconds else None
        ),
        "top_repos": top_repos,
        "created_this_week": created_this_week,
        "created_this_month": created_this_month,
    }


# ---------------------------------------------------------------------------
# Heatmap rendering
# ---------------------------------------------------------------------------

# Warm color palette (matching the screenshot's brown/orange tones)
_HEATMAP_COLORS = [
    "#3b3b3b",  # level 0: empty / dark grey
    "#8b6849",  # level 1: light brown
    "#a0764f",  # level 2: medium brown
    "#c48a4e",  # level 3: warm orange
    "#e8a04e",  # level 4: bright orange
]

_BLOCK = "█"
_DOT = "·"


def _activity_by_date(events: list[dict]) -> Counter[date]:
    """Count workspace_created events per calendar date."""
    counts: Counter[date] = Counter()
    for e in events:
        if e.get("event") == "workspace_created":
            ts = datetime.fromisoformat(e["timestamp"])
            counts[ts.date()] += 1
    return counts


def _color_for_count(count: int, max_count: int) -> str:
    """Map a count to a heatmap color index."""
    if count == 0:
        return _HEATMAP_COLORS[0]
    if max_count <= 0:
        return _HEATMAP_COLORS[1]
    # Quantize into 4 non-zero levels
    ratio = count / max_count
    if ratio <= 0.25:
        return _HEATMAP_COLORS[1]
    if ratio <= 0.50:
        return _HEATMAP_COLORS[2]
    if ratio <= 0.75:
        return _HEATMAP_COLORS[3]
    return _HEATMAP_COLORS[4]


def build_heatmap(weeks: int = 52) -> list[str]:
    """Build a GitHub-style contribution heatmap as Rich-formatted lines.

    Returns a list of strings ready for ``console.print()``.
    Covers the last *weeks* weeks ending at today.
    """
    events = _load_events()
    activity = _activity_by_date(events)

    today = date.today()
    # Start from the Monday of the week `weeks` ago
    start = today - timedelta(days=today.weekday() + (weeks - 1) * 7)

    # Build grid: 7 rows (Mon=0..Sun=6) × N columns (weeks)
    grid: list[list[int]] = [[] for _ in range(7)]
    current = start
    week_dates: list[date] = []  # first day of each week column

    col = 0
    while current <= today:
        if current.weekday() == 0:
            week_dates.append(current)
        row = current.weekday()
        # Pad rows if we started mid-week on the first column
        while len(grid[row]) < col:
            grid[row].append(-1)  # -1 = no cell (before start)
        grid[row].append(activity.get(current, 0))
        if current.weekday() == 6:
            col += 1
        current += timedelta(days=1)

    num_cols = max(len(row) for row in grid) if grid else 0
    # Pad all rows to same length
    for row in grid:
        while len(row) < num_cols:
            row.append(-1)

    max_count = max((c for row in grid for c in row if c > 0), default=0)

    pad = "      "  # left padding matching day labels
    lines: list[str] = []

    # Month labels — placed at the column where each month first appears
    month_labels: list[str] = [""] * num_cols
    last_month = -1
    for col_idx, wd in enumerate(week_dates):
        if col_idx < num_cols and wd.month != last_month:
            month_labels[col_idx] = wd.strftime("%b")
            last_month = wd.month

    # Render month header: each column is 2 chars wide
    month_header = pad
    skip = 0
    for col_idx in range(num_cols):
        if skip > 0:
            skip -= 1
            continue
        label = month_labels[col_idx]
        if label:
            month_header += label
            # Label takes ceil(len/2) columns worth of space
            skip = max(0, len(label) // 2)
            remaining = len(label) % 2
            month_header += " " * (2 - remaining) if remaining else ""
        else:
            month_header += "  "
    lines.append(month_header.rstrip())

    # Grid rows — only show labels for Mon, Wed, Fri
    day_labels = ["Mon", "   ", "Wed", "   ", "Fri", "   ", "   "]
    for row_idx in range(7):
        label = f"{day_labels[row_idx]:>3}   "
        cells = ""
        for col_idx in range(num_cols):
            count = grid[row_idx][col_idx]
            if count < 0:
                cells += "  "  # empty / no cell
            elif count == 0:
                cells += f"[{_HEATMAP_COLORS[0]}]{_DOT}[/] "
            else:
                color = _color_for_count(count, max_count)
                cells += f"[{color}]{_BLOCK}[/] "
        lines.append(label + cells.rstrip())

    # Legend
    legend = pad + "Less "
    for color in _HEATMAP_COLORS:
        legend += f"[{color}]{_BLOCK}[/] "
    legend += "More"
    lines.append("")
    lines.append(legend)

    return lines
