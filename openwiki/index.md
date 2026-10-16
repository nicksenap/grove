---
okf_version: "0.2"
---

# Files

- [Architecture](architecture.md) - Runtime and ownership map for Grove, from the thin `gw` entrypoint and Cobra commands through discovery, configuration, workspace orchestration, Git, state and statistics, lifecycle hooks, output, logging, and external plugin fallback.
- [Domain Concepts](concepts.md) - Grove models discovered repositories, workspaces, Git worktrees, configuration, provenance, and lifecycle records as separate concerns. This page defines the identity rules, branch and hook semantics, persistence compatibility, output contracts, and failure-safe invariants that changes must preserve.
- [Integrations](integrations.md) - Integration contracts for Grove's Git worktrees, shell and output surfaces, lifecycle hooks, external plugins, source provenance, and the curated plugin registry.
- [Operations and Runbook](operations.md) - Configure, diagnose, clean up, extend, and release Grove safely. Covers on-disk state, hooks, Git tracing, plugins, recovery commands, and contributor validation for failed workspace operations.
- [Quickstart](quickstart.md) - Short, safe path for installing and initializing Grove (`gw`), creating and operating a multi-repository workspace, and cleaning it up. Routes implementation, workflow, operations, integration, and testing questions to the deeper pages.
- [Testing and Quality Gates](testing.md) - How to validate Grove from focused package tests through compiled-binary end-to-end scenarios and the CI or release gate. Maps the tests and injected seams that protect CLI behavior, workspace lifecycle, state persistence, Git integration, hooks, and logging.
- [Workflows](workflows.md) - End-to-end Grove workflows for discovery, workspace creation, operation, synchronization, navigation, cleanup, pruning, hooks, and machine-readable automation. Documents ordering, locks, Git boundaries, rollback, quarantine, and partial-failure behavior.
