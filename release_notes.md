## What's New

### MCP server for cross-workspace communication
New `gw mcp-serve` command exposes a stdio JSON-RPC server that lets Claude Code instances in different workspaces communicate via announcements. Auto-configured via `.mcp.json` in workspace directories.

### End-to-end test suite
Comprehensive e2e tests covering init, create, delete, sync, rename, doctor, presets, MCP server, and more. Runs in Docker locally (`just e2e`) and natively in CI.

### Bug fixes
- `gw list --json` now returns `[]` instead of a text message when no workspaces exist
- Git operations no longer hang when SSH key requires a passphrase
- Repo URL normalization for consistent matching across SSH/HTTPS variants
- Announcement TTL pruning to prevent unbounded state growth
