"""Tests for grove.dash.store — SQLite task persistence."""

from __future__ import annotations

from pathlib import Path

from grove.dash.constants import AgentStatus
from grove.dash.store import Task, TaskStore


def _tmp_store(tmp_path: Path) -> TaskStore:
    return TaskStore(db_path=tmp_path / "tasks.db")


def test_create_and_get(tmp_path: Path) -> None:
    store = _tmp_store(tmp_path)
    task = Task.create(title="Add login page", branch="feat/login")
    store.add(task)

    loaded = store.get(task.id)
    assert loaded is not None
    assert loaded.title == "Add login page"
    assert loaded.branch == "feat/login"
    assert loaded.status == AgentStatus.PLANNED
    assert loaded.created_at
    store.close()


def test_list_active(tmp_path: Path) -> None:
    store = _tmp_store(tmp_path)
    t1 = Task.create(title="Task 1")
    t2 = Task.create(title="Task 2")
    t3 = Task.create(title="Task 3")
    store.add(t1)
    store.add(t2)
    store.add(t3)

    # Archive one
    store.archive(t2.id)

    active = store.list_active()
    assert len(active) == 2
    assert {t.title for t in active} == {"Task 1", "Task 3"}
    store.close()


def test_update_status(tmp_path: Path) -> None:
    store = _tmp_store(tmp_path)
    task = Task.create(title="Bug fix")
    store.add(task)

    store.update_status(task.id, AgentStatus.WORKING)
    loaded = store.get(task.id)
    assert loaded is not None
    assert loaded.status == AgentStatus.WORKING

    store.update_status(task.id, AgentStatus.DONE)
    loaded = store.get(task.id)
    assert loaded is not None
    assert loaded.status == AgentStatus.DONE
    assert loaded.completed_at  # should be set
    store.close()


def test_link_session(tmp_path: Path) -> None:
    store = _tmp_store(tmp_path)
    task = Task.create(title="Feature")
    store.add(task)

    store.link_session(task.id, "session-abc")

    by_session = store.find_by_session("session-abc")
    assert by_session is not None
    assert by_session.id == task.id
    store.close()


def test_find_by_workspace(tmp_path: Path) -> None:
    store = _tmp_store(tmp_path)
    task = Task.create(title="WS task")
    store.add(task)
    store.update_field(task.id, workspace="my-workspace")

    found = store.find_by_workspace("my-workspace")
    assert found is not None
    assert found.id == task.id

    # Archived tasks should not be found
    store.archive(task.id)
    found = store.find_by_workspace("my-workspace")
    assert found is None
    store.close()


def test_list_by_status(tmp_path: Path) -> None:
    store = _tmp_store(tmp_path)
    t1 = Task.create(title="Planned")
    t2 = Task.create(title="Working")
    store.add(t1)
    store.add(t2)
    store.update_status(t2.id, AgentStatus.WORKING)

    planned = store.list_by_status(AgentStatus.PLANNED)
    assert len(planned) == 1
    assert planned[0].title == "Planned"

    working = store.list_by_status(AgentStatus.WORKING)
    assert len(working) == 1
    assert working[0].title == "Working"
    store.close()


def test_delete(tmp_path: Path) -> None:
    store = _tmp_store(tmp_path)
    task = Task.create(title="Temporary")
    store.add(task)
    assert store.get(task.id) is not None

    store.delete(task.id)
    assert store.get(task.id) is None
    store.close()


def test_repos_roundtrip(tmp_path: Path) -> None:
    store = _tmp_store(tmp_path)
    task = Task.create(title="Multi-repo", repos=["api", "web", "worker"])
    store.add(task)

    loaded = store.get(task.id)
    assert loaded is not None
    assert loaded.repos == ["api", "web", "worker"]
    store.close()


def test_config_roundtrip(tmp_path: Path) -> None:
    store = _tmp_store(tmp_path)
    task = Task.create(
        title="Custom config",
        config={"permissions": "dangerously-skip", "model": "opus"},
    )
    store.add(task)

    loaded = store.get(task.id)
    assert loaded is not None
    assert loaded.config["permissions"] == "dangerously-skip"
    assert loaded.config["model"] == "opus"
    store.close()
