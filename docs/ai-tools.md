# Works with AI coding tools

Worktrees mean isolation. That makes Grove a natural fit for tools like [Claude Code](https://docs.anthropic.com/en/docs/claude-code) — spin up a workspace, let your AI agent work across repos without touching anything else, clean up when done:

```bash
gw create -p backend -b fix/auth-bug
claude "fix the auth token expiry bug across svc-auth and api-gateway"
gw delete fix-auth-bug   # removes worktrees, branches, and workspace
```

## Golden path: full Claude Code + Zellij setup

```bash
# Install plugins
gw plugin install nicksenap/gw-claude
gw plugin install nicksenap/gw-zellij
gw plugin install nicksenap/gw-dash

# Register Claude Code session tracking hooks
gw claude hook install
```

Add to `~/.grove/config.toml`:

```toml
[hooks]
post_create = "gw claude sync rehydrate {path} && gw claude copy-md {path}"
pre_delete = "gw claude sync harvest {path}"
on_close = "gw zellij close-pane"
```

Or just run `gw wizard` to do this interactively.

That gives you: memory sync on create/delete, CLAUDE.md in every workspace, session tracking dashboard, and Zellij pane close on navigate-away.

---

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

See the [gw-dash README](https://github.com/nicksenap/gw-dash) for keybindings, Zellij integration, and architecture.

## No MCP server — use the CLI

Grove has no built-in MCP server. The `gw` CLI *is* the agent interface: any agent
with shell access can drive Grove through ordinary commands with stable
machine-readable output.

```bash
gw context --format json     # where am I, what state is everything in
gw list --format json
gw status --format json
gw create feat-x -r svc-auth,api-gateway -b feat/x --format json
```

See [Agent CLI contract](agent-cli.md) for the response envelope, error codes, and
exit codes.

### Migrating from the old MCP server

Workspaces created by Grove ≤ 1.1.11 have a `grove` entry in their `.mcp.json`
pointing at the removed `gw mcp-serve`. Clean it up with:

```bash
gw doctor         # reports stale entries
gw doctor --fix   # removes only the grove entry, keeping other servers
```

Cross-workspace coordination survived the removal as first-class commands:

```bash
gw announce -c breaking_change -m "auth tokens are now opaque strings"
gw announcements --format json
```

Recent notes about your repos also appear in `gw context`, so a parallel agent
receives them while orienting rather than having to remember to ask.
