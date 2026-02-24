"""Tests for grove.workspace — create, delete, status with mocked git."""

from __future__ import annotations

from unittest.mock import patch

import pytest

from grove import state, workspace
from grove.git import GitError
from grove.models import Config


@pytest.fixture()
def mock_git():
    """Patch all git operations used by workspace module."""
    with (
        patch("grove.workspace.git.worktree_has_branch", return_value=False),
        patch("grove.workspace.git.branch_exists", return_value=True),
        patch("grove.workspace.git.create_branch"),
        patch("grove.workspace.git.worktree_add"),
        patch("grove.workspace.git.worktree_remove"),
        patch("grove.workspace.git.current_branch", return_value="feat/test"),
        patch("grove.workspace.git.repo_status", return_value=""),
    ):
        yield


class TestCreateWorkspace:
    def test_success(self, tmp_grove, mock_git):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        repo_path = tmp_grove["repos_dir"] / "svc-api"
        repo_path.mkdir()
        repos = {"svc-api": repo_path}

        ws = workspace.create_workspace("test", repos, "feat/test", cfg)
        assert ws is not None
        assert ws.name == "test"
        assert ws.branch == "feat/test"
        assert len(ws.repos) == 1
        assert ws.repos[0].repo_name == "svc-api"

        # Verify state saved
        saved = state.get_workspace("test")
        assert saved is not None

    def test_duplicate_name_fails(self, tmp_grove, mock_git, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        ws = workspace.create_workspace("test-ws", {}, "feat/x", cfg)
        assert ws is None

    def test_duplicate_branch_fails(self, tmp_grove):
        with patch("grove.workspace.git.worktree_has_branch", return_value=True):
            cfg = Config(
                repos_dir=tmp_grove["repos_dir"],
                workspace_dir=tmp_grove["workspace_dir"],
            )
            repo_path = tmp_grove["repos_dir"] / "svc-api"
            repo_path.mkdir()
            ws = workspace.create_workspace("test", {"svc-api": repo_path}, "feat/taken", cfg)
            assert ws is None

    def test_rollback_on_worktree_failure(self, tmp_grove):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        repo1 = tmp_grove["repos_dir"] / "repo1"
        repo2 = tmp_grove["repos_dir"] / "repo2"
        repo1.mkdir()
        repo2.mkdir()

        call_count = 0

        def failing_worktree_add(repo, wt_path, branch):
            nonlocal call_count
            call_count += 1
            if call_count > 1:
                raise GitError("disk full")

        with (
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=True),
            patch("grove.workspace.git.worktree_add", side_effect=failing_worktree_add),
            patch("grove.workspace.git.worktree_remove") as mock_remove,
        ):
            ws = workspace.create_workspace("test", {"repo1": repo1, "repo2": repo2}, "feat/x", cfg)
            assert ws is None
            # First worktree should be rolled back
            assert mock_remove.called

    def test_auto_creates_branch(self, tmp_grove):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        repo_path = tmp_grove["repos_dir"] / "svc-api"
        repo_path.mkdir()

        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=False),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.create_branch") as mock_create,
            patch("grove.workspace.git.worktree_add"),
        ):
            ws = workspace.create_workspace("test", {"svc-api": repo_path}, "feat/new", cfg)
            assert ws is not None
            mock_create.assert_called_once_with(repo_path, "feat/new", start_point="origin/main")


class TestSetupHook:
    def test_runs_setup_command(self, tmp_grove):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        repo_path = tmp_grove["repos_dir"] / "svc-api"
        repo_path.mkdir()

        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=True),
            patch("grove.workspace.git.worktree_add"),
            patch("grove.workspace.subprocess.run") as mock_sub,
            patch(
                "grove.workspace.git.read_grove_config",
                return_value={"setup": "pnpm install"},
            ) as mock_cfg,
        ):
            ws = workspace.create_workspace("test", {"svc-api": repo_path}, "feat/x", cfg)
            assert ws is not None
            # Config should be read from the source repo, not the worktree
            mock_cfg.assert_called_once_with(repo_path)
            mock_sub.assert_called_once_with(
                "pnpm install",
                cwd=ws.path / "svc-api",
                shell=True,
                check=True,
            )

    def test_runs_multiple_setup_commands(self, tmp_grove):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        repo_path = tmp_grove["repos_dir"] / "web"
        repo_path.mkdir()

        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=True),
            patch("grove.workspace.git.worktree_add"),
            patch("grove.workspace.subprocess.run") as mock_sub,
            patch(
                "grove.workspace.git.read_grove_config",
                return_value={"setup": ["pnpm install", "pnpm build"]},
            ),
        ):
            ws = workspace.create_workspace("test", {"web": repo_path}, "feat/x", cfg)
            assert ws is not None
            assert mock_sub.call_count == 2

    def test_no_setup_no_crash(self, tmp_grove):
        cfg = Config(
            repos_dir=tmp_grove["repos_dir"],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        repo_path = tmp_grove["repos_dir"] / "svc"
        repo_path.mkdir()

        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=True),
            patch("grove.workspace.git.worktree_add"),
            patch("grove.workspace.git.read_grove_config", return_value={}),
        ):
            ws = workspace.create_workspace("test", {"svc": repo_path}, "feat/x", cfg)
            assert ws is not None


class TestDeleteWorkspace:
    def test_success(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        with patch("grove.workspace.git.worktree_remove"):
            result = workspace.delete_workspace("test-ws")
        assert result is True
        assert state.get_workspace("test-ws") is None

    def test_not_found(self, tmp_grove):
        result = workspace.delete_workspace("nope")
        assert result is False


class TestSyncWorkspace:
    def test_up_to_date(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(1, 0)),
        ):
            results = workspace.sync_workspace(sample_workspace)
            assert len(results) == 1
            assert results[0]["result"] == "up to date"

    def test_successful_rebase(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value="origin/stage"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(2, 3)),
            patch("grove.workspace.git.rebase_onto") as mock_rebase,
        ):
            results = workspace.sync_workspace(sample_workspace)
            assert len(results) == 1
            assert results[0]["result"] == "rebased (3 new commits)"
            assert results[0]["base"] == "origin/stage"
            mock_rebase.assert_called_once()

    def test_conflict_aborts_rebase(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(1, 2)),
            patch("grove.workspace.git.rebase_onto", side_effect=GitError("conflict")),
            patch("grove.workspace.git.rebase_abort") as mock_abort,
        ):
            results = workspace.sync_workspace(sample_workspace)
            assert results[0]["result"] == "conflict"
            mock_abort.assert_called_once()

    def test_skips_dirty_worktree(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=" M dirty.py"),
        ):
            results = workspace.sync_workspace(sample_workspace)
            assert results[0]["result"] == "skipped: uncommitted changes"

    def test_unknown_base_branch(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", side_effect=GitError("no HEAD")),
        ):
            results = workspace.sync_workspace(sample_workspace)
            assert results[0]["base"] == "?"
            assert "could not determine" in results[0]["result"]

    def test_fetch_failure_continues(self, tmp_grove, sample_workspace):
        """Sync should continue even if fetch fails (offline, etc)."""
        with (
            patch("grove.workspace.git.fetch", side_effect=GitError("network error")),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 0)),
        ):
            results = workspace.sync_workspace(sample_workspace)
            # Should still produce a result (up to date based on cached refs)
            assert len(results) == 1
            assert results[0]["result"] == "up to date"

    def test_rebase_when_behind_unknown(self, tmp_grove, sample_workspace):
        """When ahead/behind fails, still attempt rebase."""
        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", side_effect=GitError("bad ref")),
            patch("grove.workspace.git.rebase_onto") as mock_rebase,
        ):
            results = workspace.sync_workspace(sample_workspace)
            assert results[0]["result"] == "rebased"
            mock_rebase.assert_called_once()


class TestWorkspaceStatus:
    def test_clean_repos(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.current_branch", return_value="feat/test"),
            patch("grove.workspace.git.repo_status", return_value=""),
        ):
            results = workspace.workspace_status(sample_workspace)
            assert len(results) == 1
            assert results[0]["status"] == "clean"
            assert results[0]["branch"] == "feat/test"
            # Ahead/behind should be present (may be "-" if git calls fail)
            assert "ahead" in results[0]
            assert "behind" in results[0]

    def test_modified_repo(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.current_branch", return_value="feat/test"),
            patch("grove.workspace.git.repo_status", return_value=" M file.py\n?? new.txt"),
        ):
            results = workspace.workspace_status(sample_workspace)
            assert results[0]["status"] == " M file.py\n?? new.txt"

    def test_git_error(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.current_branch", side_effect=GitError("broken")),
        ):
            results = workspace.workspace_status(sample_workspace)
            assert results[0]["branch"] == "?"
            assert "error" in results[0]["status"]
            assert results[0]["ahead"] == "-"
            assert results[0]["behind"] == "-"

    def test_ahead_behind_populated(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.current_branch", return_value="feat/test"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.repo_base_branch", return_value="origin/main"),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(5, 2)),
        ):
            results = workspace.workspace_status(sample_workspace)
            assert results[0]["ahead"] == "5"
            assert results[0]["behind"] == "2"

    def test_ahead_behind_fallback_on_failure(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.current_branch", return_value="feat/test"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", side_effect=GitError("no remote")),
        ):
            results = workspace.workspace_status(sample_workspace)
            assert results[0]["ahead"] == "-"
            assert results[0]["behind"] == "-"
            # Status should still work fine
            assert results[0]["status"] == "clean"
