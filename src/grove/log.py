"""Local file logging for Grove.

Logs to ``~/.grove/grove.log`` with automatic rotation (1 MB, 3 backups).
No data leaves the machine — this is purely for local debugging.

Usage::

    from grove.log import get_logger

    log = get_logger(__name__)
    log.info("workspace created: %s branch=%s", "my-ws", "feat/foo")
"""

from __future__ import annotations

import logging
from logging.handlers import RotatingFileHandler

from grove.config import GROVE_DIR

LOG_PATH = GROVE_DIR / "grove.log"
_MAX_BYTES = 1_000_000  # 1 MB
_BACKUP_COUNT = 3
_FORMAT = "%(asctime)s %(levelname)s %(name)s - %(message)s"
_DATE_FORMAT = "%Y-%m-%d %H:%M:%S"

_initialized = False


def setup(*, verbose: bool = False) -> None:
    """Configure the root ``grove`` logger.

    Safe to call multiple times — only the first call takes effect.
    Degrades gracefully if the log file cannot be created (e.g. permission error).
    """
    global _initialized  # noqa: PLW0603
    if _initialized:
        return

    try:
        GROVE_DIR.mkdir(parents=True, exist_ok=True)
        handler = RotatingFileHandler(
            LOG_PATH,
            maxBytes=_MAX_BYTES,
            backupCount=_BACKUP_COUNT,
            encoding="utf-8",
        )
    except OSError:
        _initialized = True  # don't retry on every command
        return

    handler.setFormatter(logging.Formatter(_FORMAT, datefmt=_DATE_FORMAT))

    root = logging.getLogger("grove")
    root.addHandler(handler)
    root.setLevel(logging.DEBUG if verbose else logging.INFO)
    _initialized = True


def get_logger(name: str) -> logging.Logger:
    """Return a logger under the ``grove`` namespace.

    If *name* already starts with ``grove.``, it is used as-is;
    otherwise ``grove.`` is prepended.
    """
    if name != "grove" and not name.startswith("grove."):
        name = f"grove.{name}"
    return logging.getLogger(name)
