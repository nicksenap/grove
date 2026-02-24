"""Configuration management (~/.grove/config.toml)."""

from __future__ import annotations

import tomllib
from pathlib import Path

from grove.models import Config

GROVE_DIR = Path.home() / ".grove"
CONFIG_PATH = GROVE_DIR / "config.toml"
DEFAULT_WORKSPACE_DIR = GROVE_DIR / "workspaces"


def ensure_grove_dir() -> None:
    """Create ~/.grove/ if it doesn't exist."""
    GROVE_DIR.mkdir(parents=True, exist_ok=True)


def load_config() -> Config | None:
    """Load config from TOML. Returns None if not initialized."""
    if not CONFIG_PATH.exists():
        return None
    with open(CONFIG_PATH, "rb") as f:
        data = tomllib.load(f)
    return Config.from_dict(data)


def save_config(config: Config) -> None:
    """Save config to TOML."""
    ensure_grove_dir()
    # Hand-write TOML to avoid extra dependency
    lines = [
        f'repos_dir = "{config.repos_dir}"',
        f'workspace_dir = "{config.workspace_dir}"',
    ]
    for preset_name, repos in config.presets.items():
        lines.append("")
        quoted = ", ".join(f'"{r}"' for r in repos)
        lines.append(f"[presets.{preset_name}]")
        lines.append(f"repos = [{quoted}]")
    lines.append("")
    CONFIG_PATH.write_text("\n".join(lines))


def require_config() -> Config:
    """Load config or raise an error if not initialized."""
    config = load_config()
    if config is None:
        raise SystemExit("Grove not initialized. Run: gw init <repos-dir>")
    return config
