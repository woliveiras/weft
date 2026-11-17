---
id: OPS-002
title: "Standalone Installation and Recovery"
summary: "Package the native profile, preserve offline data and qualify installation lifecycle on each supported platform."
status: in-progress
scope: ["standalone installation and recovery"]
navigation: ["specs/README.md","specs/installation-and-operations/standalone-installation-and-recovery/matrix.md"]
documentation: ["docs/standalone.md","docs/operations.md","docs/task-workflows.md"]
dependencies: ["TASK-007","TASK-008","INT-005"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Standalone Installation and Recovery

Package the native profile, preserve offline data and qualify installation lifecycle on each supported platform.

## Behavior and invariants

- The native launcher bundles the UI and initializes private SQLite and filesystem queues without Docker. It locks the selected data path, handles occupied ports and seeds Software Factory once without automatically starting it or restoring a user's removed workflow.

## Acceptance criteria

### AC-001 — Independent native profile

The native launcher bundles the UI and initializes private SQLite and filesystem queues without Docker. It locks the selected data path, handles occupied ports and seeds Software Factory once without automatically starting it or restoring a user's removed workflow.

### AC-002 — Offline backup and versioned recovery

A stopped profile backs up durable state and restores without overwriting a target. The qualified format-1 upgrade retains compatible state and rejects unsupported formats safely. External source repositories have a separate backup boundary.

### AC-003 — Foreground shutdown

Readable terminal feedback exposes local startup, browser address and shutdown progress. Shutdown drains agents, closes browser streams and reports failure while preserving recoverable state.

### AC-004 — Platform qualification record

A platform claim names the exercised operating system and runtime, model, MCP, editor and isolation boundaries. Automated macOS arm64 and real local Ollama evidence remain separate from unexercised paths.

## Explicit exclusions

- Windows is unsupported. The CLI and native bundle support Linux and macOS.
  Cloud deployments support Linux only.

- The native profile does not implicitly migrate another installation or infer sandbox isolation for host execution. Release-to-release production migration is PROD-02.

## Open decisions

- LOCAL-07b remains open for real editor/user review, optional real sandbox, external accounts and remaining Linux and macOS qualification.

## History

- 2026-10-19. The native dependency audit was followed by the packaged bundle, stopped-profile recovery, seeded Factory, format-1 upgrade, and macOS arm64 production-binary browser coverage.
- 2026-11-11. Local-first progress recorded the optional real Ollama native journey.

## Reconciliation

- [standalone.md](../../../docs/standalone.md), [operations.md](../../../docs/operations.md), [task-workflows.md](../../../docs/task-workflows.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
