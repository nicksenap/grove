---
okf_version: "0.2"
---

# Files

- [Architecture](architecture.md) - System map for Grove's Cobra CLI, operation orchestration, workspace and Git boundaries, durable state, lifecycle hooks, output, and external plugin execution. Explains cleanup ordering, stale-state handling, and plugin registry versus executable discovery.
- [Domain Concepts](concepts.md) - Grove models workspaces as persisted collections of repository worktrees and treats Git registrations, state records, timestamps, plugin identities, and lifecycle cleanup as separate but related concerns. This page defines the invariants and failure semantics that operations and extensions must preserve.
- [Integrations](integrations.md) - Integration contracts for Grove's Git worktrees, shell and output surfaces, lifecycle hooks, external plugins, source provenance, and the curated plugin registry.
- [Operations](operations.md) - Runbook for installing and operating Grove (`gw`), configuring repositories and hooks, understanding its state and cache surfaces, and recovering from failed cleanup or diagnostics. It also separates end-user troubleshooting from source-development checks and releases.
- [Grove Documentation](quickstart.md) - Engineer entry point for installing and using Grove (gw) to discover repositories and manage multi-repository Git worktree workspaces. Summarizes the current initialization, creation, operation, and cleanup lifecycle, with routes to deeper design, operations, integration, workflow, and testing guidance.
- [Testing and Change Validation](testing.md) - A practical map of Grove's focused, integration, and end-to-end tests for prune selection, workspace deletion, plugin discovery, and repository-wide quality checks. It records the invariants that make cleanup safe and the commands that exercise them.
- [Workflows](workflows.md) - End-to-end Grove guidance for discovering repositories, creating and operating workspaces, and safely deleting, pruning, recovering, or extending them. Covers lifecycle ordering, state and Git boundaries, failure recovery, hooks, age-based cleanup, and plugins.
