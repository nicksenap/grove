# Works with coding agents

Worktrees provide isolated checkouts, which makes Grove a natural fit for any coding agent or agent-enabled editor. Create a workspace, launch your preferred agent from that directory, and clean up when the work is complete:

```bash
gw create agent-fix -p backend -b fix/auth-bug
cd "$(gw go agent-fix)"
<your-agent-command> "fix the auth token expiry bug across the selected repos"
gw delete agent-fix
```

## Repository guidance

Use [`AGENTS.md`](../AGENTS.md) as the repository's vendor-neutral entry point for coding agents. It links to architecture, workflows, testing guidance, and source maps in OpenWiki.

Keep reusable agent workflows in your agent's **skills** or equivalent extension mechanism rather than duplicating instructions in vendor-specific repository files. Project facts and commands belong in `AGENTS.md`; reusable methods such as code review, testing, or release preparation belong in skills.

## Lifecycle integration

Grove does not embed a particular coding agent. Use lifecycle hooks to connect any external automation:

```toml
[hooks]
post_create = "./scripts/agent-workspace-created {path}"
pre_delete = "./scripts/agent-workspace-closing {path}"
on_close = "gw zellij close-pane"
```

Hooks can prepare ignored files, notify an agent dashboard, persist external session metadata, or run any other workspace-level integration. See [hooks.md](hooks.md) and [plugins.md](plugins.md).

## Agent dashboards and plugins

External plugins can use Grove's command protocol and environment variables without adding vendor-specific behavior to core. The [`gw-dash`](https://github.com/nicksenap/gw-dash) plugin is one example of a workspace dashboard:

```bash
gw plugin install nicksenap/gw-dash
gw dash
```

See the plugin's own documentation for supported agent/session integrations.
