---
okf_version: "0.2"
---

# Files

- [Architecture](architecture.md) - System map for Grove's Cobra CLI, operation orchestration, lifecycle-free workspace and Git boundaries, durable state, plugin execution, and output. Explains the end-to-end prune flow, cleanup invariants, and failure propagation.
- [Domain Concepts](concepts.md) - Grove models workspaces as persisted collections of repository worktrees and treats Git registrations, state records, timestamps, plugin identities, and lifecycle cleanup as separate but related concerns. This page defines the invariants and failure semantics that operations and extensions must preserve.
- [Integrations](integrations.md) - Integration contracts for Grove's Git worktrees, shell and output surfaces, lifecycle hooks, external plugins, source provenance, and the curated plugin registry.
- [Operations](operations.md) - Runbook for installing and operating Grove (`gw`), configuring repositories and hooks, understanding its state and cache surfaces, and recovering from failed cleanup or diagnostics. It also separates end-user troubleshooting from source-development checks and releases.
- [Quickstart](quickstart.md) - Short, safe path for installing and initializing Grove (`gw`), creating and operating a multi-repository workspace, and cleaning it up. Routes implementation, workflow, operations, integration, and testing questions to the deeper pages.
- [Testing and Change Validation](testing.md) - A practical guide to validating Grove at package, service, and compiled-binary boundaries. It maps focused prune and deletion invariants, isolated test seams, fixtures, and the repository commands used before merging.
- [Workflows](workflows.md) - End-to-end Grove guidance for discovering repositories, creating and operating workspaces, and safely deleting, pruning, recovering, or extending them. Covers lifecycle ordering, state and Git boundaries, failure recovery, hooks, age-based cleanup, and plugins.
