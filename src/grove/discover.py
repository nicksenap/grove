"""Discover git repositories in directories."""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path

from grove.git import parse_remote_name, remote_url


@dataclass(frozen=True)
class RepoInfo:
    """A discovered repository with its identity and location."""

    name: str  # folder name
    path: Path  # absolute path to repo
    remote: str | None  # origin remote URL
    display_name: str  # org/repo from remote, or folder name as fallback


def _repo_display_name(path: Path) -> str:
    """Derive a display name from the remote URL, falling back to folder name."""
    url = remote_url(path)
    if url:
        parsed = parse_remote_name(url)
        if parsed:
            return parsed
    return path.name


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


def discover_repos(repo_dirs: list[Path], max_depth: int = 3) -> list[RepoInfo]:
    """Deep-scan directories for git repos, deduped by remote URL.

    Returns a list of :class:`RepoInfo` sorted by display name.
    When multiple paths share the same remote URL, the one closest to a
    configured ``repo_dirs`` root wins (i.e. direct children are preferred
    over nested repos).
    """
    seen_remotes: dict[str, RepoInfo] = {}  # remote_url -> RepoInfo
    seen_paths: set[Path] = set()

    for d in repo_dirs:
        _discover_recursive(d, d, seen_remotes, seen_paths, depth=0, max_depth=max_depth)

    return sorted(seen_remotes.values(), key=lambda r: r.display_name)


def _discover_recursive(
    directory: Path,
    root: Path,
    seen_remotes: dict[str, RepoInfo],
    seen_paths: set[Path],
    depth: int,
    max_depth: int,
) -> None:
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
            resolved = entry.resolve()
            if resolved in seen_paths:
                continue
            seen_paths.add(resolved)

            url = remote_url(entry)
            display = _repo_display_name(entry)
            info = RepoInfo(
                name=entry.name,
                path=entry,
                remote=url,
                display_name=display,
            )

            if url and url in seen_remotes:
                # Prefer direct children over nested repos
                existing = seen_remotes[url]
                if existing.path.parent != root and entry.parent == root:
                    seen_remotes[url] = info
                # Otherwise keep existing (first wins)
            else:
                key = url or str(resolved)
                if key not in seen_remotes:
                    seen_remotes[key] = info
            # Don't descend into repos
        else:
            _discover_recursive(entry, root, seen_remotes, seen_paths, depth + 1, max_depth)


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
