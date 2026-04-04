# Works with AI coding tools

Worktrees mean isolation. That makes Grove a natural fit for tools like [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — spin up a workspace, let your AI agent work across repos without touching anything else, clean up when done:

```bash
gw create -p backend -b fix/auth-bug
claude "fix the auth token expiry bug across svc-auth and api-gateway"
gw delete fix-auth-bug   # removes worktrees, branches, and workspace
```

## Claude Code plugin

Install the [gw-claude](https://github.com/nicksenap/gw-claude) plugin to get:

- **Memory sync** — Claude Code memory carries over from source repos to worktrees and back
- **CLAUDE.md copy** — your project `CLAUDE.md` is copied into new workspaces automatically
- **Session tracking** — hook events are recorded for the dashboard

```bash
gw plugin install nicksenap/gw-claude
gw wizard   # configures hooks interactively
```

Or configure manually in `~/.grove/config.toml`:

```toml
[hooks]
post_create = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
pre_delete = "gw claude sync harvest {path}"
```

See [plugins.md](plugins.md) for the full command reference.

## Agent dashboard

The [gw-dash](https://github.com/nicksenap/gw-dash) plugin provides a kanban-style TUI for monitoring Claude Code agents across workspaces.

```bash
gw plugin install nicksenap/gw-dash
gw claude hook install   # register session tracking hooks
gw dash                  # launch the dashboard
```

See [dashboard.md](dashboard.md) for details.

## MCP server

Grove exposes a cross-workspace communication server via MCP (Model Context Protocol). This lets Claude Code agents in different workspaces announce changes and discover what other agents are working on.

The server is started automatically via `.mcp.json` — no manual setup needed.
