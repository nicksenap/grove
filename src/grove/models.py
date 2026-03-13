"""Domain models for Grove."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path


@dataclass
class Config:
    """Global Grove configuration."""

    repo_dirs: list[Path]
    workspace_dir: Path
    presets: dict[str, list[str]] = field(default_factory=dict)
    claude_memory_sync: bool = False

    def to_dict(self) -> dict:
        d: dict = {
            "repo_dirs": [str(p) for p in self.repo_dirs],
            "workspace_dir": str(self.workspace_dir),
        }
        if self.presets:
            d["presets"] = {name: {"repos": list(repos)} for name, repos in self.presets.items()}
        return d

    @classmethod
    def from_dict(cls, data: dict) -> Config:
        presets_raw = data.get("presets", {})
        # Each preset is a TOML table: [presets.name] with repos = [...]
        presets = {name: list(val["repos"]) for name, val in presets_raw.items()}

        # Backward compat: old config has repos_dir (singular)
        if "repo_dirs" in data:
            repo_dirs = [Path(p) for p in data["repo_dirs"]]
        elif "repos_dir" in data:
            repo_dirs = [Path(data["repos_dir"])]
        else:
            repo_dirs = []

        return cls(
            repo_dirs=repo_dirs,
            workspace_dir=Path(data["workspace_dir"]),
            presets=presets,
            claude_memory_sync=bool(data.get("claude_memory_sync", False)),
        )


@dataclass
class RepoWorktree:
    """A single repo's worktree within a workspace."""

    repo_name: str
    source_repo: Path  # path to the bare/main repo
    worktree_path: Path  # path to the created worktree
    branch: str

    def to_dict(self) -> dict[str, str]:
        return {
            "repo_name": self.repo_name,
            "source_repo": str(self.source_repo),
            "worktree_path": str(self.worktree_path),
            "branch": self.branch,
        }

    @classmethod
    def from_dict(cls, data: dict[str, str]) -> RepoWorktree:
        return cls(
            repo_name=data["repo_name"],
            source_repo=Path(data["source_repo"]),
            worktree_path=Path(data["worktree_path"]),
            branch=data["branch"],
        )


@dataclass
class Workspace:
    """A workspace containing worktrees from multiple repos."""

    name: str
    path: Path
    branch: str
    repos: list[RepoWorktree] = field(default_factory=list)
    created_at: str = field(default_factory=lambda: datetime.now().isoformat())

    def to_dict(self) -> dict:
        return {
            "name": self.name,
            "path": str(self.path),
            "branch": self.branch,
            "repos": [r.to_dict() for r in self.repos],
            "created_at": self.created_at,
        }

    @classmethod
    def from_dict(cls, data: dict) -> Workspace:
        return cls(
            name=data["name"],
            path=Path(data["path"]),
            branch=data["branch"],
            repos=[RepoWorktree.from_dict(r) for r in data.get("repos", [])],
            created_at=data.get("created_at", ""),
        )
