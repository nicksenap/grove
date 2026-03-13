"""Tests for grove.claude — Claude Code memory sync."""

from __future__ import annotations

from pathlib import Path
from unittest.mock import patch

from grove import claude


class TestEncodePath:
    def test_simple_path(self):
        assert claude.encode_path(Path("/Users/nick/dev/grove")) == "-Users-nick-dev-grove"

    def test_dotfile_path(self):
        assert (
            claude.encode_path(Path("/Users/nick/.grove/workspaces/feat-foo"))
            == "-Users-nick--grove-workspaces-feat-foo"
        )

    def test_nested_dots(self):
        assert claude.encode_path(Path("/a/.b/.c/d")) == "-a--b--c-d"


class TestMemoryDirFor:
    def test_returns_memory_subdir(self, tmp_path):
        with patch.object(claude, "CLAUDE_PROJECTS_DIR", tmp_path / ".claude" / "projects"):
            result = claude.memory_dir_for(tmp_path / "my-repo")
            encoded = claude.encode_path((tmp_path / "my-repo").resolve())
            assert result == tmp_path / ".claude" / "projects" / encoded / "memory"


class TestRehydrateMemory:
    def test_copies_files_from_source_to_worktree(self, tmp_path):
        projects_dir = tmp_path / "projects"
        source = tmp_path / "source-repo"
        worktree = tmp_path / "worktree-repo"
        source.mkdir()
        worktree.mkdir()

        with patch.object(claude, "CLAUDE_PROJECTS_DIR", projects_dir):
            # Create source memory
            src_mem = claude.memory_dir_for(source)
            src_mem.mkdir(parents=True)
            (src_mem / "user_role.md").write_text("role: dev")
            (src_mem / "MEMORY.md").write_text("# Index\n- user_role.md")

            copied = claude.rehydrate_memory(source, worktree)

            assert copied == 2
            dst_mem = claude.memory_dir_for(worktree)
            assert (dst_mem / "user_role.md").read_text() == "role: dev"
            assert (dst_mem / "MEMORY.md").read_text() == "# Index\n- user_role.md"

    def test_skips_existing_files(self, tmp_path):
        projects_dir = tmp_path / "projects"
        source = tmp_path / "source"
        worktree = tmp_path / "worktree"
        source.mkdir()
        worktree.mkdir()

        with patch.object(claude, "CLAUDE_PROJECTS_DIR", projects_dir):
            src_mem = claude.memory_dir_for(source)
            src_mem.mkdir(parents=True)
            (src_mem / "old.md").write_text("old content")

            dst_mem = claude.memory_dir_for(worktree)
            dst_mem.mkdir(parents=True)
            (dst_mem / "old.md").write_text("already here")

            copied = claude.rehydrate_memory(source, worktree)

            assert copied == 0
            assert (dst_mem / "old.md").read_text() == "already here"

    def test_no_source_memory_returns_zero(self, tmp_path):
        projects_dir = tmp_path / "projects"
        with patch.object(claude, "CLAUDE_PROJECTS_DIR", projects_dir):
            assert claude.rehydrate_memory(tmp_path / "no-src", tmp_path / "wt") == 0


class TestHarvestMemory:
    def test_copies_new_files_back(self, tmp_path):
        projects_dir = tmp_path / "projects"
        source = tmp_path / "source"
        worktree = tmp_path / "worktree"
        source.mkdir()
        worktree.mkdir()

        with patch.object(claude, "CLAUDE_PROJECTS_DIR", projects_dir):
            wt_mem = claude.memory_dir_for(worktree)
            wt_mem.mkdir(parents=True)
            (wt_mem / "new_finding.md").write_text("found a bug")

            copied = claude.harvest_memory(worktree, source)

            assert copied == 1
            src_mem = claude.memory_dir_for(source)
            assert (src_mem / "new_finding.md").read_text() == "found a bug"

    def test_overwrites_older_files(self, tmp_path):
        import os
        import time

        projects_dir = tmp_path / "projects"
        source = tmp_path / "source"
        worktree = tmp_path / "worktree"
        source.mkdir()
        worktree.mkdir()

        with patch.object(claude, "CLAUDE_PROJECTS_DIR", projects_dir):
            src_mem = claude.memory_dir_for(source)
            src_mem.mkdir(parents=True)
            (src_mem / "shared.md").write_text("old version")
            # Set old mtime
            old_time = time.time() - 100
            os.utime(src_mem / "shared.md", (old_time, old_time))

            wt_mem = claude.memory_dir_for(worktree)
            wt_mem.mkdir(parents=True)
            (wt_mem / "shared.md").write_text("updated version")

            copied = claude.harvest_memory(worktree, source)

            assert copied == 1
            assert (src_mem / "shared.md").read_text() == "updated version"

    def test_does_not_overwrite_newer_files(self, tmp_path):
        import os
        import time

        projects_dir = tmp_path / "projects"
        source = tmp_path / "source"
        worktree = tmp_path / "worktree"
        source.mkdir()
        worktree.mkdir()

        with patch.object(claude, "CLAUDE_PROJECTS_DIR", projects_dir):
            # Source has newer file
            src_mem = claude.memory_dir_for(source)
            src_mem.mkdir(parents=True)
            (src_mem / "shared.md").write_text("newer in source")

            wt_mem = claude.memory_dir_for(worktree)
            wt_mem.mkdir(parents=True)
            (wt_mem / "shared.md").write_text("older in worktree")
            # Set old mtime on worktree version
            old_time = time.time() - 100
            os.utime(wt_mem / "shared.md", (old_time, old_time))

            copied = claude.harvest_memory(worktree, source)

            assert copied == 0
            assert (src_mem / "shared.md").read_text() == "newer in source"

    def test_no_worktree_memory_returns_zero(self, tmp_path):
        projects_dir = tmp_path / "projects"
        with patch.object(claude, "CLAUDE_PROJECTS_DIR", projects_dir):
            assert claude.harvest_memory(tmp_path / "wt", tmp_path / "src") == 0


class TestFindOrphanedMemoryDirs:
    def test_finds_orphaned_dirs(self, tmp_path):
        projects_dir = tmp_path / "projects"
        workspace_dir = tmp_path / "workspaces"
        workspace_dir.mkdir()

        # Create a workspace that still exists
        (workspace_dir / "active-ws" / "repo-a").mkdir(parents=True)

        # Create Claude memory dirs
        active_encoded = claude.encode_path((workspace_dir / "active-ws" / "repo-a").resolve())
        orphan_encoded = claude.encode_path((workspace_dir / "deleted-ws" / "repo-b").resolve())

        (projects_dir / active_encoded / "memory").mkdir(parents=True)
        (projects_dir / orphan_encoded / "memory").mkdir(parents=True)

        with patch.object(claude, "CLAUDE_PROJECTS_DIR", projects_dir):
            orphans = claude.find_orphaned_memory_dirs(workspace_dir)

        assert len(orphans) == 1
        assert orphans[0].name == orphan_encoded

    def test_no_claude_dir_returns_empty(self, tmp_path):
        with patch.object(claude, "CLAUDE_PROJECTS_DIR", tmp_path / "nonexistent"):
            assert claude.find_orphaned_memory_dirs(tmp_path) == []

    def test_ignores_non_workspace_dirs(self, tmp_path):
        projects_dir = tmp_path / "projects"
        workspace_dir = tmp_path / "workspaces"
        workspace_dir.mkdir()

        # Create a Claude dir that doesn't match workspace prefix
        (projects_dir / "-Users-nick-dev-other" / "memory").mkdir(parents=True)

        with patch.object(claude, "CLAUDE_PROJECTS_DIR", projects_dir):
            assert claude.find_orphaned_memory_dirs(workspace_dir) == []


class TestCleanupOrphanedMemoryDirs:
    def test_removes_dirs(self, tmp_path):
        dirs = []
        for i in range(3):
            d = tmp_path / f"orphan-{i}"
            d.mkdir()
            (d / "memory").mkdir()
            (d / "memory" / "file.md").write_text("data")
            dirs.append(d)

        removed = claude.cleanup_orphaned_memory_dirs(dirs)

        assert removed == 3
        for d in dirs:
            assert not d.exists()

    def test_handles_already_missing(self, tmp_path):
        removed = claude.cleanup_orphaned_memory_dirs([tmp_path / "nope"])
        assert removed == 0
