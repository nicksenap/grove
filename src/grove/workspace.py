"""Core workspace orchestration: create, delete, status."""

from __future__ import annotations

import contextlib
import shutil
import subprocess
from collections.abc import Callable
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from grove import claude, config, git, state
from grove.console import console, error, info, success, warning
from grove.git import GitError
from grove.log import get_logger
from grove.models import Config, RepoWorktree, Workspace

_log = get_logger(__name__)


def _parallel(
    fn: Callable[[str, Any], Any],
    items: list[tuple[str, Any]],
    label: str = "Processing",
    *,
    spinner: bool = True,
) -> list[tuple[str, Any, Exception | None]]:
    """Run *fn(name, value)* for each ``(name, value)`` in *items* using threads.

    Returns a list of ``(name, result, error)`` tuples, one per item,
    preserving the original order of *items*.

    When *spinner* is False, a plain info message is shown instead of a
    persistent Rich spinner — useful when the tasks produce their own output.
    """
    if not items:
        return []

    order = {name: idx for idx, (name, _) in enumerate(items)}
    if len(order) != len(items):
        raise ValueError(f"Duplicate names in parallel items: {[n for n, _ in items]}")
    results: list[tuple[str, Any, Exception | None]] = [("", None, None)] * len(items)

    ctx = console.status(f"{label} {len(items)} repos…") if spinner else contextlib.nullcontext()
    if not spinner:
        info(f"{label} {len(items)} repos…")

    with ctx, ThreadPoolExecutor() as pool:
        futures = {pool.submit(fn, name, val): name for name, val in items}
        for future in as_completed(futures):
            name = futures[future]
            try:
                results[order[name]] = (name, future.result(), None)
            except Exception as exc:
                results[order[name]] = (name, None, exc)

    return results


def _provision_worktrees(
    repo_paths: dict[str, Path],
    branch: str,
    workspace_path: Path,
    *,
    remove_workspace_dir_on_rollback: bool = True,
) -> list[RepoWorktree] | None:
    """Validate, fetch, create branches/worktrees, run setup hooks.

    Returns the created RepoWorktree list, or None on failure (with rollback).
    This is the shared primitive used by both create and add-repo.
    """
    # Check for duplicate branch usage (git worktree limitation)
    for repo_name, repo_path in repo_paths.items():
        if git.worktree_has_branch(repo_path, branch):
            error(
                f"Branch [bold]{branch}[/] already has a worktree in "
                f"[bold]{repo_name}[/] — git only allows one worktree per branch"
            )
            return None

    # Fetch latest from all repos (parallel)
    def _fetch_one(_name: str, path: Path) -> None:
        git.fetch(path)

    for repo_name, _result, exc in _parallel(_fetch_one, list(repo_paths.items()), "Fetching"):
        if exc is not None:
            warning(f"Could not fetch {repo_name}: {exc}")

    # Create worktrees with rollback
    created: list[RepoWorktree] = []
    for repo_name, repo_path in repo_paths.items():
        worktree_path = workspace_path / repo_name

        # Auto-create branch from the default branch if it doesn't exist
        if not git.branch_exists(repo_path, branch):
            base = git.resolve_base_branch(repo_path)
            info(
                f"Creating branch [bold]{branch}[/] in {repo_name}"
                + (f" from {base}" if base else "")
            )
            try:
                git.create_branch(repo_path, branch, start_point=base)
            except GitError as e:
                error(f"Failed to create branch in {repo_name}: {e}")
                _rollback(
                    created,
                    workspace_path,
                    remove_workspace_dir=remove_workspace_dir_on_rollback,
                )
                return None

        with console.status(f"Creating worktree for [bold]{repo_name}[/]..."):
            try:
                git.worktree_add(repo_path, worktree_path, branch)
            except GitError as e:
                error(f"Failed to create worktree for {repo_name}: {e}")
                _rollback(
                    created,
                    workspace_path,
                    remove_workspace_dir=remove_workspace_dir_on_rollback,
                )
                return None

        repo_wt = RepoWorktree(
            repo_name=repo_name,
            source_repo=repo_path,
            worktree_path=worktree_path,
            branch=branch,
        )
        created.append(repo_wt)
        success(f"{repo_name} -> {worktree_path}")

    # Run per-repo setup hooks (parallel)
    def _setup_one(_name: str, repo_wt: RepoWorktree) -> None:
        _run_hook(repo_wt.repo_name, repo_wt.source_repo, repo_wt.worktree_path, "setup")

    setup_items = [(wt.repo_name, wt) for wt in created]
    _parallel(_setup_one, setup_items, "Running setup for", spinner=False)

    return created


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

    if state.get_workspace(name) is not None:
        error(f"Workspace [bold]{name}[/] already exists")
        return None

    workspace_path.mkdir(parents=True, exist_ok=True)

    _log.info("creating workspace %r (branch=%s, repos=%s)", name, branch, list(repo_paths.keys()))
    created = _provision_worktrees(repo_paths, branch, workspace_path)
    if created is None:
        _log.error("workspace creation failed for %r — rolled back", name)
        return None

    workspace = Workspace(
        name=name,
        path=workspace_path,
        branch=branch,
        repos=created,
    )
    state.add_workspace(workspace)

    if config.claude_memory_sync:
        _rehydrate_claude_memory(created)

    _log.info("workspace %r created at %s", name, workspace_path)
    return workspace


def _rehydrate_claude_memory(repos: list[RepoWorktree]) -> None:
    """Copy Claude Code memory from source repos into new worktrees."""
    for repo_wt in repos:
        try:
            n = claude.rehydrate_memory(repo_wt.source_repo, repo_wt.worktree_path)
            if n:
                _log.info("rehydrated %d Claude memory file(s) for %s", n, repo_wt.repo_name)
                info(f"[{repo_wt.repo_name}] rehydrated {n} Claude memory file(s)")
        except Exception as exc:
            _log.warning("Claude memory rehydrate failed for %s: %s", repo_wt.repo_name, exc)
            warning(f"[{repo_wt.repo_name}] Claude memory rehydrate failed: {exc}")


def _harvest_claude_memory(repos: list[RepoWorktree]) -> None:
    """Copy Claude Code memory from worktrees back into source repos."""
    for repo_wt in repos:
        try:
            n = claude.harvest_memory(repo_wt.worktree_path, repo_wt.source_repo)
            if n:
                _log.info("harvested %d Claude memory file(s) for %s", n, repo_wt.repo_name)
                info(f"[{repo_wt.repo_name}] harvested {n} Claude memory file(s)")
        except Exception as exc:
            _log.warning("Claude memory harvest failed for %s: %s", repo_wt.repo_name, exc)
            warning(f"[{repo_wt.repo_name}] Claude memory harvest failed: {exc}")


def _run_hook(repo_name: str, source_repo: Path, worktree_path: Path, hook: str) -> None:
    """Run hook commands from ``.grove.toml`` (read from source repo, run in worktree)."""
    commands = git.repo_hook_commands(source_repo, hook)
    if not commands:
        return

    for cmd in commands:
        info(f"[{repo_name}] {hook}: {cmd}")
        try:
            subprocess.run(
                cmd,
                cwd=worktree_path,
                shell=True,
                check=True,
                stdin=subprocess.DEVNULL,
            )
        except subprocess.CalledProcessError as e:
            warning(f"[{repo_name}] {hook} command failed (exit {e.returncode}): {cmd}")


def _rollback(
    created: list[RepoWorktree],
    workspace_path: Path,
    *,
    remove_workspace_dir: bool = True,
) -> None:
    """Remove already-created worktrees on failure."""
    if not created:
        return
    warning("Rolling back created worktrees...")
    for repo_wt in created:
        with contextlib.suppress(GitError):
            git.worktree_remove(repo_wt.source_repo, repo_wt.worktree_path)
    if remove_workspace_dir and workspace_path.exists():
        shutil.rmtree(workspace_path, ignore_errors=True)


def _teardown_and_remove(
    _name: str,
    repo_wt: RepoWorktree,
    *,
    force_cleanup: bool = False,
) -> None:
    """Run teardown hook then remove worktree. The shared removal primitive.

    *force_cleanup*: if True, fall back to ``shutil.rmtree`` when git fails.
    When force cleanup succeeds, the error is swallowed (directory is gone).
    """
    if repo_wt.worktree_path.exists():
        with contextlib.suppress(Exception):
            _run_hook(repo_wt.repo_name, repo_wt.source_repo, repo_wt.worktree_path, "teardown")
    try:
        git.worktree_remove(repo_wt.source_repo, repo_wt.worktree_path)
    except GitError:
        if force_cleanup and repo_wt.worktree_path.exists():
            shutil.rmtree(repo_wt.worktree_path, ignore_errors=True)
            if not repo_wt.worktree_path.exists():
                return  # directory cleaned up — don't propagate the error
        raise  # re-raise so _parallel records the error


def delete_workspace(name: str) -> bool:
    """Delete a workspace: remove worktrees, folder, and state entry.

    Only removes the workspace from state when all worktree removals succeed
    (or the workspace directory is gone). On partial failure the state entry is
    kept so ``gw doctor`` can help clean up.
    """
    workspace = state.get_workspace(name)
    if workspace is None:
        error(f"Workspace [bold]{name}[/] not found")
        return False

    _log.info("deleting workspace %r", name)

    # Harvest Claude memory before removing worktrees
    cfg = config.load_config()
    if cfg and cfg.claude_memory_sync:
        _harvest_claude_memory(workspace.repos)

    def _remove_one(_name: str, repo_wt: RepoWorktree) -> None:
        _teardown_and_remove(_name, repo_wt, force_cleanup=True)

    items = [(wt.repo_name, wt) for wt in workspace.repos]
    failures = 0
    for repo_name, _result, exc in _parallel(_remove_one, items, "Removing"):
        if exc is not None:
            warning(f"Could not remove worktree for {repo_name}: {exc}")
            failures += 1
        else:
            success(f"Removed worktree: {repo_name}")

    if workspace.path.exists():
        shutil.rmtree(workspace.path, ignore_errors=True)

    if failures and workspace.path.exists():
        _log.warning("workspace %r: %d worktree(s) failed to remove", name, failures)
        warning(
            f"{failures} worktree(s) could not be removed. "
            "Run [bold]gw doctor --fix[/] to clean up."
        )
        return False

    state.remove_workspace(name)
    _log.info("workspace %r deleted", name)
    return True


def _sync_one_repo(_name: str, repo_wt: RepoWorktree) -> dict[str, str]:
    """Sync a single repo: fetch, determine base, rebase if needed."""
    # Fetch latest (continue with cached refs on failure)
    with contextlib.suppress(GitError):
        git.fetch(repo_wt.worktree_path)

    # Determine base branch
    base = git.resolve_base_branch(repo_wt.source_repo)
    if base is None:
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

    # Pre-sync hook (best-effort)
    with contextlib.suppress(Exception):
        _run_hook(repo_wt.repo_name, repo_wt.source_repo, repo_wt.worktree_path, "pre_sync")

    # Rebase
    try:
        git.rebase_onto(repo_wt.worktree_path, base)
        msg = f"rebased ({behind} new commits)" if behind > 0 else "rebased"
        # Post-sync hook (best-effort, only on success)
        with contextlib.suppress(Exception):
            _run_hook(repo_wt.repo_name, repo_wt.source_repo, repo_wt.worktree_path, "post_sync")
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
    _log.info("syncing workspace %r", workspace.name)
    items = [(wt.repo_name, wt) for wt in workspace.repos]
    results: list[dict[str, str]] = []
    for _name, result, exc in _parallel(_sync_one_repo, items, "Syncing"):
        if exc is not None:
            results.append({"repo": _name, "base": "?", "result": f"error: {exc}"})
        else:
            results.append(result)
    _log.info("sync results: %s", {r["repo"]: r["result"] for r in results})
    return results


def get_runnable(ws: Workspace) -> list[tuple[RepoWorktree, list[str]]]:
    """Return repos that have a ``run`` hook, with their command lists."""
    return [
        (wt, cmds) for wt in ws.repos if (cmds := git.repo_hook_commands(wt.source_repo, "run"))
    ]


def run_pre_hooks(runnable: list[tuple[RepoWorktree, list[str]]]) -> None:
    """Execute ``pre_run`` hooks in parallel (best-effort)."""

    def _pre_run(_name: str, repo_wt: RepoWorktree) -> None:
        _run_hook(repo_wt.repo_name, repo_wt.source_repo, repo_wt.worktree_path, "pre_run")

    _parallel(_pre_run, [(wt.repo_name, wt) for wt, _ in runnable], "Pre-run", spinner=False)


def run_post_hooks(runnable: list[tuple[RepoWorktree, list[str]]]) -> None:
    """Execute ``post_run`` hooks in parallel (best-effort)."""

    def _post_run(_name: str, repo_wt: RepoWorktree) -> None:
        _run_hook(repo_wt.repo_name, repo_wt.source_repo, repo_wt.worktree_path, "post_run")

    _parallel(_post_run, [(wt.repo_name, wt) for wt, _ in runnable], "Post-run", spinner=False)


def run_workspace(ws: Workspace) -> int:
    """Run ``run`` hooks across all repos as foreground processes.

    Runs ``pre_run`` first, then starts ``run`` processes in parallel.
    On exit (Ctrl+C or natural), runs ``post_run`` hooks for cleanup.
    Returns the number of repos that had a ``run`` hook.
    """
    runnable = get_runnable(ws)
    if not runnable:
        return 0

    run_pre_hooks(runnable)

    # Start all run processes
    processes: list[tuple[str, subprocess.Popen]] = []
    for repo_wt, commands in runnable:
        # Join multiple commands with && so they run as one shell
        cmd = " && ".join(commands)
        info(f"[{repo_wt.repo_name}] run: {cmd}")
        proc = subprocess.Popen(
            cmd,
            cwd=repo_wt.worktree_path,
            shell=True,
        )
        processes.append((repo_wt.repo_name, proc))

    # Wait for all processes (Ctrl+C triggers KeyboardInterrupt)
    try:
        for _name, proc in processes:
            proc.wait()
    except KeyboardInterrupt:
        info("Stopping…")
        for _name, proc in processes:
            with contextlib.suppress(BaseException):
                proc.terminate()
        for _name, proc in processes:
            with contextlib.suppress(BaseException):
                proc.wait(timeout=5)

    run_post_hooks(runnable)

    return len(runnable)


def _status_one_repo(_name: str, repo_wt: RepoWorktree) -> dict[str, str]:
    """Get status for a single repo."""
    try:
        branch = git.current_branch(repo_wt.worktree_path)
        status_text = git.repo_status(repo_wt.worktree_path)

        # Ahead/behind (best-effort — never crash status for this)
        ahead_str = "-"
        behind_str = "-"
        try:
            base = git.resolve_base_branch(repo_wt.source_repo)
            if base is not None:
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


# ---------------------------------------------------------------------------
# Status --all
# ---------------------------------------------------------------------------


def all_workspaces_summary() -> list[dict[str, str]]:
    """Return a summary row for every workspace.

    Each dict contains ``{name, branch, repos, status, path}``.
    """
    workspaces = state.load_workspaces()
    if not workspaces:
        return []

    def _summarise(_name: str, ws: Workspace) -> dict[str, str]:
        statuses = workspace_status(ws)
        clean = sum(1 for s in statuses if s["status"] == "clean")
        modified = sum(
            1
            for s in statuses
            if s["status"] not in ("clean", "") and not s["status"].startswith("error:")
        )
        errored = sum(1 for s in statuses if s["status"].startswith("error:"))

        parts: list[str] = []
        if clean:
            parts.append(f"{clean} clean")
        if modified:
            parts.append(f"{modified} modified")
        if errored:
            parts.append(f"{errored} error")
        summary = ", ".join(parts) if parts else "empty"

        return {
            "name": ws.name,
            "branch": ws.branch,
            "repos": str(len(ws.repos)),
            "status": summary,
            "path": str(ws.path),
        }

    items = [(ws.name, ws) for ws in workspaces]
    results: list[dict[str, str]] = []
    for _name, result, exc in _parallel(_summarise, items, "Checking"):
        if exc is not None:
            ws_match = next((ws for ws in workspaces if ws.name == _name), None)
            results.append(
                {
                    "name": _name,
                    "branch": ws_match.branch if ws_match else "?",
                    "repos": str(len(ws_match.repos)) if ws_match else "?",
                    "status": f"error: {exc}",
                    "path": str(ws_match.path) if ws_match else "?",
                }
            )
        else:
            results.append(result)
    return results


# ---------------------------------------------------------------------------
# Add / remove repos
# ---------------------------------------------------------------------------


def add_repo_to_workspace(
    ws: Workspace,
    repo_paths: dict[str, Path],
    config: Config,
) -> list[RepoWorktree] | None:
    """Add new repos to an existing workspace.

    Returns the newly created RepoWorktree entries, or None on failure.
    """
    existing_names = {r.repo_name for r in ws.repos}

    # Filter out already-present repos
    to_add = {n: p for n, p in repo_paths.items() if n not in existing_names}
    if not to_add:
        warning("All selected repos are already in the workspace")
        return None

    created = _provision_worktrees(
        to_add, ws.branch, ws.path, remove_workspace_dir_on_rollback=False
    )
    if created is None:
        return None

    ws.repos.extend(created)
    state.update_workspace(ws)

    if config.claude_memory_sync:
        _rehydrate_claude_memory(created)

    return created


def remove_repo_from_workspace(
    ws: Workspace,
    repo_names: list[str],
    *,
    force: bool = False,
) -> bool:
    """Remove repos from an existing workspace.

    Returns True if all removals succeeded, False if any failed.
    """
    existing = {r.repo_name: r for r in ws.repos}
    to_remove: list[RepoWorktree] = []
    for name in repo_names:
        if name not in existing:
            warning(f"{name} is not in workspace [bold]{ws.name}[/], skipping")
            continue
        to_remove.append(existing[name])

    if not to_remove:
        warning("No repos to remove")
        return True

    # Harvest Claude memory before removing worktrees
    cfg = config.load_config()
    if cfg and cfg.claude_memory_sync:
        _harvest_claude_memory(to_remove)

    items = [(wt.repo_name, wt) for wt in to_remove]
    removed_names: set[str] = set()
    for name, _result, exc in _parallel(_teardown_and_remove, items, "Removing"):
        if exc is not None:
            warning(f"Could not remove worktree for {name}: {exc}")
        else:
            removed_names.add(name)
            success(f"Removed repo: {name}")

    # Update state for successful removals
    if removed_names:
        ws.repos = [r for r in ws.repos if r.repo_name not in removed_names]
        state.update_workspace(ws)

    return len(removed_names) == len(to_remove)


# ---------------------------------------------------------------------------
# Rename
# ---------------------------------------------------------------------------


def rename_workspace(old_name: str, new_name: str, config: Config) -> bool:
    """Rename a workspace: directory, internal paths, state entry, and git linkage.

    Updates state first, then renames the directory.  If the directory rename
    fails the state change is rolled back.
    """
    ws = state.get_workspace(old_name)
    if ws is None:
        error(f"Workspace [bold]{old_name}[/] not found")
        return False

    if state.get_workspace(new_name) is not None:
        error(f"Workspace [bold]{new_name}[/] already exists")
        return False

    old_path = ws.path
    new_path = config.workspace_dir / new_name

    if new_path.exists():
        error(f"Directory already exists: {new_path}")
        return False

    # Save original values for rollback
    orig_name = ws.name
    orig_path = ws.path
    orig_wt_paths = [(r, r.worktree_path) for r in ws.repos]

    # Update state first (easier to rollback than a directory rename)
    ws.name = new_name
    ws.path = new_path
    for repo_wt in ws.repos:
        repo_wt.worktree_path = new_path / repo_wt.repo_name
    state.update_workspace(ws, match_name=old_name)

    # Rename directory
    try:
        old_path.rename(new_path)
    except OSError as e:
        # Rollback state to original values
        ws.name = orig_name
        ws.path = orig_path
        for repo_wt, orig_wt_path in orig_wt_paths:
            repo_wt.worktree_path = orig_wt_path
        state.update_workspace(ws, match_name=new_name)
        error(f"Failed to rename directory: {e}")
        return False

    # Repair git worktree linkage (best-effort per repo)
    for repo_wt in ws.repos:
        with contextlib.suppress(Exception):
            git.worktree_repair(repo_wt.source_repo, repo_wt.worktree_path)

    # Migrate Claude Code memory directories to the new paths (best-effort)
    if config.claude_memory_sync:
        for repo_wt, orig_wt_path in orig_wt_paths:
            try:
                claude.migrate_memory_dir(orig_wt_path, repo_wt.worktree_path)
            except Exception as exc:
                warning(f"[{repo_wt.repo_name}] Claude memory migration failed: {exc}")

    return True


# ---------------------------------------------------------------------------
# Doctor
# ---------------------------------------------------------------------------


@dataclass
class DoctorIssue:
    """A single issue found by the doctor command."""

    workspace_name: str
    repo_name: str | None
    issue: str
    suggested_action: str


def diagnose_workspaces(config: Config) -> list[DoctorIssue]:
    """Check all workspaces for health issues."""
    issues: list[DoctorIssue] = []
    workspaces = state.load_workspaces()

    # Check for orphaned Claude Code memory directories.
    # Only checks worktree paths known to state — if a workspace was fully
    # deleted, its memory was already harvested during delete_workspace.
    if config.claude_memory_sync:
        all_wt_paths = [repo_wt.worktree_path for ws in workspaces for repo_wt in ws.repos]
        orphaned = claude.find_orphaned_memory_dirs(all_wt_paths)
        for orphan_dir in orphaned:
            issues.append(
                DoctorIssue(
                    workspace_name="[claude]",
                    repo_name=orphan_dir.name,
                    issue="orphaned Claude memory directory",
                    suggested_action="remove orphaned Claude memory",
                )
            )

    for ws in workspaces:
        # Check: workspace directory exists
        if not ws.path.exists():
            issues.append(
                DoctorIssue(
                    workspace_name=ws.name,
                    repo_name=None,
                    issue="workspace directory missing",
                    suggested_action="remove stale state entry",
                )
            )
            continue  # no point checking repos if dir is gone

        for repo_wt in ws.repos:
            # Check: source repo exists
            if not repo_wt.source_repo.exists():
                issues.append(
                    DoctorIssue(
                        workspace_name=ws.name,
                        repo_name=repo_wt.repo_name,
                        issue="source repo missing",
                        suggested_action="remove stale repo entry",
                    )
                )
                continue

            # Check: worktree directory exists
            if not repo_wt.worktree_path.exists():
                issues.append(
                    DoctorIssue(
                        workspace_name=ws.name,
                        repo_name=repo_wt.repo_name,
                        issue="worktree directory missing",
                        suggested_action="remove stale repo entry",
                    )
                )
                continue

            # Check: git worktree is registered
            try:
                wt_list = git.worktree_list(repo_wt.source_repo)
                registered = any(
                    Path(wt["path"]).resolve() == repo_wt.worktree_path.resolve() for wt in wt_list
                )
                if not registered:
                    issues.append(
                        DoctorIssue(
                            workspace_name=ws.name,
                            repo_name=repo_wt.repo_name,
                            issue="worktree not registered in git",
                            suggested_action="re-register with git worktree repair",
                        )
                    )
            except GitError:
                issues.append(
                    DoctorIssue(
                        workspace_name=ws.name,
                        repo_name=repo_wt.repo_name,
                        issue="git error checking worktree registration",
                        suggested_action="check repo manually",
                    )
                )

    return issues


def fix_workspace_issues(issues: list[DoctorIssue]) -> int:
    """Auto-fix stale state entries. Returns the number of fixes applied."""
    fixed = 0

    # Group workspace-level removals
    ws_to_remove: set[str] = set()
    # Group repo-level removals: workspace_name -> set of repo_names
    repos_to_remove: dict[str, set[str]] = {}

    # Collect orphaned Claude memory dirs for cleanup
    claude_orphans: list[Path] = []

    for issue in issues:
        if issue.repo_name is None and "remove stale state" in issue.suggested_action:
            ws_to_remove.add(issue.workspace_name)
        elif issue.repo_name is not None and "remove stale repo" in issue.suggested_action:
            repos_to_remove.setdefault(issue.workspace_name, set()).add(issue.repo_name)
        elif "orphaned Claude memory" in issue.suggested_action and issue.repo_name:
            orphan_path = claude.CLAUDE_PROJECTS_DIR / issue.repo_name
            if orphan_path.is_dir():
                claude_orphans.append(orphan_path)

    # Remove stale workspaces
    for ws_name in ws_to_remove:
        state.remove_workspace(ws_name)
        fixed += 1

    # Remove stale repos from workspaces
    for ws_name, repo_names in repos_to_remove.items():
        if ws_name in ws_to_remove:
            continue  # already removed entirely
        ws = state.get_workspace(ws_name)
        if ws is None:
            continue
        ws.repos = [r for r in ws.repos if r.repo_name not in repo_names]
        state.update_workspace(ws)
        fixed += len(repo_names)

    # Clean up orphaned Claude memory directories
    fixed += claude.cleanup_orphaned_memory_dirs(claude_orphans)

    return fixed
