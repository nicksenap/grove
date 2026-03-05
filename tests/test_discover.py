"""Tests for grove.discover — repo discovery across directories."""

from __future__ import annotations

from pathlib import Path

from grove.discover import explore_repos, find_all_repos, find_repos


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
