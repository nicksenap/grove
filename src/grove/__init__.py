"""Grove — Git Worktree Workspace Orchestrator."""


def __getattr__(name: str) -> str:
    if name == "__version__":
        from importlib.metadata import version

        v = version("grove")
        globals()["__version__"] = v
        return v
    raise AttributeError(f"module {__name__!r} has no attribute {name!r}")
