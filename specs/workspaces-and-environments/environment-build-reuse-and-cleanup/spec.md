---
id: ENV-008
title: "Environment Build, Reuse and Cleanup"
summary: "Build and reuse compatible environments while retaining isolated workspaces and protecting referenced generations."
status: implemented
scope: ["environment build reuse and cleanup"]
navigation: ["specs/README.md","specs/workspaces-and-environments/environment-build-reuse-and-cleanup/matrix.md"]
documentation: ["docs/environments.md","docs/sandbox-acceptance.md","docs/product-contracts.md"]
dependencies: ["ENV-006","ENV-007","ENV-009"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Environment Build, Reuse and Cleanup

Build and reuse compatible environments while retaining isolated workspaces and protecting referenced generations.

## Behavior and invariants

- Preparation exposes bounded build/setup logs, phases, failure and cancellation. An environment-dependent node waits for a ready definition and interrupted work is reconciled.

## Acceptance criteria

### AC-001 — Build lifecycle and progress

Preparation exposes bounded build/setup logs, phases, failure and cancellation. An environment-dependent node waits for a ready definition and interrupted work is reconciled.

### AC-002 — Fingerprint and reuse

Compatible definitions reuse prepared images and snapshots. Changed accepted inputs invalidate reuse, concurrent managers deduplicate builds and a task receives a private cache copy and workspace.

### AC-003 — Safe cleanup

Rebuild, reset and unused-generation cleanup preserve resources with live references. Blocked cleanup or dependency failure remains visible and can be retried safely.

## Explicit exclusions

- Exact file diffs and durable queue-position estimates are outside environment progress.

## Open decisions

- Resource policy remains independently governed by ENV-006 and ENV-009. Preserve the historical distinction between private per-graph preparation and shared generations.

## History

- 2026-10-18 Release 1 checkpoint. Containerized environments record lifecycle, cached preparation, invalidation and reviewed cleanup.

## Reconciliation

- [environments.md](../../../docs/environments.md), [sandbox-acceptance.md](../../../docs/sandbox-acceptance.md), [product-contracts.md](../../../docs/product-contracts.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
