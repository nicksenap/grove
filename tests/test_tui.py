"""Tests for grove.tui — ProcessState unit tests + Textual async pilot tests."""

from __future__ import annotations

import pytest

from grove.tui import ProcessState, ProcStatus, RunApp

# ---------------------------------------------------------------------------
# ProcessState unit tests
# ---------------------------------------------------------------------------


class TestProcessState:
    def test_defaults(self):
        ps = ProcessState(repo_name="api", command="npm start", cwd="/tmp")
        assert ps.status == ProcStatus.STARTING
        assert ps.process is None
        assert ps.exit_code is None

    def test_status_icon_starting(self):
        ps = ProcessState(repo_name="api", command="cmd", cwd="/tmp")
        assert "yellow" in ps.status_icon

    def test_status_icon_running(self):
        ps = ProcessState(repo_name="api", command="cmd", cwd="/tmp", status=ProcStatus.RUNNING)
        assert "green" in ps.status_icon

    def test_status_icon_exited_success(self):
        ps = ProcessState(
            repo_name="api", command="cmd", cwd="/tmp", status=ProcStatus.EXITED, exit_code=0
        )
        assert "dim" in ps.status_icon

    def test_status_icon_exited_failure(self):
        ps = ProcessState(
            repo_name="api", command="cmd", cwd="/tmp", status=ProcStatus.EXITED, exit_code=1
        )
        assert "red" in ps.status_icon


class TestProcStatus:
    def test_values(self):
        assert ProcStatus.STARTING.name == "STARTING"
        assert ProcStatus.RUNNING.name == "RUNNING"
        assert ProcStatus.EXITED.name == "EXITED"


# ---------------------------------------------------------------------------
# Textual async pilot tests
# ---------------------------------------------------------------------------


@pytest.mark.asyncio
async def test_sidebar_renders_repos():
    """Sidebar should show all repo names."""
    entries = [
        ("api", "echo api", "/tmp"),
        ("web", "echo web", "/tmp"),
    ]
    app = RunApp(entries)
    async with app.run_test(size=(120, 30)):
        # Check sidebar items exist
        sidebar = app.query_one("#sidebar")
        items = sidebar.query("RepoItem")
        assert len(items) == 2


@pytest.mark.asyncio
async def test_first_log_visible_on_mount():
    """The first repo's log should be visible by default."""
    entries = [
        ("api", "echo hello", "/tmp"),
        ("web", "echo world", "/tmp"),
    ]
    app = RunApp(entries)
    async with app.run_test(size=(120, 30)):
        log0 = app.query_one("#log-0")
        log1 = app.query_one("#log-1")
        assert log0.has_class("active")
        assert not log1.has_class("active")


@pytest.mark.asyncio
async def test_number_key_switches_log():
    """Pressing '2' should switch to the second repo's log."""
    entries = [
        ("api", "echo api", "/tmp"),
        ("web", "echo web", "/tmp"),
    ]
    app = RunApp(entries)
    async with app.run_test(size=(120, 30)) as pilot:
        await pilot.press("2")
        log0 = app.query_one("#log-0")
        log1 = app.query_one("#log-1")
        assert not log0.has_class("active")
        assert log1.has_class("active")


@pytest.mark.asyncio
async def test_quit_key_exits():
    """Pressing 'q' should exit the app."""
    entries = [("api", "sleep 10", "/tmp")]
    app = RunApp(entries)
    async with app.run_test(size=(120, 30)) as pilot:
        await pilot.press("q")
        # App should have exited — if we reach here without hanging, it worked
