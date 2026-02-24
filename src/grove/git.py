"""Git subprocess operations."""

from __future__ import annotations

import subprocess
from pathlib import Path


class GitError(Exception):
    """Raised when a git command fails."""


def _run(args: list[str], cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    """Run a git command and return the result."""
    try:
        return subprocess.run(
            ["git", *args],
            cwd=cwd,
            capture_output=True,
            text=True,
            check=True,
        )
    except subprocess.CalledProcessError as e:
        raise GitError(e.stderr.strip() or e.stdout.strip()) from e


def is_git_repo(path: Path) -> bool:
    """Check if a directory is a git repository."""
    try:
        _run(["rev-parse", "--git-dir"], cwd=path)
        return True
    except (GitError, FileNotFoundError):
        return False


def branch_exists(repo: Path, branch: str) -> bool:
    """Check if a branch exists in the repo (local only)."""
    try:
        _run(["rev-parse", "--verify", f"refs/heads/{branch}"], cwd=repo)
        return True
    except GitError:
        return False


def fetch(repo: Path) -> None:
    """Fetch latest from all remotes."""
    _run(["fetch", "--all", "--quiet"], cwd=repo)


def default_branch(repo: Path) -> str:
    """Return the remote default branch ref (e.g. ``origin/main``)."""
    try:
        result = _run(["symbolic-ref", "refs/remotes/origin/HEAD", "--short"], cwd=repo)
        return result.stdout.strip()
    except GitError:
        # origin/HEAD not set — probe common names
        for name in ("main", "master"):
            try:
                _run(["rev-parse", "--verify", f"refs/remotes/origin/{name}"], cwd=repo)
                return f"origin/{name}"
            except GitError:
                continue
        raise GitError("Could not determine default branch") from None


def create_branch(repo: Path, branch: str, start_point: str | None = None) -> None:
    """Create a new branch, optionally from *start_point*."""
    cmd = ["branch", branch]
    if start_point:
        cmd.append(start_point)
    _run(cmd, cwd=repo)


def worktree_add(repo: Path, worktree_path: Path, branch: str) -> None:
    """Add a git worktree at the given path on the given branch."""
    _run(["worktree", "add", str(worktree_path), branch], cwd=repo)


def worktree_remove(repo: Path, worktree_path: Path) -> None:
    """Remove a git worktree."""
    _run(["worktree", "remove", str(worktree_path), "--force"], cwd=repo)


def worktree_list(repo: Path) -> list[dict[str, str]]:
    """List worktrees for a repo. Returns list of {path, branch} dicts."""
    result = _run(["worktree", "list", "--porcelain"], cwd=repo)
    worktrees: list[dict[str, str]] = []
    current: dict[str, str] = {}
    for line in result.stdout.splitlines():
        if line.startswith("worktree "):
            current = {"path": line.removeprefix("worktree ")}
        elif line.startswith("branch "):
            current["branch"] = line.removeprefix("branch refs/heads/")
        elif line == "" and current:
            worktrees.append(current)
            current = {}
    if current:
        worktrees.append(current)
    return worktrees


def worktree_has_branch(repo: Path, branch: str) -> bool:
    """Check if a worktree already exists for this branch in the repo."""
    return any(wt.get("branch") == branch for wt in worktree_list(repo))


def repo_status(path: Path) -> str:
    """Get short git status for a path."""
    result = _run(["status", "--short"], cwd=path)
    return result.stdout.strip()


def current_branch(path: Path) -> str:
    """Get the current branch name."""
    result = _run(["branch", "--show-current"], cwd=path)
    return result.stdout.strip()
