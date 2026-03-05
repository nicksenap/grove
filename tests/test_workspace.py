"""Tests for grove.workspace — create, delete, status with mocked git."""

from __future__ import annotations

import subprocess
from pathlib import Path
from unittest.mock import patch

import pytest

from grove import state, workspace
from grove.git import GitError
from grove.models import Config, RepoWorktree, Workspace


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
            repo_dirs=[tmp_grove["repos_dir"]],
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
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        ws = workspace.create_workspace("test-ws", {}, "feat/x", cfg)
        assert ws is None

    def test_duplicate_branch_fails(self, tmp_grove):
        with patch("grove.workspace.git.worktree_has_branch", return_value=True):
            cfg = Config(
                repo_dirs=[tmp_grove["repos_dir"]],
                workspace_dir=tmp_grove["workspace_dir"],
            )
            repo_path = tmp_grove["repos_dir"] / "svc-api"
            repo_path.mkdir()
            ws = workspace.create_workspace("test", {"svc-api": repo_path}, "feat/taken", cfg)
            assert ws is None

    def test_rollback_on_worktree_failure(self, tmp_grove):
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
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
            repo_dirs=[tmp_grove["repos_dir"]],
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
            repo_dirs=[tmp_grove["repos_dir"]],
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
                stdin=subprocess.DEVNULL,
            )

    def test_runs_multiple_setup_commands(self, tmp_grove):
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
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
            repo_dirs=[tmp_grove["repos_dir"]],
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

    def test_partial_failure_keeps_state(self, tmp_grove, sample_workspace):
        """When worktree removal fails and directory persists, state is kept."""
        state.add_workspace(sample_workspace)
        sample_workspace.path.mkdir(exist_ok=True)
        # Make the worktree path exist so shutil.rmtree of workspace dir
        # doesn't fully clean up (simulate stuck directory)
        wt_dir = sample_workspace.repos[0].worktree_path
        wt_dir.mkdir(parents=True, exist_ok=True)
        # Make a file that prevents rmtree from cleaning up
        # (We mock the removal to always fail)
        with (
            patch("grove.workspace.git.worktree_remove", side_effect=GitError("locked")),
            patch("grove.workspace.shutil.rmtree"),  # prevent actual cleanup
        ):
            result = workspace.delete_workspace("test-ws")
        # With failures and directory still present, state should be preserved
        assert result is False
        assert state.get_workspace("test-ws") is not None


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


def _multi_repo_workspace(tmp_grove: dict, count: int = 3) -> Workspace:
    """Build a workspace with *count* repos for parallel tests."""
    ws_path = tmp_grove["workspace_dir"] / "multi-ws"
    ws_path.mkdir(exist_ok=True)
    repos = []
    for i in range(count):
        name = f"repo-{i}"
        src = tmp_grove["repos_dir"] / name
        src.mkdir(exist_ok=True)
        repos.append(
            RepoWorktree(
                repo_name=name,
                source_repo=src,
                worktree_path=ws_path / name,
                branch="feat/parallel",
            )
        )
    return Workspace(
        name="multi-ws",
        path=ws_path,
        branch="feat/parallel",
        repos=repos,
        created_at="2025-01-01T00:00:00",
    )


class TestParallelExecution:
    """Tests that verify parallel (multi-repo) behaviour."""

    def test_multi_repo_fetch_all_called(self, tmp_grove):
        """All repos are fetched in parallel during create."""
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        repos: dict[str, Path] = {}
        for name in ("alpha", "beta", "gamma"):
            p = tmp_grove["repos_dir"] / name
            p.mkdir()
            repos[name] = p

        with (
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=True),
            patch("grove.workspace.git.worktree_add"),
            patch("grove.workspace.git.fetch") as mock_fetch,
            patch("grove.workspace.git.read_grove_config", return_value={}),
        ):
            ws = workspace.create_workspace("par", repos, "feat/x", cfg)
            assert ws is not None
            assert mock_fetch.call_count == len(repos)

    def test_multi_repo_status(self, tmp_grove):
        """Status is collected for every repo in a multi-repo workspace."""
        ws = _multi_repo_workspace(tmp_grove)
        with (
            patch("grove.workspace.git.current_branch", return_value="feat/parallel"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 0)),
        ):
            results = workspace.workspace_status(ws)
            assert len(results) == 3
            repo_names = {r["repo"] for r in results}
            assert repo_names == {"repo-0", "repo-1", "repo-2"}
            for r in results:
                assert r["status"] == "clean"

    def test_error_in_one_repo_does_not_break_others(self, tmp_grove):
        """A failure in one repo should not prevent results from other repos."""
        ws = _multi_repo_workspace(tmp_grove)

        fail_path = ws.repos[1].worktree_path  # repo-1 will fail

        def branch_side_effect(path):
            if path == fail_path:
                raise GitError("corrupt repo")
            return "feat/parallel"

        with (
            patch("grove.workspace.git.current_branch", side_effect=branch_side_effect),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 0)),
        ):
            results = workspace.workspace_status(ws)
            assert len(results) == 3

            # The failed repo should have an error status
            failed = [r for r in results if r["repo"] == "repo-1"]
            assert len(failed) == 1
            assert "error" in failed[0]["status"]

            # The other repos should be fine
            ok = [r for r in results if r["repo"] != "repo-1"]
            assert all(r["status"] == "clean" for r in ok)

    def test_multi_repo_sync(self, tmp_grove):
        """Sync runs in parallel across multiple repos."""
        ws = _multi_repo_workspace(tmp_grove)
        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 0)),
        ):
            results = workspace.sync_workspace(ws)
            assert len(results) == 3
            assert all(r["result"] == "up to date" for r in results)

    def test_results_preserve_order(self, tmp_grove):
        """Results should come back in the same order as workspace.repos."""
        ws = _multi_repo_workspace(tmp_grove, count=5)
        with (
            patch("grove.workspace.git.current_branch", return_value="feat/parallel"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 0)),
        ):
            results = workspace.workspace_status(ws)
            assert [r["repo"] for r in results] == [f"repo-{i}" for i in range(5)]


class TestLifecycleHooks:
    def test_teardown_runs_before_delete(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        call_order: list[str] = []

        def mock_hook(repo_name, source_repo, worktree_path, hook):
            if hook == "teardown":
                call_order.append("teardown")

        def mock_remove(repo, path):
            call_order.append("remove")

        wt_path = sample_workspace.repos[0].worktree_path
        wt_path.mkdir(parents=True, exist_ok=True)

        with (
            patch("grove.workspace._run_hook", side_effect=mock_hook),
            patch("grove.workspace.git.worktree_remove", side_effect=mock_remove),
        ):
            workspace.delete_workspace("test-ws")
        assert call_order == ["teardown", "remove"]

    def test_teardown_failure_does_not_block_delete(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        wt_path = sample_workspace.repos[0].worktree_path
        wt_path.mkdir(parents=True, exist_ok=True)

        with (
            patch(
                "grove.workspace._run_hook",
                side_effect=subprocess.CalledProcessError(1, "teardown"),
            ),
            patch("grove.workspace.git.worktree_remove"),
        ):
            result = workspace.delete_workspace("test-ws")
        assert result is True
        assert state.get_workspace("test-ws") is None

    def test_pre_sync_runs_before_rebase(self, tmp_grove, sample_workspace):
        call_order: list[str] = []

        def mock_hook(repo_name, source_repo, worktree_path, hook):
            call_order.append(hook)

        def mock_rebase(path, base):
            call_order.append("rebase")

        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(1, 3)),
            patch("grove.workspace._run_hook", side_effect=mock_hook),
            patch("grove.workspace.git.rebase_onto", side_effect=mock_rebase),
        ):
            results = workspace.sync_workspace(sample_workspace)
        assert results[0]["result"] == "rebased (3 new commits)"
        assert call_order == ["pre_sync", "rebase", "post_sync"]

    def test_post_sync_not_on_conflict(self, tmp_grove, sample_workspace):
        hooks_called: list[str] = []

        def mock_hook(repo_name, source_repo, worktree_path, hook):
            hooks_called.append(hook)

        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 2)),
            patch("grove.workspace._run_hook", side_effect=mock_hook),
            patch("grove.workspace.git.rebase_onto", side_effect=GitError("conflict")),
            patch("grove.workspace.git.rebase_abort"),
        ):
            results = workspace.sync_workspace(sample_workspace)
        assert results[0]["result"] == "conflict"
        assert "pre_sync" in hooks_called
        assert "post_sync" not in hooks_called

    def test_pre_sync_not_when_up_to_date(self, tmp_grove, sample_workspace):
        hooks_called: list[str] = []

        def mock_hook(repo_name, source_repo, worktree_path, hook):
            hooks_called.append(hook)

        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(1, 0)),
            patch("grove.workspace._run_hook", side_effect=mock_hook),
        ):
            results = workspace.sync_workspace(sample_workspace)
        assert results[0]["result"] == "up to date"
        assert hooks_called == []

    def test_post_sync_after_successful_rebase(self, tmp_grove, sample_workspace):
        hooks_called: list[str] = []

        def mock_hook(repo_name, source_repo, worktree_path, hook):
            hooks_called.append(hook)

        with (
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.repo_base_branch", return_value="origin/main"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 5)),
            patch("grove.workspace._run_hook", side_effect=mock_hook),
            patch("grove.workspace.git.rebase_onto"),
        ):
            results = workspace.sync_workspace(sample_workspace)
        assert results[0]["result"] == "rebased (5 new commits)"
        assert "post_sync" in hooks_called


class TestAddRepoToWorkspace:
    def test_success(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        new_repo = tmp_grove["repos_dir"] / "svc-api"
        new_repo.mkdir()

        with (
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=True),
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.worktree_add"),
            patch("grove.workspace.git.read_grove_config", return_value={}),
        ):
            result = workspace.add_repo_to_workspace(sample_workspace, {"svc-api": new_repo}, cfg)
        assert result is not None
        assert len(result) == 1
        assert result[0].repo_name == "svc-api"
        # State updated
        saved = state.get_workspace("test-ws")
        assert len(saved.repos) == 2

    def test_already_in_workspace_skip(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        existing = tmp_grove["repos_dir"] / "svc-auth"
        existing.mkdir(exist_ok=True)

        result = workspace.add_repo_to_workspace(sample_workspace, {"svc-auth": existing}, cfg)
        assert result is None

    def test_branch_conflict(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        new_repo = tmp_grove["repos_dir"] / "svc-api"
        new_repo.mkdir()

        with patch("grove.workspace.git.worktree_has_branch", return_value=True):
            result = workspace.add_repo_to_workspace(sample_workspace, {"svc-api": new_repo}, cfg)
        assert result is None

    def test_branch_creation(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        new_repo = tmp_grove["repos_dir"] / "svc-api"
        new_repo.mkdir()

        with (
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=False),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.create_branch") as mock_create,
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.worktree_add"),
            patch("grove.workspace.git.read_grove_config", return_value={}),
        ):
            result = workspace.add_repo_to_workspace(sample_workspace, {"svc-api": new_repo}, cfg)
        assert result is not None
        mock_create.assert_called_once()

    def test_setup_hook_runs(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        new_repo = tmp_grove["repos_dir"] / "svc-api"
        new_repo.mkdir()

        with (
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=True),
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.worktree_add"),
            patch("grove.workspace.subprocess.run") as mock_sub,
            patch(
                "grove.workspace.git.read_grove_config",
                return_value={"setup": "npm install"},
            ),
            patch(
                "grove.workspace.git.repo_hook_commands",
                return_value=["npm install"],
            ),
        ):
            result = workspace.add_repo_to_workspace(sample_workspace, {"svc-api": new_repo}, cfg)
        assert result is not None
        assert mock_sub.called

    def test_rollback_does_not_delete_workspace_dir(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        new_repo = tmp_grove["repos_dir"] / "svc-api"
        new_repo.mkdir()

        with (
            patch("grove.workspace.git.worktree_has_branch", return_value=False),
            patch("grove.workspace.git.branch_exists", return_value=True),
            patch("grove.workspace.git.fetch"),
            patch("grove.workspace.git.worktree_add", side_effect=GitError("disk full")),
            patch("grove.workspace.git.worktree_remove"),
        ):
            result = workspace.add_repo_to_workspace(sample_workspace, {"svc-api": new_repo}, cfg)
        assert result is None
        # Workspace dir should still exist
        assert sample_workspace.path.exists()


class TestRemoveRepoFromWorkspace:
    def test_success(self, tmp_grove):
        ws_path = tmp_grove["workspace_dir"] / "test-ws"
        ws_path.mkdir()
        ws = Workspace(
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
                RepoWorktree(
                    repo_name="svc-api",
                    source_repo=tmp_grove["repos_dir"] / "svc-api",
                    worktree_path=ws_path / "svc-api",
                    branch="feat/test",
                ),
            ],
        )
        state.add_workspace(ws)

        with (
            patch("grove.workspace._run_hook"),
            patch("grove.workspace.git.worktree_remove"),
        ):
            ok = workspace.remove_repo_from_workspace(ws, ["svc-auth"])
        assert ok is True
        saved = state.get_workspace("test-ws")
        assert len(saved.repos) == 1
        assert saved.repos[0].repo_name == "svc-api"

    def test_not_in_workspace_skip(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        ok = workspace.remove_repo_from_workspace(sample_workspace, ["nonexistent"])
        assert ok is True  # nothing to remove = success

    def test_teardown_hook_runs(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        wt_path = sample_workspace.repos[0].worktree_path
        wt_path.mkdir(parents=True, exist_ok=True)
        hooks: list[str] = []

        def mock_hook(repo_name, source_repo, worktree_path, hook):
            hooks.append(hook)

        with (
            patch("grove.workspace._run_hook", side_effect=mock_hook),
            patch("grove.workspace.git.worktree_remove"),
        ):
            workspace.remove_repo_from_workspace(sample_workspace, ["svc-auth"])
        assert "teardown" in hooks

    def test_partial_failure(self, tmp_grove):
        ws_path = tmp_grove["workspace_dir"] / "test-ws"
        ws_path.mkdir()
        ws = Workspace(
            name="test-ws",
            path=ws_path,
            branch="feat/test",
            repos=[
                RepoWorktree(
                    repo_name="good",
                    source_repo=tmp_grove["repos_dir"] / "good",
                    worktree_path=ws_path / "good",
                    branch="feat/test",
                ),
                RepoWorktree(
                    repo_name="bad",
                    source_repo=tmp_grove["repos_dir"] / "bad",
                    worktree_path=ws_path / "bad",
                    branch="feat/test",
                ),
            ],
        )
        state.add_workspace(ws)

        def failing_remove(repo, path):
            if "bad" in str(path):
                raise GitError("cannot remove")

        with (
            patch("grove.workspace._run_hook"),
            patch("grove.workspace.git.worktree_remove", side_effect=failing_remove),
        ):
            ok = workspace.remove_repo_from_workspace(ws, ["good", "bad"])
        assert ok is False
        # Good repo should be removed from state
        saved = state.get_workspace("test-ws")
        assert len(saved.repos) == 1
        assert saved.repos[0].repo_name == "bad"


class TestRenameWorkspace:
    def test_success(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        with patch("grove.workspace.git.worktree_repair"):
            result = workspace.rename_workspace("test-ws", "new-name", cfg)
        assert result is True
        assert state.get_workspace("test-ws") is None
        saved = state.get_workspace("new-name")
        assert saved is not None
        assert saved.path == cfg.workspace_dir / "new-name"

    def test_not_found(self, tmp_grove):
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        assert workspace.rename_workspace("nope", "new", cfg) is False

    def test_name_taken(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        other = Workspace(
            name="other",
            path=tmp_grove["workspace_dir"] / "other",
            branch="feat/x",
            repos=[],
        )
        (tmp_grove["workspace_dir"] / "other").mkdir()
        state.add_workspace(other)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        assert workspace.rename_workspace("test-ws", "other", cfg) is False

    def test_dir_exists(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        # Create target dir manually (not as workspace)
        (cfg.workspace_dir / "conflict").mkdir()
        assert workspace.rename_workspace("test-ws", "conflict", cfg) is False

    def test_paths_updated(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        with patch("grove.workspace.git.worktree_repair"):
            workspace.rename_workspace("test-ws", "renamed", cfg)
        saved = state.get_workspace("renamed")
        for repo_wt in saved.repos:
            assert "renamed" in str(repo_wt.worktree_path)

    def test_rollback_on_directory_rename_failure(self, tmp_grove, sample_workspace):
        """If directory rename fails, state reverts to original values."""
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        with patch.object(Path, "rename", side_effect=OSError("permission denied")):
            result = workspace.rename_workspace("test-ws", "new-name", cfg)
        assert result is False
        # State should still have the old workspace
        ws = state.get_workspace("test-ws")
        assert ws is not None
        assert ws.path == sample_workspace.path
        # New name should not exist in state
        assert state.get_workspace("new-name") is None

    def test_created_at_preserved(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        original_created = sample_workspace.created_at
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        with patch("grove.workspace.git.worktree_repair"):
            workspace.rename_workspace("test-ws", "renamed", cfg)
        saved = state.get_workspace("renamed")
        assert saved.created_at == original_created

    def test_worktree_repair_called(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        with patch("grove.workspace.git.worktree_repair") as mock_repair:
            workspace.rename_workspace("test-ws", "renamed", cfg)
        assert mock_repair.called

    def test_repair_failure_does_not_abort(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        with patch("grove.workspace.git.worktree_repair", side_effect=GitError("broken")):
            result = workspace.rename_workspace("test-ws", "renamed", cfg)
        assert result is True
        assert state.get_workspace("renamed") is not None


class TestAllWorkspacesSummary:
    def test_empty(self, tmp_grove):
        result = workspace.all_workspaces_summary()
        assert result == []

    def test_all_clean(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        with (
            patch("grove.workspace.git.current_branch", return_value="feat/test"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 0)),
        ):
            results = workspace.all_workspaces_summary()
        assert len(results) == 1
        assert results[0]["name"] == "test-ws"
        assert "1 clean" in results[0]["status"]

    def test_mixed_status(self, tmp_grove):
        ws_path = tmp_grove["workspace_dir"] / "mixed"
        ws_path.mkdir()
        ws = Workspace(
            name="mixed",
            path=ws_path,
            branch="feat/x",
            repos=[
                RepoWorktree(
                    repo_name="clean-repo",
                    source_repo=tmp_grove["repos_dir"] / "clean-repo",
                    worktree_path=ws_path / "clean-repo",
                    branch="feat/x",
                ),
                RepoWorktree(
                    repo_name="dirty-repo",
                    source_repo=tmp_grove["repos_dir"] / "dirty-repo",
                    worktree_path=ws_path / "dirty-repo",
                    branch="feat/x",
                ),
            ],
        )
        state.add_workspace(ws)

        def status_side_effect(path):
            if "dirty" in str(path):
                return " M file.py"
            return ""

        with (
            patch("grove.workspace.git.current_branch", return_value="feat/x"),
            patch("grove.workspace.git.repo_status", side_effect=status_side_effect),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 0)),
        ):
            results = workspace.all_workspaces_summary()
        assert len(results) == 1
        assert "1 clean" in results[0]["status"]
        assert "1 modified" in results[0]["status"]

    def test_multi_workspace(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        ws2_path = tmp_grove["workspace_dir"] / "ws2"
        ws2_path.mkdir()
        ws2 = Workspace(
            name="ws2",
            path=ws2_path,
            branch="feat/other",
            repos=[],
        )
        state.add_workspace(ws2)

        with (
            patch("grove.workspace.git.current_branch", return_value="feat/test"),
            patch("grove.workspace.git.repo_status", return_value=""),
            patch("grove.workspace.git.repo_base_branch", return_value=None),
            patch("grove.workspace.git.default_branch", return_value="origin/main"),
            patch("grove.workspace.git.commits_ahead_behind", return_value=(0, 0)),
        ):
            results = workspace.all_workspaces_summary()
        assert len(results) == 2
        names = {r["name"] for r in results}
        assert names == {"test-ws", "ws2"}

    def test_error_isolation(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        with (
            patch(
                "grove.workspace.git.current_branch",
                side_effect=GitError("broken"),
            ),
        ):
            results = workspace.all_workspaces_summary()
        assert len(results) == 1
        # Should still get a result even with error
        assert results[0]["name"] == "test-ws"


class TestDiagnoseWorkspaces:
    def test_no_issues(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)
        # Create worktree path
        wt_path = sample_workspace.repos[0].worktree_path
        wt_path.mkdir(parents=True, exist_ok=True)
        # Create source repo
        src = sample_workspace.repos[0].source_repo
        src.mkdir(parents=True, exist_ok=True)

        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        with patch(
            "grove.workspace.git.worktree_list",
            return_value=[
                {"path": str(wt_path.resolve()), "branch": "feat/test"},
            ],
        ):
            issues = workspace.diagnose_workspaces(cfg)
        assert issues == []

    def test_workspace_dir_missing(self, tmp_grove):
        ws_path = tmp_grove["workspace_dir"] / "gone"
        # Don't create the dir
        ws = Workspace(name="gone", path=ws_path, branch="feat/x", repos=[])
        state.add_workspace(ws)

        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        issues = workspace.diagnose_workspaces(cfg)
        assert len(issues) == 1
        assert issues[0].issue == "workspace directory missing"

    def test_source_repo_missing(self, tmp_grove):
        ws_path = tmp_grove["workspace_dir"] / "test-ws"
        ws_path.mkdir()
        wt_path = ws_path / "svc-auth"
        wt_path.mkdir()
        ws = Workspace(
            name="test-ws",
            path=ws_path,
            branch="feat/test",
            repos=[
                RepoWorktree(
                    repo_name="svc-auth",
                    source_repo=tmp_grove["repos_dir"] / "nonexistent",
                    worktree_path=wt_path,
                    branch="feat/test",
                ),
            ],
        )
        state.add_workspace(ws)

        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        issues = workspace.diagnose_workspaces(cfg)
        assert len(issues) == 1
        assert issues[0].issue == "source repo missing"

    def test_worktree_dir_missing(self, tmp_grove):
        ws_path = tmp_grove["workspace_dir"] / "test-ws"
        ws_path.mkdir()
        src = tmp_grove["repos_dir"] / "svc-auth"
        src.mkdir()
        ws = Workspace(
            name="test-ws",
            path=ws_path,
            branch="feat/test",
            repos=[
                RepoWorktree(
                    repo_name="svc-auth",
                    source_repo=src,
                    worktree_path=ws_path / "svc-auth",  # not created
                    branch="feat/test",
                ),
            ],
        )
        state.add_workspace(ws)

        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        issues = workspace.diagnose_workspaces(cfg)
        assert len(issues) == 1
        assert issues[0].issue == "worktree directory missing"

    def test_git_unregistered(self, tmp_grove):
        ws_path = tmp_grove["workspace_dir"] / "test-ws"
        ws_path.mkdir()
        wt_path = ws_path / "svc-auth"
        wt_path.mkdir()
        src = tmp_grove["repos_dir"] / "svc-auth"
        src.mkdir()
        ws = Workspace(
            name="test-ws",
            path=ws_path,
            branch="feat/test",
            repos=[
                RepoWorktree(
                    repo_name="svc-auth",
                    source_repo=src,
                    worktree_path=wt_path,
                    branch="feat/test",
                ),
            ],
        )
        state.add_workspace(ws)

        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        # Return empty worktree list — not registered
        with patch("grove.workspace.git.worktree_list", return_value=[]):
            issues = workspace.diagnose_workspaces(cfg)
        assert len(issues) == 1
        assert issues[0].issue == "worktree not registered in git"

    def test_git_error(self, tmp_grove):
        ws_path = tmp_grove["workspace_dir"] / "test-ws"
        ws_path.mkdir()
        wt_path = ws_path / "svc-auth"
        wt_path.mkdir()
        src = tmp_grove["repos_dir"] / "svc-auth"
        src.mkdir()
        ws = Workspace(
            name="test-ws",
            path=ws_path,
            branch="feat/test",
            repos=[
                RepoWorktree(
                    repo_name="svc-auth",
                    source_repo=src,
                    worktree_path=wt_path,
                    branch="feat/test",
                ),
            ],
        )
        state.add_workspace(ws)

        cfg = Config(
            repo_dirs=[tmp_grove["repos_dir"]],
            workspace_dir=tmp_grove["workspace_dir"],
        )
        with patch("grove.workspace.git.worktree_list", side_effect=GitError("broken")):
            issues = workspace.diagnose_workspaces(cfg)
        assert len(issues) == 1
        assert "git error" in issues[0].issue


class TestFixWorkspaceIssues:
    def test_removes_stale_workspace(self, tmp_grove):
        ws = Workspace(
            name="gone",
            path=tmp_grove["workspace_dir"] / "gone",
            branch="feat/x",
            repos=[],
        )
        state.add_workspace(ws)

        issues = [
            workspace.DoctorIssue(
                workspace_name="gone",
                repo_name=None,
                issue="workspace directory missing",
                suggested_action="remove stale state entry",
            )
        ]
        fixed = workspace.fix_workspace_issues(issues)
        assert fixed == 1
        assert state.get_workspace("gone") is None

    def test_removes_stale_repo(self, tmp_grove):
        ws_path = tmp_grove["workspace_dir"] / "test-ws"
        ws_path.mkdir()
        ws = Workspace(
            name="test-ws",
            path=ws_path,
            branch="feat/test",
            repos=[
                RepoWorktree(
                    repo_name="good",
                    source_repo=tmp_grove["repos_dir"] / "good",
                    worktree_path=ws_path / "good",
                    branch="feat/test",
                ),
                RepoWorktree(
                    repo_name="stale",
                    source_repo=tmp_grove["repos_dir"] / "stale",
                    worktree_path=ws_path / "stale",
                    branch="feat/test",
                ),
            ],
        )
        state.add_workspace(ws)

        issues = [
            workspace.DoctorIssue(
                workspace_name="test-ws",
                repo_name="stale",
                issue="source repo missing",
                suggested_action="remove stale repo entry",
            )
        ]
        fixed = workspace.fix_workspace_issues(issues)
        assert fixed == 1
        saved = state.get_workspace("test-ws")
        assert len(saved.repos) == 1
        assert saved.repos[0].repo_name == "good"

    def test_skips_git_issues(self, tmp_grove, sample_workspace):
        state.add_workspace(sample_workspace)

        issues = [
            workspace.DoctorIssue(
                workspace_name="test-ws",
                repo_name="svc-auth",
                issue="worktree not registered in git",
                suggested_action="re-register with git worktree repair",
            )
        ]
        fixed = workspace.fix_workspace_issues(issues)
        assert fixed == 0


class TestRunWorkspace:
    def test_no_run_hook_returns_zero(self, tmp_grove, sample_workspace):
        with patch("grove.workspace.git.repo_hook_commands", return_value=[]):
            count = workspace.run_workspace(sample_workspace)
        assert count == 0

    def test_runs_processes(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.repo_hook_commands") as mock_cmds,
            patch("grove.workspace.subprocess.Popen") as mock_popen,
            patch("grove.workspace._run_hook"),
        ):
            mock_cmds.return_value = ["echo hello"]
            mock_proc = mock_popen.return_value
            mock_proc.wait.return_value = 0
            count = workspace.run_workspace(sample_workspace)
        assert count == 1
        mock_popen.assert_called_once()

    def test_pre_run_before_processes(self, tmp_grove, sample_workspace):
        call_order: list[str] = []

        def mock_hook(repo_name, source_repo, worktree_path, hook):
            call_order.append(hook)

        with (
            patch("grove.workspace.git.repo_hook_commands", return_value=["echo hi"]),
            patch("grove.workspace._run_hook", side_effect=mock_hook),
            patch("grove.workspace.subprocess.Popen") as mock_popen,
        ):
            mock_proc = mock_popen.return_value
            mock_proc.wait.return_value = 0
            workspace.run_workspace(sample_workspace)
        assert call_order[0] == "pre_run"
        assert call_order[-1] == "post_run"

    def test_post_run_on_keyboard_interrupt(self, tmp_grove, sample_workspace):
        hooks_called: list[str] = []

        def mock_hook(repo_name, source_repo, worktree_path, hook):
            hooks_called.append(hook)

        with (
            patch("grove.workspace.git.repo_hook_commands", return_value=["sleep 100"]),
            patch("grove.workspace._run_hook", side_effect=mock_hook),
            patch("grove.workspace.subprocess.Popen") as mock_popen,
        ):
            mock_proc = mock_popen.return_value
            mock_proc.wait.side_effect = KeyboardInterrupt
            mock_proc.terminate.return_value = None
            workspace.run_workspace(sample_workspace)
        assert "post_run" in hooks_called

    def test_processes_terminated_on_interrupt(self, tmp_grove, sample_workspace):
        with (
            patch("grove.workspace.git.repo_hook_commands", return_value=["sleep 100"]),
            patch("grove.workspace._run_hook"),
            patch("grove.workspace.subprocess.Popen") as mock_popen,
        ):
            mock_proc = mock_popen.return_value
            mock_proc.wait.side_effect = [KeyboardInterrupt, None]
            mock_proc.terminate.return_value = None
            workspace.run_workspace(sample_workspace)
        mock_proc.terminate.assert_called_once()


class TestGetRunnable:
    def test_returns_repos_with_run_hook(self, tmp_grove, sample_workspace):
        with patch(
            "grove.workspace.git.repo_hook_commands",
            side_effect=lambda _src, hook: ["npm start"] if hook == "run" else [],
        ):
            result = workspace.get_runnable(sample_workspace)
        assert len(result) == 1
        assert result[0][0].repo_name == "svc-auth"
        assert result[0][1] == ["npm start"]

    def test_empty_when_no_run_hook(self, tmp_grove, sample_workspace):
        with patch("grove.workspace.git.repo_hook_commands", return_value=[]):
            result = workspace.get_runnable(sample_workspace)
        assert result == []

    def test_multiple_commands(self, tmp_grove, sample_workspace):
        with patch(
            "grove.workspace.git.repo_hook_commands",
            side_effect=lambda _src, hook: ["npm install", "npm start"] if hook == "run" else [],
        ):
            result = workspace.get_runnable(sample_workspace)
        assert len(result) == 1
        assert result[0][1] == ["npm install", "npm start"]


class TestRunPreHooks:
    def test_calls_pre_run_hook(self, tmp_grove, sample_workspace):
        wt = sample_workspace.repos[0]
        runnable = [(wt, ["npm start"])]
        with patch("grove.workspace._run_hook") as mock_hook:
            workspace.run_pre_hooks(runnable)
        mock_hook.assert_called_once_with(wt.repo_name, wt.source_repo, wt.worktree_path, "pre_run")

    def test_noop_when_empty(self):
        # Should not raise
        workspace.run_pre_hooks([])


class TestRunPostHooks:
    def test_calls_post_run_hook(self, tmp_grove, sample_workspace):
        wt = sample_workspace.repos[0]
        runnable = [(wt, ["npm start"])]
        with patch("grove.workspace._run_hook") as mock_hook:
            workspace.run_post_hooks(runnable)
        mock_hook.assert_called_once_with(
            wt.repo_name, wt.source_repo, wt.worktree_path, "post_run"
        )

    def test_noop_when_empty(self):
        workspace.run_post_hooks([])
