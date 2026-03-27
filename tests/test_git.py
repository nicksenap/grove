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


class TestWorktreeListDetachedHead:
    def test_detached_head_has_no_branch_key(self, mock_run):
        mock_run.return_value = MagicMock(
            stdout=(
                "worktree /repo\nbranch refs/heads/main\n\n"
                "worktree /ws/detached\nHEAD abc1234\ndetached\n"
            )
        )
        result = git.worktree_list(Path("/repo"))
        assert len(result) == 2
        assert result[0] == {"path": "/repo", "branch": "main"}
        assert result[1] == {"path": "/ws/detached"}
        assert "branch" not in result[1]

    def test_detached_head_not_matched_by_has_branch(self, mock_run):
        mock_run.return_value = MagicMock(stdout="worktree /ws/detached\nHEAD abc1234\ndetached\n")
        assert git.worktree_has_branch(Path("/repo"), "main") is False


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


class TestRepoBaseBranch:
    def test_returns_configured_branch(self, tmp_path):
        (tmp_path / ".grove.toml").write_text('base_branch = "stage"\n')
        assert git.repo_base_branch(tmp_path) == "origin/stage"

    def test_returns_none_when_no_file(self, tmp_path):
        assert git.repo_base_branch(tmp_path) is None

    def test_returns_none_when_no_key(self, tmp_path):
        (tmp_path / ".grove.toml").write_text("[other]\nfoo = 1\n")
        assert git.repo_base_branch(tmp_path) is None


class TestRebaseOnto:
    def test_success(self, mock_run):
        git.rebase_onto(Path("/ws/repo"), "origin/main")
        mock_run.assert_called_once_with(["rebase", "origin/main"], cwd=Path("/ws/repo"))

    def test_failure(self, mock_run):
        mock_run.side_effect = GitError("conflict")
        with pytest.raises(GitError):
            git.rebase_onto(Path("/ws/repo"), "origin/main")


class TestRebaseAbort:
    def test_success(self, mock_run):
        git.rebase_abort(Path("/ws/repo"))
        mock_run.assert_called_once_with(["rebase", "--abort"], cwd=Path("/ws/repo"))


class TestCommitsAheadBehind:
    def test_parses_output(self, mock_run):
        # left=2 (behind), right=3 (ahead)
        mock_run.return_value = MagicMock(stdout="2\t3\n")
        ahead, behind = git.commits_ahead_behind(Path("/ws"), "origin/main")
        assert ahead == 3
        assert behind == 2
        mock_run.assert_called_once_with(
            ["rev-list", "--left-right", "--count", "origin/main...HEAD"],
            cwd=Path("/ws"),
        )

    def test_zero_drift(self, mock_run):
        mock_run.return_value = MagicMock(stdout="0\t0\n")
        ahead, behind = git.commits_ahead_behind(Path("/ws"), "origin/main")
        assert ahead == 0
        assert behind == 0

    def test_git_failure(self, mock_run):
        mock_run.side_effect = GitError("bad ref")
        with pytest.raises(GitError):
            git.commits_ahead_behind(Path("/ws"), "origin/nope")

    def test_bad_output_raises(self, mock_run):
        mock_run.return_value = MagicMock(stdout="garbage\n")
        with pytest.raises(GitError, match="Unexpected rev-list output"):
            git.commits_ahead_behind(Path("/ws"), "origin/main")


class TestPrStatus:
    def test_returns_none_when_gh_missing(self):
        with patch("shutil.which", return_value=None):
            assert git.pr_status(Path("/ws")) is None

    def test_returns_pr_data(self):
        pr_json = '{"number": 42, "state": "OPEN", "reviewDecision": "APPROVED"}'
        with (
            patch("shutil.which", return_value="/usr/bin/gh"),
            patch("subprocess.run") as mock_sub,
        ):
            mock_sub.return_value = MagicMock(stdout=pr_json, returncode=0)
            result = git.pr_status(Path("/ws"))
        assert result == {"number": 42, "state": "OPEN", "reviewDecision": "APPROVED"}

    def test_returns_none_on_no_pr(self):
        with (
            patch("shutil.which", return_value="/usr/bin/gh"),
            patch("subprocess.run") as mock_sub,
        ):
            mock_sub.side_effect = subprocess.CalledProcessError(1, "gh", stderr="no PR")
            assert git.pr_status(Path("/ws")) is None


class TestRemoteUrl:
    def test_returns_url(self, mock_run):
        mock_run.return_value = MagicMock(stdout="git@github.com:org/repo.git\n")
        assert git.remote_url(Path("/repo")) == "git@github.com:org/repo.git"

    def test_returns_none_on_error(self, mock_run):
        mock_run.side_effect = GitError("no remote")
        assert git.remote_url(Path("/repo")) is None

    def test_custom_remote(self, mock_run):
        mock_run.return_value = MagicMock(stdout="https://github.com/org/repo.git\n")
        git.remote_url(Path("/repo"), remote="upstream")
        mock_run.assert_called_once_with(["remote", "get-url", "upstream"], cwd=Path("/repo"))


class TestParseRemoteName:
    def test_ssh_format(self):
        assert git.parse_remote_name("git@github.com:org/repo.git") == "org/repo"

    def test_ssh_no_git_suffix(self):
        assert git.parse_remote_name("git@github.com:org/repo") == "org/repo"

    def test_https_format(self):
        assert git.parse_remote_name("https://github.com/org/repo.git") == "org/repo"

    def test_https_no_git_suffix(self):
        assert git.parse_remote_name("https://github.com/org/repo") == "org/repo"

    def test_nested_path(self):
        assert git.parse_remote_name("git@gitlab.com:group/sub/repo.git") == "group/sub/repo"

    def test_invalid_url(self):
        assert git.parse_remote_name("not-a-url") is None


class TestRunIntegration:
    """Test the actual _run function with subprocess mocking."""

    def test_raises_git_error_on_failure(self):
        with patch("subprocess.run") as mock_sub:
            mock_sub.side_effect = subprocess.CalledProcessError(1, "git", stderr="fatal: error")
            with pytest.raises(GitError, match="fatal: error"):
                git._run(["status"], cwd=Path("/tmp"))

    def test_uses_devnull_stdin_and_no_prompt_env(self):
        with patch("subprocess.run") as mock_sub:
            mock_sub.return_value = MagicMock()
            git._run(["status"], cwd=Path("/tmp"))
            call_kwargs = mock_sub.call_args.kwargs
            assert call_kwargs["stdin"] == subprocess.DEVNULL
            assert call_kwargs["env"]["GIT_TERMINAL_PROMPT"] == "0"
            assert "BatchMode=yes" in call_kwargs["env"]["GIT_SSH_COMMAND"]

    def test_auth_error_gives_helpful_message(self):
        with patch("subprocess.run") as mock_sub:
            mock_sub.side_effect = subprocess.CalledProcessError(
                128, "git", stderr="Permission denied (publickey)."
            )
            with pytest.raises(GitError, match="ssh-add") as exc_info:
                git._run(["fetch", "--all"], cwd=Path("/tmp"))
            assert "non-interactively" in str(exc_info.value)

    def test_terminal_prompts_disabled_gives_helpful_message(self):
        with patch("subprocess.run") as mock_sub:
            mock_sub.side_effect = subprocess.CalledProcessError(
                128, "git", stderr="terminal prompts disabled"
            )
            with pytest.raises(GitError, match="ssh-add") as exc_info:
                git._run(["fetch", "--all"], cwd=Path("/tmp"))
            assert "credential helper" in str(exc_info.value)


class TestIsAuthError:
    def test_permission_denied(self):
        assert git._is_auth_error("Permission denied (publickey).") is True

    def test_host_key_verification(self):
        assert git._is_auth_error("Host key verification failed") is True

    def test_terminal_prompts_disabled(self):
        assert git._is_auth_error("terminal prompts disabled") is True

    def test_unrelated_error(self):
        assert git._is_auth_error("fatal: not a git repository") is False
