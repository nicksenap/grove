"""Tests for grove.git — mock subprocess.run for all git wrappers."""

from __future__ import annotations

import subprocess
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

from grove import git
from grove.git import GitError


@pytest.fixture()
def mock_run():
    with patch("grove.git._run") as m:
        yield m


class TestIsGitRepo:
    def test_true_for_git_repo(self, mock_run):
        mock_run.return_value = MagicMock()
        assert git.is_git_repo(Path("/repo")) is True
        mock_run.assert_called_once_with(["rev-parse", "--git-dir"], cwd=Path("/repo"))

    def test_false_for_non_repo(self, mock_run):
        mock_run.side_effect = GitError("not a repo")
        assert git.is_git_repo(Path("/nope")) is False


class TestBranchExists:
    def test_exists(self, mock_run):
        mock_run.return_value = MagicMock()
        assert git.branch_exists(Path("/repo"), "main") is True

    def test_not_exists(self, mock_run):
        mock_run.side_effect = GitError("not found")
        assert git.branch_exists(Path("/repo"), "nope") is False


class TestCreateBranch:
    def test_success(self, mock_run):
        git.create_branch(Path("/repo"), "feat/new")
        mock_run.assert_called_once_with(["branch", "feat/new"], cwd=Path("/repo"))

    def test_failure(self, mock_run):
        mock_run.side_effect = GitError("already exists")
        with pytest.raises(GitError):
            git.create_branch(Path("/repo"), "existing")


class TestWorktreeAdd:
    def test_success(self, mock_run):
        git.worktree_add(Path("/repo"), Path("/ws/repo"), "feat/x")
        mock_run.assert_called_once_with(
            ["worktree", "add", "/ws/repo", "feat/x"], cwd=Path("/repo")
        )


class TestWorktreeRemove:
    def test_success(self, mock_run):
        git.worktree_remove(Path("/repo"), Path("/ws/repo"))
        mock_run.assert_called_once_with(
            ["worktree", "remove", "/ws/repo", "--force"], cwd=Path("/repo")
        )


class TestWorktreeList:
    def test_parses_porcelain(self, mock_run):
        mock_run.return_value = MagicMock(
            stdout=(
                "worktree /repo\nbranch refs/heads/main\n\n"
                "worktree /ws/feat\nbranch refs/heads/feat/x\n"
            )
        )
        result = git.worktree_list(Path("/repo"))
        assert len(result) == 2
        assert result[0] == {"path": "/repo", "branch": "main"}
        assert result[1] == {"path": "/ws/feat", "branch": "feat/x"}


class TestWorktreeHasBranch:
    def test_found(self, mock_run):
        mock_run.return_value = MagicMock(stdout="worktree /repo\nbranch refs/heads/main\n")
        assert git.worktree_has_branch(Path("/repo"), "main") is True

    def test_not_found(self, mock_run):
        mock_run.return_value = MagicMock(stdout="worktree /repo\nbranch refs/heads/main\n")
        assert git.worktree_has_branch(Path("/repo"), "feat/x") is False


class TestRepoStatus:
    def test_clean(self, mock_run):
        mock_run.return_value = MagicMock(stdout="")
        assert git.repo_status(Path("/ws")) == ""

    def test_modified(self, mock_run):
        mock_run.return_value = MagicMock(stdout=" M file.py\n?? new.txt\n")
        assert git.repo_status(Path("/ws")) == "M file.py\n?? new.txt"


class TestCurrentBranch:
    def test_returns_branch(self, mock_run):
        mock_run.return_value = MagicMock(stdout="feat/login\n")
        assert git.current_branch(Path("/ws")) == "feat/login"


class TestRunIntegration:
    """Test the actual _run function with subprocess mocking."""

    def test_raises_git_error_on_failure(self):
        with patch("subprocess.run") as mock_sub:
            mock_sub.side_effect = subprocess.CalledProcessError(1, "git", stderr="fatal: error")
            with pytest.raises(GitError, match="fatal: error"):
                git._run(["status"], cwd=Path("/tmp"))
