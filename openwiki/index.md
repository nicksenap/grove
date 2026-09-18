---
okf_version: "0.2"
---

# Files

- [Architecture](architecture.md) - Repository-specific architecture of Grove's Cobra CLI, workspace orchestration, Git subprocess boundary, discovery, persistence, lifecycle hooks, plugins, concurrency, and safe cleanup.
- [Core Concepts and Invariants](concepts.md) - Grove models a workspace as a named, persisted collection of Git worktrees, with one recorded branch and source identity per repository. This page defines discovery, configuration, provenance, lifecycle, and state rules that changes must preserve.
- [Integrations](integrations.md) - Stable contracts for extending Grove with external gw-* plugins, GitHub release installation, lifecycle hooks, shell integration, provenance, editors, coding agents, and per-repository automation.
- [Operations](operations.md) - Runbook for installing and operating Grove (`gw`), configuring repositories and hooks, understanding its state and cache surfaces, and recovering from failed cleanup or diagnostics. It also separates end-user troubleshooting from source-development checks and releases.
- [Grove Documentation](quickstart.md) - Engineer entry point for installing and using Grove (gw) to discover repositories and manage multi-repository Git worktree workspaces. Summarizes the core lifecycle, configuration, extension boundary, and routes to deeper design and operating guidance.
- [Testing and Change Validation](testing.md) - A practical guide to validating Grove changes from focused Go tests through repository-wide checks and compiled-binary end-to-end scenarios. It maps high-value invariants, test isolation patterns, optional network coverage, and release validation to the commands that exercise them.
- [Workflows](workflows.md) - End-to-end Grove workflows for discovery, workspace lifecycle, multi-repository synchronization, navigation, hooks, and recovery. Describes state transitions, safety checks, rollback, and per-repository failure behavior.
