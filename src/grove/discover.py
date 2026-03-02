"""Discover git repositories in a directory."""

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
        if entry.is_dir() and not entry.name.startswith(".") and (entry / ".git").exists():
            repos[entry.name] = entry
    return repos
