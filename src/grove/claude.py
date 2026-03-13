"""Claude Code memory sync for worktrees.

Claude Code stores per-project memory at ``~/.claude/projects/<encoded-path>/memory/``.
Since worktrees live at different paths than source repos, each gets its own
isolated memory directory. This module syncs memory between source repos and
their worktrees so context isn't lost when worktrees are created or deleted.
"""

from __future__ import annotations

import shutil
from pathlib import Path

CLAUDE_PROJECTS_DIR = Path.home() / ".claude" / "projects"


def encode_path(path: Path) -> str:
    """Encode an absolute path the way Claude Code does.

    Replaces ``/`` and ``.`` with ``-`` so the result is a flat directory name.

    >>> encode_path(Path("/Users/nick/.grove/workspaces/feat-foo"))
    '-Users-nick--grove-workspaces-feat-foo'
    """
    return str(path).replace("/", "-").replace(".", "-")


def memory_dir_for(path: Path) -> Path:
    """Return the Claude Code memory directory for a given project path."""
    return CLAUDE_PROJECTS_DIR / encode_path(path.resolve()) / "memory"


def rehydrate_memory(source_repo: Path, worktree_path: Path) -> int:
    """Copy memory files from *source_repo*'s Claude dir into *worktree_path*'s.

    Creates the target memory directory if it doesn't exist.
    Only copies files that don't already exist in the target (fresh worktree).

    Returns the number of files copied.
    """
    src = memory_dir_for(source_repo)
    if not src.is_dir():
        return 0

    dst = memory_dir_for(worktree_path)
    dst.mkdir(parents=True, exist_ok=True)

    copied = 0
    for src_file in src.iterdir():
        if not src_file.is_file():
            continue
        dst_file = dst / src_file.name
        if not dst_file.exists():
            shutil.copy2(src_file, dst_file)
            copied += 1
    return copied


def harvest_memory(worktree_path: Path, source_repo: Path) -> int:
    """Merge memory files from *worktree_path*'s Claude dir back into *source_repo*'s.

    Copies files that are new or newer (by mtime) than what exists in the
    source repo's memory directory.

    Returns the number of files copied/updated.
    """
    src = memory_dir_for(worktree_path)
    if not src.is_dir():
        return 0

    dst = memory_dir_for(source_repo)
    dst.mkdir(parents=True, exist_ok=True)

    copied = 0
    for src_file in src.iterdir():
        if not src_file.is_file():
            continue
        dst_file = dst / src_file.name
        if not dst_file.exists() or src_file.stat().st_mtime > dst_file.stat().st_mtime:
            shutil.copy2(src_file, dst_file)
            copied += 1
    return copied


def migrate_memory_dir(old_path: Path, new_path: Path) -> bool:
    """Rename a Claude memory project dir from *old_path*'s encoding to *new_path*'s.

    Used when a worktree is moved (e.g. workspace rename) so the memory
    follows the new location. Returns True if a migration was performed.
    """
    old_dir = memory_dir_for(old_path).parent  # the project dir, not memory/
    new_dir = memory_dir_for(new_path).parent
    if not old_dir.is_dir():
        return False
    if new_dir.exists():
        # Target already exists — merge instead of clobbering
        old_mem = old_dir / "memory"
        new_mem = new_dir / "memory"
        if old_mem.is_dir():
            new_mem.mkdir(parents=True, exist_ok=True)
            for f in old_mem.iterdir():
                if f.is_file():
                    shutil.copy2(f, new_mem / f.name)
            shutil.rmtree(old_dir, ignore_errors=True)
        return True
    new_dir.parent.mkdir(parents=True, exist_ok=True)
    old_dir.rename(new_dir)
    return True


def find_orphaned_memory_dirs(active_worktree_paths: list[Path]) -> list[Path]:
    """Find Claude memory dirs whose worktrees no longer exist on disk.

    Takes the list of *all known worktree paths* (from workspace state) and
    checks which ones have a Claude memory directory but no longer exist on
    disk.  This is precise — it only looks at paths Grove created, avoiding
    false positives from unrelated Claude projects.

    Returns a list of orphaned Claude project directories (the parent of ``memory/``).
    """
    if not CLAUDE_PROJECTS_DIR.is_dir():
        return []

    orphaned: list[Path] = []
    for wt_path in active_worktree_paths:
        project_dir = memory_dir_for(wt_path).parent
        if not project_dir.is_dir():
            continue
        if not (project_dir / "memory").is_dir():
            continue
        # The worktree is gone but the Claude memory dir remains
        if not wt_path.exists():
            orphaned.append(project_dir)

    return orphaned


def cleanup_orphaned_memory_dirs(dirs: list[Path]) -> int:
    """Remove orphaned Claude project directories.

    Returns the number of directories removed.
    """
    removed = 0
    for d in dirs:
        if d.is_dir():
            shutil.rmtree(d, ignore_errors=True)
            if not d.exists():
                removed += 1
    return removed
