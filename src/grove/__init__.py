"""Grove — Git Worktree Workspace Orchestrator."""


def __getattr__(name: str) -> str:
    if name == "__version__":
        try:
            from importlib.metadata import version

            try:
                v = version("gw-cli")
            except Exception:
                v = version("grove")
        except Exception:
            import logging

            logging.getLogger("grove").debug("could not read package version", exc_info=True)
            v = "unknown"
        globals()["__version__"] = v
        return v
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
