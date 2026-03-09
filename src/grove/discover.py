"""Discover git repositories in directories."""

from __future__ import annotations

import json
import time
from concurrent.futures import ThreadPoolExecutor, as_completed
from dataclasses import dataclass
from pathlib import Path

from grove.git import parse_remote_name, remote_url

# ---------------------------------------------------------------------------
# Remote URL cache — avoids re-spawning `git remote get-url` on every run
# ---------------------------------------------------------------------------

_CACHE_DIR = Path.home() / ".grove" / "cache"
_REMOTE_CACHE_FILE = _CACHE_DIR / "remotes.json"
_CACHE_TTL_SECONDS = 86400  # 24 h — remote URLs rarely change


def _load_remote_cache() -> dict[str, dict]:
    """Load the on-disk remote URL cache."""
    try:
        return json.loads(_REMOTE_CACHE_FILE.read_text())
    except (FileNotFoundError, json.JSONDecodeError, OSError):
        return {}


def _save_remote_cache(cache: dict[str, dict]) -> None:
    """Persist the remote URL cache."""
    _CACHE_DIR.mkdir(parents=True, exist_ok=True)
    _REMOTE_CACHE_FILE.write_text(json.dumps(cache))


def _git_config_mtime(repo_path: Path) -> float:
    """Return mtime of .git/config (our invalidation key)."""
    config = repo_path / ".git" / "config"
    try:
        return config.stat().st_mtime
    except OSError:
        return 0.0


_CACHE_MISS = object()


def _resolve_remote_cached(
    repo_path: Path, cache: dict[str, dict], now: float
) -> str | None | object:
    """Look up remote URL from cache. Returns _CACHE_MISS on miss."""
    key = str(repo_path.resolve())
    entry = cache.get(key)
    if entry is None:
        return _CACHE_MISS
    if entry.get("mtime", 0) != _git_config_mtime(repo_path):
        return _CACHE_MISS
    if now - entry.get("ts", 0) > _CACHE_TTL_SECONDS:
        return _CACHE_MISS
    url = entry.get("url", "")
    return url or None  # "" → None


def _batch_resolve_remotes(
    repo_paths: list[Path],
) -> dict[Path, str | None]:
    """Resolve remote URLs for many repos, using cache + thread parallelism."""
    cache = _load_remote_cache()
    now = time.time()

    results: dict[Path, str | None] = {}
    to_fetch: list[Path] = []

    for p in repo_paths:
        cached = _resolve_remote_cached(p, cache, now)
        if cached is not _CACHE_MISS:
            results[p] = cached
        else:
            to_fetch.append(p)

    if to_fetch:
        # Parallel git subprocess calls — big speedup for cold cache
        with ThreadPoolExecutor(max_workers=16) as pool:
            futures = {pool.submit(remote_url, p): p for p in to_fetch}
            for future in as_completed(futures):
                p = futures[future]
                try:
                    url = future.result()
                except Exception:
                    url = None
                results[p] = url
                cache[str(p.resolve())] = {
                    "url": url or "",
                    "mtime": _git_config_mtime(p),
                    "ts": now,
                }

        _save_remote_cache(cache)

    return results


# ---------------------------------------------------------------------------
# Public types / helpers
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class RepoInfo:
    """A discovered repository with its identity and location."""

    name: str  # folder name
    path: Path  # absolute path to repo
    remote: str | None  # origin remote URL
    display_name: str  # org/repo from remote, or folder name as fallback


def _display_name_from_url(url: str | None, fallback: str) -> str:
    """Derive a display name from a remote URL, falling back to *fallback*."""
    if url:
        parsed = parse_remote_name(url)
        if parsed:
            return parsed
    return fallback


def _repo_display_name(path: Path) -> str:
    """Derive a display name from the remote URL, falling back to folder name."""
    url = remote_url(path)
    if url:
        parsed = parse_remote_name(url)
        if parsed:
            return parsed
    return path.name


# ---------------------------------------------------------------------------
# Repo discovery
# ---------------------------------------------------------------------------


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
    # Phase 1: fast filesystem scan (no subprocesses)
    candidates: list[tuple[Path, Path]] = []  # (entry, root)
    seen_paths: set[Path] = set()

    for d in repo_dirs:
        _collect_repo_paths(d, d, candidates, seen_paths, depth=0, max_depth=max_depth)

    if not candidates:
        return []

    # Phase 2: batch-resolve remote URLs (cached + parallel)
    repo_paths = [entry for entry, _ in candidates]
    url_map = _batch_resolve_remotes(repo_paths)

    # Phase 3: build RepoInfo list with dedup
    seen_remotes: dict[str, RepoInfo] = {}
    for entry, root in candidates:
        url = url_map.get(entry)
        display = _display_name_from_url(url, entry.name)
        info = RepoInfo(
            name=entry.name,
            path=entry,
            remote=url,
            display_name=display,
        )

        if url and url in seen_remotes:
            existing = seen_remotes[url]
            if existing.path.parent != root and entry.parent == root:
                seen_remotes[url] = info
        else:
            key = url or str(entry.resolve())
            if key not in seen_remotes:
                seen_remotes[key] = info

    return sorted(seen_remotes.values(), key=lambda r: r.display_name)


def _collect_repo_paths(
    directory: Path,
    root: Path,
    candidates: list[tuple[Path, Path]],
    seen_paths: set[Path],
    depth: int,
    max_depth: int,
) -> None:
    """Recursively collect repo paths without resolving remotes."""
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
            if resolved not in seen_paths:
                seen_paths.add(resolved)
                candidates.append((entry, root))
            # Don't descend into repos
        else:
            _collect_repo_paths(entry, root, candidates, seen_paths, depth + 1, max_depth)


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
