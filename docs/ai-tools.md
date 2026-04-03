# Works with AI coding tools

Worktrees mean isolation. That makes Grove a natural fit for tools like [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — spin up a workspace, let your AI agent work across repos without touching anything else, clean up when done:

```bash
gw create -p backend -b fix/auth-bug
claude "fix the auth token expiry bug across svc-auth and api-gateway"
gw delete fix-auth-bug   # removes worktrees, branches, and workspace
```

## CLAUDE.md syncing

Grove copies your `CLAUDE.md` into new workspaces, so your agent gets project context from the start.

## Agent dashboard

Grove includes a [dashboard](dashboard.md) for monitoring multiple Claude Code agents across workspaces. Install hooks with `gw dash install`, then launch with `gw dash`.

## MCP server

Grove exposes a cross-workspace communication server via MCP (Model Context Protocol). This lets Claude Code agents in different workspaces announce changes and discover what other agents are working on.

The server is started automatically via `.mcp.json` — no manual setup needed.
