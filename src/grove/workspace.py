"""Core workspace orchestration: create, delete, status."""

from __future__ import annotations

import contextlib
import shutil
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

    workspace = Workspace(
        name=name,
        path=workspace_path,
        branch=branch,
        repos=created,
    )
    state.add_workspace(workspace)
    return workspace


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


def workspace_status(workspace: Workspace) -> list[dict[str, str]]:
    """Get status of all repos in a workspace.

    Returns list of {repo, branch, status} dicts.
    """
    results: list[dict[str, str]] = []
    for repo_wt in workspace.repos:
        try:
            branch = git.current_branch(repo_wt.worktree_path)
            status_text = git.repo_status(repo_wt.worktree_path)
            results.append(
                {
                    "repo": repo_wt.repo_name,
                    "branch": branch,
                    "status": status_text or "clean",
                }
            )
        except GitError as e:
            results.append(
                {
                    "repo": repo_wt.repo_name,
                    "branch": "?",
                    "status": f"error: {e}",
                }
            )
    return results
