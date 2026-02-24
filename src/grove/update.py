"""Check for newer versions of grove on GitHub."""

from __future__ import annotations

import json
import threading
import time
from pathlib import Path

_CACHE_FILE = Path("~/.grove/update-check.json").expanduser()
_CHECK_INTERVAL = 86400  # 24 hours
_REPO = "nicksenap/grove"


def _parse_version(v: str) -> tuple[int, ...]:
    return tuple(int(x) for x in v.split("."))


def _fetch_latest() -> None:
    """Fetch latest version from GitHub and write to cache."""
    try:
        from urllib.request import Request, urlopen

        req = Request(
            f"https://api.github.com/repos/{_REPO}/releases/latest",
            headers={"Accept": "application/vnd.github.v3+json", "User-Agent": "grove"},
        )
        with urlopen(req, timeout=5) as resp:
            data = json.loads(resp.read())
            latest = data["tag_name"].lstrip("v")

        _CACHE_FILE.parent.mkdir(parents=True, exist_ok=True)
        _CACHE_FILE.write_text(json.dumps({"last_check": time.time(), "latest": latest}))
    except Exception:
        pass


def get_newer_version(current: str) -> str | None:
    """Return latest version if newer than *current*, else ``None``.

    Reads a local cache (~/.grove/update-check.json) and kicks off a background
    refresh when the cache is older than 24 h.  Never blocks and never raises.
    """
    try:
        latest: str | None = None
        last_check = 0.0

        if _CACHE_FILE.exists():
            cached = json.loads(_CACHE_FILE.read_text())
            latest = cached.get("latest")
            last_check = cached.get("last_check", 0.0)

        # Refresh in background when stale
        if time.time() - last_check >= _CHECK_INTERVAL:
            threading.Thread(target=_fetch_latest, daemon=True).start()

        if latest and _parse_version(latest) > _parse_version(current):
            return latest
    except Exception:
        pass

    return None
