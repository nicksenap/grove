"""Domain models for Grove."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path


@dataclass
class Config:
    """Global Grove configuration."""

    repos_dir: Path
    workspace_dir: Path

    def to_dict(self) -> dict[str, str]:
        return {
            "repos_dir": str(self.repos_dir),
            "workspace_dir": str(self.workspace_dir),
        }

    @classmethod
    def from_dict(cls, data: dict[str, str]) -> Config:
        return cls(
            repos_dir=Path(data["repos_dir"]),
            workspace_dir=Path(data["workspace_dir"]),
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
