"""Shared test fixtures for Grove tests."""

from __future__ import annotations

from pathlib import Path
from unittest.mock import patch

import pytest

from grove import git, log
from grove.models import Config, RepoWorktree, Workspace


@pytest.fixture(autouse=True)
def _clear_grove_config_cache():
    """Clear the ``read_grove_config`` LRU cache and log state between tests."""
    git.read_grove_config.cache_clear()
    log._initialized = False
    yield
    git.read_grove_config.cache_clear()
    log._initialized = False


@pytest.fixture()
def tmp_grove(tmp_path: Path):
    """Set up a temporary Grove home directory with config and state."""
    grove_dir = tmp_path / ".grove"
    grove_dir.mkdir()
    workspace_dir = grove_dir / "workspaces"
    workspace_dir.mkdir()
    repos_dir = tmp_path / "repos"
    repos_dir.mkdir()

    config_path = grove_dir / "config.toml"
    state_path = grove_dir / "state.json"

    # Write minimal config
    config_path.write_text(f'repo_dirs = ["{repos_dir}"]\nworkspace_dir = "{workspace_dir}"\n')
    state_path.write_text("[]")

    # Patch Grove constants to use tmp dirs
    with (
        patch("grove.config.GROVE_DIR", grove_dir),
        patch("grove.config.CONFIG_PATH", config_path),
        patch("grove.config.DEFAULT_WORKSPACE_DIR", workspace_dir),
        patch("grove.state.GROVE_DIR", grove_dir),
        patch("grove.state.STATE_PATH", state_path),
        patch("grove.stats.GROVE_DIR", grove_dir),
        patch("grove.stats.STATS_PATH", grove_dir / "stats.json"),
        patch("grove.mcp_store.DB_PATH", grove_dir / "messages.db"),
    ):
        yield {
            "grove_dir": grove_dir,
            "repos_dir": repos_dir,
            "workspace_dir": workspace_dir,
            "config_path": config_path,
            "state_path": state_path,
        }


@pytest.fixture()
def fake_repos(tmp_grove: dict) -> dict[str, Path]:
    """Create fake repo directories (not real git repos)."""
    repos_dir = tmp_grove["repos_dir"]
    names = ["svc-auth", "svc-api", "svc-gateway", "web-app", "design-system"]
    repos = {}
    for name in names:
        repo = repos_dir / name
        repo.mkdir()
        (repo / ".git").mkdir()  # Fake git dir
        repos[name] = repo
    return repos


@pytest.fixture()
def sample_config(tmp_grove: dict) -> Config:
    """Return a Config pointing at the tmp dirs."""
    return Config(
        repo_dirs=[tmp_grove["repos_dir"]],
        workspace_dir=tmp_grove["workspace_dir"],
    )


@pytest.fixture()
def sample_workspace(tmp_grove: dict) -> Workspace:
    """Return a sample Workspace object."""
    ws_path = tmp_grove["workspace_dir"] / "test-ws"
    ws_path.mkdir()
    return Workspace(
        name="test-ws",
        path=ws_path,
        branch="feat/test",
        repos=[
            RepoWorktree(
                repo_name="svc-auth",
                source_repo=tmp_grove["repos_dir"] / "svc-auth",
                worktree_path=ws_path / "svc-auth",
                branch="feat/test",
            ),
        ],
        created_at="2025-01-01T00:00:00",
    )
