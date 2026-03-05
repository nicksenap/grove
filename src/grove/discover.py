"""Discover git repositories in directories."""

from __future__ import annotations

from pathlib import Path


def find_repos(repos_dir: Path) -> dict[str, Path]:
    """Find all git repos in the given directory (one level deep).

    Returns a dict mapping repo name -> repo path.
    Uses ``.git`` existence check instead of ``git rev-parse`` to avoid
    spawning a subprocess per directory.
    """
    repos: dict[str, Path] = {}
    if not repos_dir.is_dir():
        return repos
    for entry in sorted(repos_dir.iterdir()):
        if entry.is_dir() and not entry.name.startswith(".") and (entry / ".git").is_dir():
            repos[entry.name] = entry
    return repos


def find_all_repos(repo_dirs: list[Path]) -> dict[str, Path]:
    """Find all git repos across multiple directories (one level deep each).

    Returns a dict mapping repo name -> repo path. If the same repo name
    appears in multiple directories, the first occurrence wins.
    """
    repos: dict[str, Path] = {}
    for d in repo_dirs:
        for name, path in find_repos(d).items():
            if name not in repos:
                repos[name] = path
    return repos


def explore_repos(repo_dirs: list[Path], max_depth: int = 3) -> dict[Path, dict[str, Path]]:
    """Scan directories recursively for git repos, grouped by source dir.

    Returns ``{source_dir: {repo_name: repo_path}}``.
    Scans up to *max_depth* levels deep.  Stops descending into a directory
    once a ``.git`` is found (a repo's subdirs are not scanned).
    """
    result: dict[Path, dict[str, Path]] = {}
    for d in repo_dirs:
        found: dict[str, Path] = {}
        _scan_recursive(d, found, depth=0, max_depth=max_depth)
        if found:
            result[d] = found
    return result


def _scan_recursive(directory: Path, found: dict[str, Path], depth: int, max_depth: int) -> None:
    """Recursively scan *directory* for git repos up to *max_depth*."""
    if depth > max_depth or not directory.is_dir():
        return
    try:
        entries = sorted(directory.iterdir())
    except PermissionError:
        return
    for entry in entries:
        if not entry.is_dir() or entry.name.startswith("."):
            continue
        if (entry / ".git").is_dir():
            name = entry.name
            if name not in found:
                found[name] = entry
            # Don't descend into repos
        else:
            _scan_recursive(entry, found, depth + 1, max_depth)
