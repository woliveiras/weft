---
id: TASK-003
title: "Graph Editing and Versioned Saves"
summary: "Edit complete graph definitions in the canvas and recover safely from validation and save conflicts."
status: implemented
scope: ["graph editing and versioned saves"]
navigation: ["specs/README.md","specs/product-and-tasks/graph-editing-and-versioned-saves/matrix.md"]
documentation: ["docs/api.md","docs/product-contracts.md","docs/interface-navigation.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Graph Editing and Versioned Saves

Edit complete graph definitions in the canvas and recover safely from validation and save conflicts.

## Behavior and invariants

- A valid whole-document save with the current If-Match revision persists supported graph edits, positions and preserved extension fields. A stale revision leaves server state intact and the local draft recoverable.

## Acceptance criteria

### AC-001 — Versioned graph save

A valid whole-document save with the current If-Match revision persists supported graph edits, positions and preserved extension fields. A stale revision leaves server state intact and the local draft recoverable.

### AC-002 — Canvas interaction

Users create, connect, configure and remove agents or channels with explicit destructive confirmation. Runtime state updates and reopening preserve positions and restore visible saved edges.

### AC-003 — Advanced editing

The YAML editor remains available. Graph validation identifies the affected element or field, and advanced fields the visual form does not edit survive a round trip.

## Explicit exclusions

- Reusable components and their version updates are governed by RELEASE-2.

## Open decisions

- The original human walkthrough still needs to confirm graph discovery and editing without facilitation under PROD-04.

## History

- 2026-10-17. Visual consolidation and graph selection improved access to existing graphs.
- 2026-10-18. A reload/reopen regression was traced to discarded connection measurements. The canvas refreshes those measurements on structural changes and reveal.

## Reconciliation

- [api.md](../../../docs/api.md), [product-contracts.md](../../../docs/product-contracts.md), [interface-navigation.md](../../../docs/interface-navigation.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
