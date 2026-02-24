"""Tests for grove.models — roundtrip serialization."""

from __future__ import annotations

from pathlib import Path

from grove.models import Config, RepoWorktree, Workspace


class TestConfig:
    def test_roundtrip_basic(self):
        cfg = Config(repos_dir=Path("/repos"), workspace_dir=Path("/ws"))
        d = cfg.to_dict()
        cfg2 = Config.from_dict(d)
        assert cfg2.repos_dir == cfg.repos_dir
        assert cfg2.workspace_dir == cfg.workspace_dir
        assert cfg2.presets == {}

    def test_roundtrip_with_presets(self):
        cfg = Config(
            repos_dir=Path("/repos"),
            workspace_dir=Path("/ws"),
            presets={
                "backend": ["svc-auth", "svc-api"],
                "frontend": ["web-app"],
            },
        )
        d = cfg.to_dict()
        cfg2 = Config.from_dict(d)
        assert cfg2.presets == {"backend": ["svc-auth", "svc-api"], "frontend": ["web-app"]}

    def test_from_dict_missing_presets(self):
        d = {"repos_dir": "/repos", "workspace_dir": "/ws"}
        cfg = Config.from_dict(d)
        assert cfg.presets == {}

    def test_to_dict_no_presets_key_when_empty(self):
        cfg = Config(repos_dir=Path("/r"), workspace_dir=Path("/w"))
        d = cfg.to_dict()
        assert "presets" not in d


class TestRepoWorktree:
    def test_roundtrip(self):
        rw = RepoWorktree(
            repo_name="svc-api",
            source_repo=Path("/repos/svc-api"),
            worktree_path=Path("/ws/test/svc-api"),
            branch="feat/login",
        )
        d = rw.to_dict()
        rw2 = RepoWorktree.from_dict(d)
        assert rw2.repo_name == "svc-api"
        assert rw2.source_repo == Path("/repos/svc-api")
        assert rw2.worktree_path == Path("/ws/test/svc-api")
        assert rw2.branch == "feat/login"


class TestWorkspace:
    def test_roundtrip(self):
        ws = Workspace(
            name="my-ws",
            path=Path("/ws/my-ws"),
            branch="main",
            repos=[
                RepoWorktree(
                    repo_name="a",
                    source_repo=Path("/r/a"),
                    worktree_path=Path("/ws/my-ws/a"),
                    branch="main",
                ),
            ],
            created_at="2025-01-01T00:00:00",
        )
        d = ws.to_dict()
        ws2 = Workspace.from_dict(d)
        assert ws2.name == "my-ws"
        assert ws2.branch == "main"
        assert len(ws2.repos) == 1
        assert ws2.repos[0].repo_name == "a"
        assert ws2.created_at == "2025-01-01T00:00:00"

    def test_from_dict_missing_repos(self):
        d = {"name": "x", "path": "/x", "branch": "b"}
        ws = Workspace.from_dict(d)
        assert ws.repos == []
        assert ws.created_at == ""
