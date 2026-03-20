"""Tests for grove.stats — event log and statistics."""

from __future__ import annotations

import json
from datetime import date, datetime, timedelta

from grove import stats


def test_load_events_empty(tmp_grove):
    """Returns empty list when no stats file exists."""
    assert stats._load_events() == []


def test_record_created(tmp_grove):
    """Records a workspace_created event."""
    stats.record_created("feat-login", "feat/login", ["svc-auth", "svc-api"])

    events = stats._load_events()
    assert len(events) == 1
    assert events[0]["event"] == "workspace_created"
    assert events[0]["workspace_name"] == "feat-login"
    assert events[0]["branch"] == "feat/login"
    assert events[0]["repo_names"] == ["svc-auth", "svc-api"]
    assert events[0]["repo_count"] == 2


def test_record_deleted(tmp_grove):
    """Records a workspace_deleted event."""
    stats.record_deleted("feat-login", "feat/login", ["svc-auth"])

    events = stats._load_events()
    assert len(events) == 1
    assert events[0]["event"] == "workspace_deleted"


def test_multiple_events_append(tmp_grove):
    """Multiple events are appended in order."""
    stats.record_created("ws-1", "b1", ["repo-a"])
    stats.record_created("ws-2", "b2", ["repo-b", "repo-c"])
    stats.record_deleted("ws-1", "b1", ["repo-a"])

    events = stats._load_events()
    assert len(events) == 3
    assert events[0]["workspace_name"] == "ws-1"
    assert events[1]["workspace_name"] == "ws-2"
    assert events[2]["event"] == "workspace_deleted"


def test_compute_stats_empty(tmp_grove):
    """Empty event log returns zeroed stats."""
    data = stats.compute_stats()
    assert data["total_created"] == 0
    assert data["total_deleted"] == 0
    assert data["active_count"] == 0
    assert data["avg_lifetime_seconds"] is None
    assert data["avg_lifetime_human"] is None
    assert data["top_repos"] == []
    assert data["created_this_week"] == 0
    assert data["created_this_month"] == 0


def test_compute_stats_with_data(tmp_grove):
    """Basic counts are correct."""
    stats.record_created("a", "b1", ["r1", "r2"])
    stats.record_created("b", "b2", ["r1"])
    stats.record_deleted("a", "b1", ["r1", "r2"])

    data = stats.compute_stats()
    assert data["total_created"] == 2
    assert data["total_deleted"] == 1


def test_avg_lifetime(tmp_grove):
    """Average lifetime is computed from create/delete pairs."""
    stats_path = tmp_grove["grove_dir"] / "stats.json"
    now = datetime.now()

    events = [
        {
            "event": "workspace_created",
            "timestamp": (now - timedelta(hours=2)).isoformat(),
            "workspace_name": "ws-a",
            "branch": "b",
            "repo_names": ["r"],
            "repo_count": 1,
        },
        {
            "event": "workspace_deleted",
            "timestamp": now.isoformat(),
            "workspace_name": "ws-a",
            "branch": "b",
            "repo_names": ["r"],
            "repo_count": 1,
        },
    ]
    stats_path.write_text(json.dumps(events))

    data = stats.compute_stats()
    assert data["avg_lifetime_seconds"] is not None
    # Should be approximately 2 hours (7200 seconds), allow some tolerance
    assert 7100 < data["avg_lifetime_seconds"] < 7300
    assert data["avg_lifetime_human"] == "2h 0m"


def test_top_repos(tmp_grove):
    """Top repos are ranked by frequency."""
    stats.record_created("ws-1", "b", ["alpha", "beta"])
    stats.record_created("ws-2", "b", ["alpha", "gamma"])
    stats.record_created("ws-3", "b", ["alpha"])

    data = stats.compute_stats()
    assert data["top_repos"][0] == ("alpha", 3)
    assert ("beta", 1) in data["top_repos"]
    assert ("gamma", 1) in data["top_repos"]


def test_created_this_week_and_month(tmp_grove):
    """Events from this week/month are counted, old ones are not."""
    stats_path = tmp_grove["grove_dir"] / "stats.json"
    now = datetime.now()

    events = [
        {
            "event": "workspace_created",
            "timestamp": now.isoformat(),
            "workspace_name": "recent",
            "branch": "b",
            "repo_names": ["r"],
            "repo_count": 1,
        },
        {
            "event": "workspace_created",
            "timestamp": (now - timedelta(days=60)).isoformat(),
            "workspace_name": "old",
            "branch": "b",
            "repo_names": ["r"],
            "repo_count": 1,
        },
    ]
    stats_path.write_text(json.dumps(events))

    data = stats.compute_stats()
    assert data["total_created"] == 2
    assert data["created_this_week"] == 1
    assert data["created_this_month"] == 1


def test_corrupt_stats_file(tmp_grove):
    """Corrupt stats file returns empty events, no crash."""
    stats_path = tmp_grove["grove_dir"] / "stats.json"
    stats_path.write_text("not valid json{{{")

    events = stats._load_events()
    assert events == []

    # compute_stats also survives
    data = stats.compute_stats()
    assert data["total_created"] == 0


def test_format_duration():
    """Duration formatting covers days, hours, minutes, and sub-minute."""
    assert stats._format_duration(30) == "<1m"
    assert stats._format_duration(60) == "1m"
    assert stats._format_duration(3661) == "1h 1m"
    assert stats._format_duration(90061) == "1d 1h"


def test_build_heatmap_empty(tmp_grove):
    """Heatmap renders without errors when no events exist."""
    lines = stats.build_heatmap()
    # Should have: month header + 7 day rows + blank + legend = 10 lines
    assert len(lines) == 10
    # Legend line should contain "Less" and "More"
    assert "Less" in lines[-1]
    assert "More" in lines[-1]


def test_build_heatmap_with_activity(tmp_grove):
    """Heatmap shows blocks for days with activity."""
    stats_path = tmp_grove["grove_dir"] / "stats.json"
    today = date.today()
    events = [
        {
            "event": "workspace_created",
            "timestamp": datetime(today.year, today.month, today.day, 10).isoformat(),
            "workspace_name": "ws",
            "branch": "b",
            "repo_names": ["r"],
            "repo_count": 1,
        },
    ]
    stats_path.write_text(json.dumps(events))

    lines = stats.build_heatmap()
    # The heatmap should contain at least one filled block
    all_text = "\n".join(lines)
    assert stats._BLOCK in all_text


def test_activity_by_date(tmp_grove):
    """Counts are grouped by calendar date."""
    stats_path = tmp_grove["grove_dir"] / "stats.json"
    today = date.today()
    events = [
        {
            "event": "workspace_created",
            "timestamp": datetime(today.year, today.month, today.day, 9).isoformat(),
            "workspace_name": "a",
            "branch": "b",
            "repo_names": [],
            "repo_count": 0,
        },
        {
            "event": "workspace_created",
            "timestamp": datetime(today.year, today.month, today.day, 15).isoformat(),
            "workspace_name": "b",
            "branch": "b",
            "repo_names": [],
            "repo_count": 0,
        },
        {
            "event": "workspace_deleted",
            "timestamp": datetime(today.year, today.month, today.day, 16).isoformat(),
            "workspace_name": "a",
            "branch": "b",
            "repo_names": [],
            "repo_count": 0,
        },
    ]
    stats_path.write_text(json.dumps(events))

    activity = stats._activity_by_date(events)
    # Only counts workspace_created, not deleted
    assert activity[today] == 2
