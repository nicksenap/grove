"""Tests for grove.discover — repo discovery across directories."""

from __future__ import annotations

import time
from pathlib import Path
from unittest.mock import patch

from grove.discover import (
    _batch_resolve_remotes,
    discover_repos,
    explore_repos,
    find_all_repos,
    find_repos,
)


def _make_repo(path: Path) -> None:
    """Create a fake repo at *path* (directory with .git subdirectory)."""
    path.mkdir(parents=True, exist_ok=True)
    (path / ".git").mkdir(exist_ok=True)


class TestFindRepos:
    def test_single_dir(self, tmp_path: Path):
        _make_repo(tmp_path / "repo-a")
        _make_repo(tmp_path / "repo-b")
        result = find_repos(tmp_path)
        assert set(result.keys()) == {"repo-a", "repo-b"}

    def test_skips_hidden_dirs(self, tmp_path: Path):
        _make_repo(tmp_path / ".hidden")
        _make_repo(tmp_path / "visible")
        result = find_repos(tmp_path)
        assert list(result.keys()) == ["visible"]

    def test_nonexistent_dir(self, tmp_path: Path):
        result = find_repos(tmp_path / "nope")
        assert result == {}


class TestFindAllRepos:
    def test_multiple_dirs(self, tmp_path: Path):
        dir1 = tmp_path / "dir1"
        dir2 = tmp_path / "dir2"
        _make_repo(dir1 / "repo-a")
        _make_repo(dir2 / "repo-b")
        result = find_all_repos([dir1, dir2])
        assert set(result.keys()) == {"repo-a", "repo-b"}

    def test_first_occurrence_wins(self, tmp_path: Path):
        dir1 = tmp_path / "dir1"
        dir2 = tmp_path / "dir2"
        _make_repo(dir1 / "same-name")
        _make_repo(dir2 / "same-name")
        result = find_all_repos([dir1, dir2])
        assert result["same-name"] == dir1 / "same-name"

    def test_empty_list(self):
        assert find_all_repos([]) == {}


class TestExploreRepos:
    def test_nested_repos(self, tmp_path: Path):
        _make_repo(tmp_path / "org" / "repo-deep")
        _make_repo(tmp_path / "repo-shallow")
        result = explore_repos([tmp_path])
        repos = result[tmp_path]
        assert "repo-deep" in repos
        assert "repo-shallow" in repos

    def test_respects_max_depth(self, tmp_path: Path):
        _make_repo(tmp_path / "a" / "b" / "c" / "d" / "too-deep")
        result = explore_repos([tmp_path], max_depth=2)
        repos = result.get(tmp_path, {})
        assert "too-deep" not in repos

    def test_grouped_by_source_dir(self, tmp_path: Path):
        dir1 = tmp_path / "dir1"
        dir2 = tmp_path / "dir2"
        _make_repo(dir1 / "repo-a")
        _make_repo(dir2 / "repo-b")
        result = explore_repos([dir1, dir2])
        assert dir1 in result
        assert dir2 in result
        assert "repo-a" in result[dir1]
        assert "repo-b" in result[dir2]


class TestDiscoverRepos:
    """Tests for discover_repos — deep scan with remote identity."""

    def _make_repo_with_remote(self, path: Path, url: str) -> None:
        _make_repo(path)
        # We mock remote_url, so just need the .git dir to exist

    def test_uses_remote_for_display_name(self, tmp_path: Path):
        _make_repo(tmp_path / "my-repo")
        with patch("grove.discover.remote_url", return_value="git@github.com:org/my-repo.git"):
            result = discover_repos([tmp_path])
        assert len(result) == 1
        assert result[0].display_name == "org/my-repo"
        assert result[0].name == "my-repo"

    def test_falls_back_to_folder_name(self, tmp_path: Path):
        _make_repo(tmp_path / "local-only")
        with patch("grove.discover.remote_url", return_value=None):
            result = discover_repos([tmp_path])
        assert len(result) == 1
        assert result[0].display_name == "local-only"

    def test_dedup_by_remote_url(self, tmp_path: Path):
        dir1 = tmp_path / "dir1"
        dir2 = tmp_path / "dir2"
        _make_repo(dir1 / "repo-a")
        _make_repo(dir2 / "repo-a-fork")
        url = "git@github.com:org/repo-a.git"
        with patch("grove.discover.remote_url", return_value=url):
            result = discover_repos([dir1, dir2])
        # Same remote → deduped to one entry
        assert len(result) == 1

    def test_different_remotes_not_deduped(self, tmp_path: Path):
        dir1 = tmp_path / "dir1"
        dir2 = tmp_path / "dir2"
        _make_repo(dir1 / "repo")
        _make_repo(dir2 / "repo")

        def mock_remote(path, remote="origin"):
            if "dir1" in str(path):
                return "git@github.com:org-a/repo.git"
            return "git@github.com:org-b/repo.git"

        with patch("grove.discover.remote_url", side_effect=mock_remote):
            result = discover_repos([dir1, dir2])
        assert len(result) == 2
        names = {r.display_name for r in result}
        assert names == {"org-a/repo", "org-b/repo"}

    def test_prefers_direct_child_over_nested(self, tmp_path: Path):
        _make_repo(tmp_path / "repo-a")
        _make_repo(tmp_path / "nested" / "repo-a")
        url = "git@github.com:org/repo-a.git"
        with patch("grove.discover.remote_url", return_value=url):
            result = discover_repos([tmp_path])
        assert len(result) == 1
        assert result[0].path == tmp_path / "repo-a"

    def test_finds_nested_repos(self, tmp_path: Path):
        _make_repo(tmp_path / "sub" / "deep-repo")
        with patch("grove.discover.remote_url", return_value="git@github.com:org/deep.git"):
            result = discover_repos([tmp_path])
        assert len(result) == 1
        assert result[0].display_name == "org/deep"


class TestDiscoveryPerformance:
    """Stress tests verifying parallel + cached discovery stays fast."""

    REPO_COUNT = 200

    def _make_many_repos(self, tmp_path: Path) -> Path:
        """Create REPO_COUNT fake repos under tmp_path."""
        for i in range(self.REPO_COUNT):
            repo = tmp_path / f"repo-{i:03d}"
            _make_repo(repo)
            # Write a .git/config so the cache mtime key works
            (repo / ".git" / "config").write_text(f"[remote]\nurl = fake-{i}\n")
        return tmp_path

    def test_parallel_discovery_faster_than_sequential(self, tmp_path: Path):
        """With many repos, parallel batch should be significantly faster
        than sequential subprocess calls."""
        base = self._make_many_repos(tmp_path)

        # Simulate a slow remote_url (20ms per call — realistic for git subprocess)
        call_count = 0

        def slow_remote(path, remote="origin"):
            nonlocal call_count
            call_count += 1
            time.sleep(0.02)
            return f"git@github.com:org/{path.name}.git"

        with patch("grove.discover.remote_url", side_effect=slow_remote):
            start = time.monotonic()
            result = discover_repos([base])
            elapsed = time.monotonic() - start

        assert len(result) == self.REPO_COUNT
        # Sequential would take 200 * 20ms = 4s minimum.
        # Parallel with 16 threads should finish in well under 2s.
        assert elapsed < 2.0, (
            f"discover_repos took {elapsed:.1f}s for {self.REPO_COUNT} repos "
            f"(expected < 2s with parallelism)"
        )

    def test_cached_discovery_skips_subprocesses(self, tmp_path: Path):
        """Second call should use cache and make zero subprocess calls."""
        base = self._make_many_repos(tmp_path)

        def fast_remote(path, remote="origin"):
            return f"git@github.com:org/{path.name}.git"

        # First call populates cache
        cache_dir = tmp_path / "cache"
        cache_file = cache_dir / "r.json"
        with (
            patch("grove.discover._CACHE_DIR", cache_dir),
            patch("grove.discover._REMOTE_CACHE_FILE", cache_file),
            patch("grove.discover.remote_url", side_effect=fast_remote),
        ):
            discover_repos([base])

        # Second call — remote_url should NOT be called
        with (
            patch("grove.discover._CACHE_DIR", cache_dir),
            patch("grove.discover._REMOTE_CACHE_FILE", cache_file),
            patch(
                "grove.discover.remote_url",
                side_effect=AssertionError("cache miss"),
            ),
        ):
            result = discover_repos([base])

        assert len(result) == self.REPO_COUNT

    def test_batch_resolve_uses_threads(self, tmp_path: Path):
        """_batch_resolve_remotes should run git calls in parallel."""
        base = self._make_many_repos(tmp_path)
        repo_paths = sorted(base.iterdir())

        concurrent_peak = 0
        active = 0

        import threading

        lock = threading.Lock()

        def counting_remote(path, remote="origin"):
            nonlocal concurrent_peak, active
            with lock:
                active += 1
                concurrent_peak = max(concurrent_peak, active)
            time.sleep(0.01)
            with lock:
                active -= 1
            return f"git@github.com:org/{path.name}.git"

        with patch("grove.discover.remote_url", side_effect=counting_remote):
            _batch_resolve_remotes(repo_paths)

        # Should have had multiple concurrent calls
        assert concurrent_peak > 1, f"Peak concurrency was {concurrent_peak}, expected > 1"
