"""MCP server for cross-workspace communication between Claude Code instances."""

from __future__ import annotations

import sqlite3
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager
from dataclasses import dataclass

from mcp.server.fastmcp import FastMCP

from grove import mcp_store, state

# Set from CLI before server starts.
_workspace_id: str = ""


@dataclass
class _AppContext:
    db: sqlite3.Connection


@asynccontextmanager
async def _lifespan(server: FastMCP) -> AsyncIterator[_AppContext]:
    db = mcp_store.open_db()
    try:
        yield _AppContext(db=db)
    finally:
        mcp_store.close_db(db)


mcp = FastMCP("grove", lifespan=_lifespan)


@mcp.tool()
def announce(repo_url: str, category: str, message: str) -> str:
    """Broadcast an announcement to other workspaces working on the same repo.

    Use this when you make breaking changes, start/finish major work, or need
    to warn other Claude Code instances about something important.

    Args:
        repo_url: The remote URL of the repo this announcement is about.
        category: One of: breaking_change, status, warning, info.
        message: A clear description of what happened or what others should know.
    """
    ctx = mcp.get_context()
    db = ctx.request_context.lifespan_context.db
    try:
        row_id = mcp_store.insert_announcement(db, _workspace_id, repo_url, category, message)
    except ValueError as e:
        return f"Error: {e}"
    return f"Announcement #{row_id} published ({category})"


@mcp.tool()
def get_announcements(repo_url: str, since: str | None = None) -> list[dict]:
    """Check for announcements from other workspaces working on the same repo.

    Call this before starting work on a file or module to see if another
    workspace has made breaking changes or has warnings you should know about.

    Args:
        repo_url: The remote URL of the repo to check announcements for.
        since: Optional ISO datetime to filter announcements (e.g. "2025-01-15T10:00:00").
    """
    ctx = mcp.get_context()
    db = ctx.request_context.lifespan_context.db
    return mcp_store.query_announcements(db, repo_url, exclude_workspace=_workspace_id, since=since)


@mcp.tool()
def list_workspaces() -> list[dict]:
    """List all active Grove workspaces and their repos/branches.

    Use this to see what other workspaces exist and what branches they are
    working on. Helps you understand who else might be affected by your changes.
    """
    workspaces = state.load_workspaces()
    return [
        {
            "name": ws.name,
            "branch": ws.branch,
            "path": str(ws.path),
            "repos": [
                {
                    "repo_name": r.repo_name,
                    "branch": r.branch,
                    "source_repo": str(r.source_repo),
                }
                for r in ws.repos
            ],
        }
        for ws in workspaces
    ]


def run_server(workspace_id: str) -> None:
    """Start the MCP server. Called from the CLI."""
    if not workspace_id:
        raise ValueError("workspace_id must not be empty")
    global _workspace_id  # noqa: PLW0603
    _workspace_id = workspace_id
    mcp.run(transport="stdio")
