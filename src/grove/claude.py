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


def find_orphaned_memory_dirs(workspace_dir: Path) -> list[Path]:
    """Find Claude memory dirs that look like Grove worktrees but no longer exist.

    Builds a set of encoded paths for all *existing* workspace subdirectories,
    then flags any Claude project dir under the workspace prefix that isn't in
    that active set.  This avoids the lossy-decode problem entirely.

    Returns a list of orphaned Claude project directories (the parent of ``memory/``).
    """
    if not CLAUDE_PROJECTS_DIR.is_dir():
        return []

    prefix = encode_path(workspace_dir.resolve())

    # Build set of encoded paths for all existing workspace dirs and their children
    active_encoded: set[str] = set()
    if workspace_dir.is_dir():
        for ws_dir in workspace_dir.iterdir():
            if not ws_dir.is_dir():
                continue
            active_encoded.add(encode_path(ws_dir.resolve()))
            for child in ws_dir.iterdir():
                if child.is_dir():
                    active_encoded.add(encode_path(child.resolve()))

    orphaned: list[Path] = []
    for entry in CLAUDE_PROJECTS_DIR.iterdir():
        if not entry.is_dir():
            continue
        if not entry.name.startswith(prefix):
            continue
        # Must be a subdirectory of workspace_dir (not workspace_dir itself)
        if entry.name == prefix:
            continue
        # Must have a memory/ subdir (otherwise it's not a Claude memory project)
        if not (entry / "memory").is_dir():
            continue
        # If not in the active set, it's orphaned
        if entry.name not in active_encoded:
            orphaned.append(entry)

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
