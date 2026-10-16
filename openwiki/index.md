---
okf_version: "0.2"
---

# Files

- [Architecture](architecture.md) - Runtime and ownership map for Grove, from Cobra command entrypoints through discovery, configuration, workspace orchestration, Git, durable state, lifecycle hooks, output, logging, and external plugins.
- [Concepts and Data Model](concepts.md) - Grove separates workspace intent and provenance from Git worktree state, repository discovery, configuration, hooks, and durable state. This page defines the branch, identity, persistence, lifecycle, and rollback invariants required when changing those boundaries.
- [Integrations](integrations.md) - Public integration contracts between Grove and Git, shells, lifecycle hooks, external plugins, source providers, editors, installers, and machine-oriented consumers.
- [Operations and Runbook](operations.md) - Configure, diagnose, clean up, extend, and release Grove safely. Covers on-disk state, hooks, Git tracing, plugins, recovery commands, and contributor validation for failed workspace operations.
- [Quickstart](quickstart.md) - The shortest safe path from installing and initializing Grove (`gw`) to creating, navigating, inspecting, synchronizing, and removing a multi-repository workspace. Includes first-use safety notes and routes command, hook, configuration, integration, and testing questions to deeper pages.
- [Testing and Verification](testing.md) - A maintainer guide to validating Grove changes from focused package tests through compiled-binary end-to-end checks and CI. It maps coverage to ownership boundaries and highlights lifecycle ordering, state and Git safety, hooks, output, logging, and quality gates.
- [Workflows](workflows.md) - End-to-end Grove workflows for discovery, workspace creation, operation, synchronization, navigation, cleanup, pruning, hooks, and machine-readable automation. Documents ordering, locks, Git boundaries, rollback, quarantine, and partial-failure behavior.
