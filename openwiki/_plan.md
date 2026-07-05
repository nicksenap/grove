# OpenWiki Documentation Plan for Grove

## Pages to Create

### openwiki/quickstart.md
- **Purpose**: Entrypoint for the wiki
- **Content**:
  - What is Grove (2–3 sentences)
  - Key features and use cases
  - How to get started (install, config, first commands)
  - Link to architecture, workflows, operations, integrations
  - Key source files and entry points
- **Source**: README.md, AGENTS.md, main.go, cmd/root.go

### openwiki/architecture.md
- **Purpose**: Explain the system design, components, and how they interact
- **Content**:
  - Core concept: git worktree orchestration
  - Package layout and responsibilities
  - Key data structures: Workspace, RepoWorktree, Config, Preset
  - State persistence model
  - Plugin architecture
  - Hook system overview
- **Source**: AGENTS.md, CLAUDE.md, internal/models/, internal/workspace/, internal/config/, internal/lifecycle/

### openwiki/workflows.md
- **Purpose**: Explain user workflows and how commands map to code
- **Content**:
  - Setup workflow (init, add-dir, explore)
  - Workspace creation (from preset, repos, or interactive)
  - Multi-repo operations (status, sync, add-repo, remove-repo)
  - Workspace cleanup (delete, rename)
  - Plugin and hook workflows
- **Source**: cmd/create.go, cmd/delete.go, cmd/status.go, cmd/sync_cmd.go, README.md

### openwiki/operations.md
- **Purpose**: Explain installation, configuration, and debugging
- **Content**:
  - Configuration file (~/.grove/config.toml)
  - Per-repo config (.grove.toml)
  - Hooks: global lifecycle hooks, per-repo hooks
  - Plugins: install, manage, write
  - Shell integration
  - Troubleshooting (gw doctor)
- **Source**: docs/hooks.md, docs/plugins.md, cmd/wizard.go, cmd/doctor.go, internal/config/

### openwiki/integrations.md
- **Purpose**: Document integrations with external tools and MCP
- **Content**:
  - Plugin ecosystem overview (gw-claude, gw-zellij, gw-dash, gw-archive)
  - MCP server (gw mcp-serve)
  - How plugins hook into lifecycle
  - Environment variables passed to plugins
- **Source**: docs/plugins.md, docs/ai-tools.md, cmd/plugin.go, internal/mcp/, cmd/mcp.go

## Evidence and Source Files

### Key Source Files
- `/cmd/gw/main.go` - Entry point
- `/cmd/root.go` - Root Cobra command setup
- `/cmd/create.go` - Workspace creation logic
- `/cmd/delete.go` - Cleanup
- `/cmd/status.go`, `/cmd/sync_cmd.go` - Multi-repo operations
- `/internal/workspace/workspace.go` - Core orchestration (27K, lots of context)
- `/internal/state/state.go` - State persistence (atomic writes)
- `/internal/models/models.go` - Data structures
- `/internal/config/config.go` - Configuration loading
- `/internal/discover/discover.go` - Repository discovery
- `/internal/gitops/gitops.go` - Git subprocess wrappers
- `/internal/lifecycle/lifecycle.go` - Hook system
- `/internal/plugin/` - Plugin management
- `/docs/hooks.md`, `/docs/plugins.md`, `/docs/ai-tools.md` - Existing docs

### Test Files (for examples)
- `/cmd/create_test.go`
- `/internal/workspace/workspace_test.go` (51K)
- `/internal/models/models_test.go`
- `/e2e/run.sh` - End-to-end test suite

## Remaining Questions
- None identified; the codebase is well-structured and the existing docs are clear

## Organization

```
openwiki/
├── .last-update.json
├── quickstart.md
├── architecture.md
├── workflows.md
├── operations.md
└── integrations.md
```

Simple flat structure with 5 pages total. No need for section directories — the pages are independent and focused.
