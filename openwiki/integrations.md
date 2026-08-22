---
type: "Reference"
title: "Integrations"
description: "Vendor-neutral Grove integration points, including external plugins, lifecycle hooks, coding agents, dashboards, and terminal multiplexers."
tags: [grove, integrations, plugins, hooks, agents]
---

# Integrations

Grove core is a Git worktree orchestrator. Coding agents, terminal multiplexers, dashboards, notifications, and archival systems integrate through external plugins and lifecycle hooks rather than vendor-specific core behavior.

## Plugin model

Any executable named `gw-<name>` can extend Grove. When a built-in command does not match, Grove searches `~/.grove/plugins/` and `$PATH`, then executes the plugin with:

| Variable | Description |
|---|---|
| `GROVE_DIR` | Grove configuration directory |
| `GROVE_CONFIG` | Path to `config.toml` |
| `GROVE_STATE` | Path to `state.json` |
| `GROVE_WORKSPACE` | Current workspace name, when detected from cwd |

Install released plugins by passing their real GitHub `OWNER/REPOSITORY` identifier to `gw plugin install`.

See [Plugin documentation](../docs/plugins.md) for installation, command dispatch, and authoring details.

## Coding-agent integration

Grove is agent-agnostic. Repository-specific instructions live in `AGENTS.md`, while reusable workflows such as review, testing, or release preparation belong in agent skills or an equivalent extension mechanism.

A typical workflow is:

```bash
gw create agent-task -p backend -b feat/agent-task
cd "$(gw go agent-task)"
<your-agent-command>
gw delete agent-task
```

Use hooks when an external agent system needs workspace lifecycle events:

```toml
[hooks]
post_create = "./scripts/workspace-created {path} {name}"
pre_delete = "./scripts/workspace-closing {path} {name}"
```

Possible integrations include preparing ignored files, publishing task metadata, recording an external session, or notifying a dashboard. Grove does not prescribe the storage format or agent vendor.

## Terminal multiplexer integration

The `gw-zellij` plugin demonstrates terminal integration:

```bash
gw plugin install nicksenap/gw-zellij
```

```toml
[hooks]
on_close = "gw zellij close-pane"
```

Other terminal multiplexers can use the same hook contract.

## Dashboards

The `gw-dash` plugin provides a workspace TUI:

```bash
gw plugin install nicksenap/gw-dash
gw dash
```

Dashboard-specific agent/session support belongs to the dashboard plugin and should be documented there rather than in Grove core.

## Workspace archival

The `gw-archive` plugin provides external workspace export and restore workflows:

```bash
gw plugin install nicksenap/gw-archive
gw archive export <name> <file>
gw archive import <file>
gw archive list
```

Archive formats and retention policy belong to the plugin rather than Grove core.

## Source provenance

Workspace source metadata can connect agents and automation back to a task or pull request:

```bash
gw create my-feature -b feat/login -r svc-a,svc-b \
  --source-url "https://github.com/org/repo/pull/42" \
  --source-provider github \
  --source-ref "42" \
  --source-title "Add login flow"
```

The metadata is stored in workspace state and exposed to hooks through `{source_url}`, `{source_ref}`, and `{source_title}`.

## Writing a plugin

A plugin can be any executable:

```bash
#!/bin/sh
# ~/.grove/plugins/gw-notify
printf 'workspace=%s event=%s\n' "${GROVE_WORKSPACE:-}" "${1:-}"
```

Register plugin commands as hooks when they should receive lifecycle events:

```toml
[hooks]
post_create = "gw notify created {path} {name}"
pre_delete = "gw notify closing {path} {name}"
```

### Best practices

1. Use Grove environment variables rather than assuming paths.
2. Accept documented placeholders explicitly.
3. Keep hooks fast or configure `stream` and `timeout` metadata.
4. Fail gracefully when optional external tooling is unavailable.
5. Keep vendor-specific state and behavior outside Grove core.
6. Test against real Git worktrees.

## Troubleshooting

If a plugin is not found, verify it is executable and installed in `~/.grove/plugins/` or on `$PATH`:

```bash
gw plugin list
which gw-example
```

If a hook does not fire, validate `~/.grove/config.toml` and run a command with `--verbose`:

```bash
gw doctor
gw --verbose create my-feature
```

See [Operations](operations.md#lifecycle-hooks) for hook execution and failure policy.
