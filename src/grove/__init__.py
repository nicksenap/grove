"""Grove — Git Worktree Workspace Orchestrator."""


def __getattr__(name: str) -> str:
    if name == "__version__":
        try:
            from importlib.metadata import version

            v = version("grove")
        except Exception:
            v = "unknown"
        globals()["__version__"] = v
        return v
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
