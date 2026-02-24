"""Core workspace orchestration: create, delete, status."""

from __future__ import annotations

import contextlib
import shutil
import subprocess
from pathlib import Path

from grove import git, state
from grove.console import console, error, info, success, warning
from grove.git import GitError
from grove.models import Config, RepoWorktree, Workspace


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

    # --- Fetch latest from all repos ---
    for repo_name, repo_path in repo_paths.items():
        with console.status(f"Fetching [bold]{repo_name}[/]…"):
            try:
                git.fetch(repo_path)
            except GitError as e:
                warning(f"Could not fetch {repo_name}: {e}")

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

    # --- Run per-repo setup hooks ---
    for repo_wt in created:
        _run_setup(repo_wt.repo_name, repo_wt.source_repo, repo_wt.worktree_path)

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

    for repo_wt in workspace.repos:
        with console.status(f"Removing worktree for [bold]{repo_wt.repo_name}[/]..."):
            try:
                git.worktree_remove(repo_wt.source_repo, repo_wt.worktree_path)
                success(f"Removed worktree: {repo_wt.repo_name}")
            except GitError as e:
                warning(f"Could not remove worktree for {repo_wt.repo_name}: {e}")
                # Try manual cleanup if worktree path exists
                if repo_wt.worktree_path.exists():
                    shutil.rmtree(repo_wt.worktree_path, ignore_errors=True)

    if workspace.path.exists():
        shutil.rmtree(workspace.path, ignore_errors=True)

    state.remove_workspace(name)
    return True


def sync_workspace(workspace: Workspace) -> list[dict[str, str]]:
    """Sync all repos by rebasing onto their base branches.

    Returns list of ``{repo, base, result}`` dicts.
    """
    results: list[dict[str, str]] = []
    for repo_wt in workspace.repos:
        # Fetch latest
        with console.status(f"Fetching [bold]{repo_wt.repo_name}[/]…"):
            try:
                git.fetch(repo_wt.worktree_path)
            except GitError as e:
                warning(f"Could not fetch {repo_wt.repo_name}: {e}")

        # Determine base branch
        base = git.repo_base_branch(repo_wt.source_repo)
        if base is None:
            try:
                base = git.default_branch(repo_wt.source_repo)
            except GitError:
                results.append(
                    {
                        "repo": repo_wt.repo_name,
                        "base": "?",
                        "result": "error: could not determine base branch",
                    }
                )
                continue

        # Skip dirty worktrees — rebase on uncommitted changes is dangerous
        status_text = git.repo_status(repo_wt.worktree_path)
        if status_text:
            results.append(
                {
                    "repo": repo_wt.repo_name,
                    "base": base,
                    "result": "skipped: uncommitted changes",
                }
            )
            continue

        # Check if rebase is needed
        try:
            _ahead, behind = git.commits_ahead_behind(repo_wt.worktree_path, base)
        except GitError:
            behind = -1  # unknown, attempt rebase anyway

        if behind == 0:
            results.append(
                {
                    "repo": repo_wt.repo_name,
                    "base": base,
                    "result": "up to date",
                }
            )
            continue

        # Rebase
        with console.status(f"Rebasing [bold]{repo_wt.repo_name}[/] onto {base}…"):
            try:
                git.rebase_onto(repo_wt.worktree_path, base)
                msg = f"rebased ({behind} new commits)" if behind > 0 else "rebased"
                results.append(
                    {
                        "repo": repo_wt.repo_name,
                        "base": base,
                        "result": msg,
                    }
                )
            except GitError:
                # Abort failed rebase to leave worktree in a clean state
                with contextlib.suppress(GitError):
                    git.rebase_abort(repo_wt.worktree_path)
                results.append(
                    {
                        "repo": repo_wt.repo_name,
                        "base": base,
                        "result": "conflict",
                    }
                )

    return results


def workspace_status(workspace: Workspace) -> list[dict[str, str]]:
    """Get status of all repos in a workspace.

    Returns list of ``{repo, branch, status, ahead, behind}`` dicts.
    """
    results: list[dict[str, str]] = []
    for repo_wt in workspace.repos:
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

            results.append(
                {
                    "repo": repo_wt.repo_name,
                    "branch": branch,
                    "status": status_text or "clean",
                    "ahead": ahead_str,
                    "behind": behind_str,
                }
            )
        except GitError as e:
            results.append(
                {
                    "repo": repo_wt.repo_name,
                    "branch": "?",
                    "status": f"error: {e}",
                    "ahead": "-",
                    "behind": "-",
                }
            )
    return results
