"""Core workspace orchestration: create, delete, status."""

from __future__ import annotations

import contextlib
import shutil
import subprocess
from collections.abc import Callable
from concurrent.futures import ThreadPoolExecutor, as_completed
from pathlib import Path
from typing import Any

from grove import git, state
from grove.console import console, error, info, success, warning
from grove.git import GitError
from grove.models import Config, RepoWorktree, Workspace


def _parallel(
    fn: Callable[[str, Any], Any],
    items: list[tuple[str, Any]],
    label: str = "Processing",
) -> list[tuple[str, Any, Exception | None]]:
    """Run *fn(name, value)* for each ``(name, value)`` in *items* using threads.

    Returns a list of ``(name, result, error)`` tuples, one per item,
    preserving the original order of *items*.
    """
    if not items:
        return []

    order = {name: idx for idx, (name, _) in enumerate(items)}
    results: list[tuple[str, Any, Exception | None]] = [("", None, None)] * len(items)

    with (
        console.status(f"{label} {len(items)} repos…"),
        ThreadPoolExecutor() as pool,
    ):
        futures = {pool.submit(fn, name, val): name for name, val in items}
        for future in as_completed(futures):
            name = futures[future]
            try:
                results[order[name]] = (name, future.result(), None)
            except Exception as exc:
                results[order[name]] = (name, None, exc)

    return results


def create_workspace(
    name: str,
    repo_paths: dict[str, Path],
    branch: str,
    config: Config,
) -> Workspace | None:
    """Create a workspace with worktrees from multiple repos.

    Returns the Workspace on success, None on failure (with rollback).
    """
    workspace_path = config.workspace_dir / name

    # --- Validate upfront ---
    if state.get_workspace(name) is not None:
        error(f"Workspace [bold]{name}[/] already exists")
        return None

    # Check for duplicate branch usage (git worktree limitation)
    for repo_name, repo_path in repo_paths.items():
        if git.worktree_has_branch(repo_path, branch):
            error(
                f"Branch [bold]{branch}[/] already has a worktree in "
                f"[bold]{repo_name}[/] — git only allows one worktree per branch"
            )
            return None

    # --- Fetch latest from all repos (parallel) ---
    def _fetch_one(_name: str, path: Path) -> None:
        git.fetch(path)

    for repo_name, _result, exc in _parallel(_fetch_one, list(repo_paths.items()), "Fetching"):
        if exc is not None:
            warning(f"Could not fetch {repo_name}: {exc}")

    # --- Create workspace directory ---
    workspace_path.mkdir(parents=True, exist_ok=True)

    # --- Create worktrees with rollback ---
    created: list[RepoWorktree] = []
    for repo_name, repo_path in repo_paths.items():
        worktree_path = workspace_path / repo_name

        # Auto-create branch from the default branch if it doesn't exist
        if not git.branch_exists(repo_path, branch):
            # Per-repo .grove.toml > auto-detect
            base = git.repo_base_branch(repo_path)
            if base is None:
                try:
                    base = git.default_branch(repo_path)
                except GitError:
                    base = None
            info(
                f"Creating branch [bold]{branch}[/] in {repo_name}"
                + (f" from {base}" if base else "")
            )
            try:
                git.create_branch(repo_path, branch, start_point=base)
            except GitError as e:
                error(f"Failed to create branch in {repo_name}: {e}")
                _rollback(created, workspace_path)
                return None

        with console.status(f"Creating worktree for [bold]{repo_name}[/]..."):
            try:
                git.worktree_add(repo_path, worktree_path, branch)
            except GitError as e:
                error(f"Failed to create worktree for {repo_name}: {e}")
                _rollback(created, workspace_path)
                return None

        repo_wt = RepoWorktree(
            repo_name=repo_name,
            source_repo=repo_path,
            worktree_path=worktree_path,
            branch=branch,
        )
        created.append(repo_wt)
        success(f"{repo_name} -> {worktree_path}")

    # --- Run per-repo setup hooks (parallel) ---
    def _setup_one(_name: str, repo_wt: RepoWorktree) -> None:
        _run_setup(repo_wt.repo_name, repo_wt.source_repo, repo_wt.worktree_path)

    setup_items = [(wt.repo_name, wt) for wt in created]
    _parallel(_setup_one, setup_items, "Running setup for")

    workspace = Workspace(
        name=name,
        path=workspace_path,
        branch=branch,
        repos=created,
    )
    state.add_workspace(workspace)
    return workspace


def _run_setup(repo_name: str, source_repo: Path, worktree_path: Path) -> None:
    """Run setup commands from ``.grove.toml`` (read from source repo, run in worktree)."""
    cfg = git.read_grove_config(source_repo)
    setup = cfg.get("setup")
    if not setup:
        return

    commands = [setup] if isinstance(setup, str) else setup
    for cmd in commands:
        info(f"[{repo_name}] running: {cmd}")
        try:
            subprocess.run(
                cmd,
                cwd=worktree_path,
                shell=True,
                check=True,
            )
        except subprocess.CalledProcessError as e:
            warning(f"[{repo_name}] setup command failed (exit {e.returncode}): {cmd}")


def _rollback(created: list[RepoWorktree], workspace_path: Path) -> None:
    """Remove already-created worktrees on failure."""
    if not created:
        return
    warning("Rolling back created worktrees...")
    for repo_wt in created:
        with contextlib.suppress(GitError):
            git.worktree_remove(repo_wt.source_repo, repo_wt.worktree_path)
    if workspace_path.exists():
        shutil.rmtree(workspace_path, ignore_errors=True)


def delete_workspace(name: str) -> bool:
    """Delete a workspace: remove worktrees, folder, and state entry."""
    workspace = state.get_workspace(name)
    if workspace is None:
        error(f"Workspace [bold]{name}[/] not found")
        return False

    def _remove_one(_name: str, repo_wt: RepoWorktree) -> None:
        try:
            git.worktree_remove(repo_wt.source_repo, repo_wt.worktree_path)
        except GitError:
            # Try manual cleanup if worktree path exists
            if repo_wt.worktree_path.exists():
                shutil.rmtree(repo_wt.worktree_path, ignore_errors=True)
            raise  # re-raise so _parallel records the error

    items = [(wt.repo_name, wt) for wt in workspace.repos]
    for repo_name, _result, exc in _parallel(_remove_one, items, "Removing"):
        if exc is not None:
            warning(f"Could not remove worktree for {repo_name}: {exc}")
        else:
            success(f"Removed worktree: {repo_name}")

    if workspace.path.exists():
        shutil.rmtree(workspace.path, ignore_errors=True)

    state.remove_workspace(name)
    return True


def _sync_one_repo(_name: str, repo_wt: RepoWorktree) -> dict[str, str]:
    """Sync a single repo: fetch, determine base, rebase if needed."""
    # Fetch latest (continue with cached refs on failure)
    with contextlib.suppress(GitError):
        git.fetch(repo_wt.worktree_path)

    # Determine base branch
    base = git.repo_base_branch(repo_wt.source_repo)
    if base is None:
        try:
            base = git.default_branch(repo_wt.source_repo)
        except GitError:
            return {
                "repo": repo_wt.repo_name,
                "base": "?",
                "result": "error: could not determine base branch",
            }

    # Skip dirty worktrees — rebase on uncommitted changes is dangerous
    status_text = git.repo_status(repo_wt.worktree_path)
    if status_text:
        return {
            "repo": repo_wt.repo_name,
            "base": base,
            "result": "skipped: uncommitted changes",
        }

    # Check if rebase is needed
    try:
        _ahead, behind = git.commits_ahead_behind(repo_wt.worktree_path, base)
    except GitError:
        behind = -1  # unknown, attempt rebase anyway

    if behind == 0:
        return {
            "repo": repo_wt.repo_name,
            "base": base,
            "result": "up to date",
        }

    # Rebase
    try:
        git.rebase_onto(repo_wt.worktree_path, base)
        msg = f"rebased ({behind} new commits)" if behind > 0 else "rebased"
        return {
            "repo": repo_wt.repo_name,
            "base": base,
            "result": msg,
        }
    except GitError:
        # Abort failed rebase to leave worktree in a clean state
        with contextlib.suppress(GitError):
            git.rebase_abort(repo_wt.worktree_path)
        return {
            "repo": repo_wt.repo_name,
            "base": base,
            "result": "conflict",
        }


def sync_workspace(workspace: Workspace) -> list[dict[str, str]]:
    """Sync all repos by rebasing onto their base branches.

    Returns list of ``{repo, base, result}`` dicts.
    """
    items = [(wt.repo_name, wt) for wt in workspace.repos]
    results: list[dict[str, str]] = []
    for _name, result, exc in _parallel(_sync_one_repo, items, "Syncing"):
        if exc is not None:
            results.append({"repo": _name, "base": "?", "result": f"error: {exc}"})
        else:
            results.append(result)
    return results


def _status_one_repo(_name: str, repo_wt: RepoWorktree) -> dict[str, str]:
    """Get status for a single repo."""
    try:
        branch = git.current_branch(repo_wt.worktree_path)
        status_text = git.repo_status(repo_wt.worktree_path)

        # Ahead/behind (best-effort — never crash status for this)
        ahead_str = "-"
        behind_str = "-"
        try:
            base = git.repo_base_branch(repo_wt.source_repo)
            if base is None:
                base = git.default_branch(repo_wt.source_repo)
            ahead, behind = git.commits_ahead_behind(repo_wt.worktree_path, base)
            ahead_str = str(ahead)
            behind_str = str(behind)
        except Exception:
            pass

        return {
            "repo": repo_wt.repo_name,
            "branch": branch,
            "status": status_text or "clean",
            "ahead": ahead_str,
            "behind": behind_str,
        }
    except GitError as e:
        return {
            "repo": repo_wt.repo_name,
            "branch": "?",
            "status": f"error: {e}",
            "ahead": "-",
            "behind": "-",
        }


def workspace_status(workspace: Workspace) -> list[dict[str, str]]:
    """Get status of all repos in a workspace.

    Returns list of ``{repo, branch, status, ahead, behind}`` dicts.
    """
    items = [(wt.repo_name, wt) for wt in workspace.repos]
    results: list[dict[str, str]] = []
    for _name, result, exc in _parallel(_status_one_repo, items, "Checking"):
        if exc is not None:
            results.append(
                {
                    "repo": _name,
                    "branch": "?",
                    "status": f"error: {exc}",
                    "ahead": "-",
                    "behind": "-",
                }
            )
        else:
            results.append(result)
    return results
