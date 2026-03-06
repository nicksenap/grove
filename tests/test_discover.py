"""Tests for grove.discover — repo discovery across directories."""

from __future__ import annotations

from pathlib import Path
from unittest.mock import patch

from grove.discover import discover_repos, explore_repos, find_all_repos, find_repos


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
