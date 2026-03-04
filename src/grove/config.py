"""Configuration management (~/.grove/config.toml)."""

from __future__ import annotations

import contextlib
import os
import re
import tempfile
import tomllib
from pathlib import Path

from grove.models import Config

GROVE_DIR = Path.home() / ".grove"
CONFIG_PATH = GROVE_DIR / "config.toml"
DEFAULT_WORKSPACE_DIR = GROVE_DIR / "workspaces"

# Preset names must be simple identifiers safe for TOML section headers.
_VALID_PRESET_NAME = re.compile(r"^[a-zA-Z0-9_-]+$")


def validate_preset_name(name: str) -> None:
    """Raise ``ValueError`` if *name* is not a valid preset identifier."""
    if not _VALID_PRESET_NAME.match(name):
        raise ValueError(
            f"Invalid preset name {name!r} — only letters, digits, hyphens, and underscores allowed"
        )


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
    """Save config to TOML atomically."""
    ensure_grove_dir()
    # Validate preset names before writing
    for preset_name in config.presets:
        validate_preset_name(preset_name)
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
    _atomic_write(CONFIG_PATH, "\n".join(lines))


def _atomic_write(path: Path, content: str) -> None:
    """Write *content* to *path* atomically via temp file + rename."""
    fd, tmp = tempfile.mkstemp(dir=path.parent, suffix=".tmp")
    try:
        with os.fdopen(fd, "w") as f:
            f.write(content)
        os.replace(tmp, path)
    except BaseException:
        with contextlib.suppress(OSError):
            os.unlink(tmp)
        raise


def require_config() -> Config:
    """Load config or raise an error if not initialized."""
    config = load_config()
    if config is None:
        raise SystemExit("Grove not initialized. Run: gw init <repos-dir>")
    return config
