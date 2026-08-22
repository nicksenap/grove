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
on_close = "./scripts/close-workspace-pane {path}"
```

Hooks can prepare ignored files, notify an agent dashboard, persist external session metadata, or run any other workspace-level integration. See [hooks.md](hooks.md) and [plugins.md](plugins.md).

## Agent dispatch plugin

The [`gw-dispatch`](https://github.com/nicksenap/gw-dispatch) plugin creates a workspace and starts a selected coding agent there with an initial prompt:

```bash
gw plugin install nicksenap/gw-dispatch
gw dispatch -n -r api,web -P "Implement login"
```

For editor workflows, [`gw-code`](https://github.com/igor-kupczynski/gw-code) generates and opens a multi-folder editor workspace:

```bash
gw plugin install igor-kupczynski/gw-code
gw code my-workspace
```
