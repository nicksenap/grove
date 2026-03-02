"""Git subprocess operations."""

from __future__ import annotations

import functools
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


@functools.cache
def read_grove_config(path: Path) -> dict:
    """Read ``.grove.toml`` from *path* and return the parsed dict (empty if absent).

    Cached for the lifetime of the process — safe because ``.grove.toml``
    doesn't change during a single CLI invocation.
    """
    import tomllib

    grove_toml = path / ".grove.toml"
    if not grove_toml.exists():
        return {}
    with open(grove_toml, "rb") as f:
        return tomllib.load(f)


def repo_base_branch(repo: Path) -> str | None:
    """Return ``origin/<base_branch>`` from ``.grove.toml``, or ``None``."""
    base = read_grove_config(repo).get("base_branch")
    return f"origin/{base}" if base else None


def resolve_base_branch(repo: Path) -> str | None:
    """Return the base branch for *repo*: ``.grove.toml`` > auto-detect > ``None``."""
    base = repo_base_branch(repo)
    if base is not None:
        return base
    try:
        return default_branch(repo)
    except GitError:
        return None


def repo_hook_commands(source_repo: Path, hook: str) -> list[str]:
    """Return the command list for *hook* from ``.grove.toml`` (empty if absent)."""
    value = read_grove_config(source_repo).get(hook)
    if not value:
        return []
    return [value] if isinstance(value, str) else list(value)


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


def worktree_repair(repo: Path, worktree_path: Path) -> None:
    """Repair worktree linkage after a directory move."""
    _run(["worktree", "repair", str(worktree_path)], cwd=repo)


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


def rebase_onto(path: Path, base: str) -> None:
    """Rebase the current branch onto *base*."""
    _run(["rebase", base], cwd=path)


def rebase_abort(path: Path) -> None:
    """Abort an in-progress rebase."""
    _run(["rebase", "--abort"], cwd=path)


def commits_ahead_behind(path: Path, upstream: str) -> tuple[int, int]:
    """Return ``(ahead, behind)`` commit counts relative to *upstream*.

    Uses ``git rev-list --left-right --count upstream...HEAD``.
    """
    result = _run(["rev-list", "--left-right", "--count", f"{upstream}...HEAD"], cwd=path)
    parts = result.stdout.strip().split()
    if len(parts) != 2:
        raise GitError(f"Unexpected rev-list output: {result.stdout.strip()}")
    try:
        # left = upstream-only (behind), right = HEAD-only (ahead)
        return int(parts[1]), int(parts[0])
    except ValueError as e:
        raise GitError(f"Could not parse rev-list output: {result.stdout.strip()}") from e


def pr_status(path: Path) -> dict | None:
    """Get PR info via GitHub CLI. Returns ``None`` if unavailable."""
    import json
    import shutil

    if not shutil.which("gh"):
        return None
    try:
        result = subprocess.run(
            ["gh", "pr", "view", "--json", "number,state,reviewDecision"],
            cwd=path,
            capture_output=True,
            text=True,
            check=True,
        )
        return json.loads(result.stdout)
    except (subprocess.CalledProcessError, json.JSONDecodeError, OSError):
        return None
