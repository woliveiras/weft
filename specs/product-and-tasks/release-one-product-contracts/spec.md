---
id: TASK-001
title: "Release 1 Product Contracts"
summary: "Define shared release boundaries, observable errors, and audit identity."
status: implemented
scope: ["release one product contracts"]
navigation: ["specs/README.md","specs/product-and-tasks/release-one-product-contracts/matrix.md"]
documentation: ["docs/product-contracts.md","docs/api.md","docs/events.md","docs/architecture.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Release 1 Product Contracts

Define shared release boundaries, observable errors, and audit identity.

## Behavior and invariants

- Shared API, UI and runtime contracts distinguish delivered behavior from proposals. Operation authority follows installation and resource scope.

## Acceptance criteria

### AC-001 — Scoped operation

A request receives an outcome for its authorized scope. Validation, unavailability, approval waits and version conflicts surface as distinguishable states.

### AC-002 — Audit identity

An event retains the applicable graph, agent, replica, task or execution identity so the console can correlate it with the recorded operation.

### AC-003 — Supported product surface

Users edit and operate Weft graphs directly. Legacy converter routes and UI entry points are outside the delivered product.

## Explicit exclusions

- Reusable components belong to Release 2. Authorization of a new deployment or integration belongs to the appropriate production specification.

## Open decisions

- Product Contracts labels several tables as planned. Reconcile them against the owning capacity before treating any table as an implemented guarantee.

## History

- 2026-10-19 delivery checkpoint. The [Implementation Tasks §§0–1](../../historical-source-map.md#tasksmd) record contract inventory, architecture references and legacy converter removal as complete. The original checklist does not date the individual removals.
- 2026-11-11. The initial Git snapshot preserves the product plan and existing contracts.

## Reconciliation

- [product-contracts.md](../../../docs/product-contracts.md), [api.md](../../../docs/api.md), [events.md](../../../docs/events.md), [architecture.md](../../../docs/architecture.md).
- [Behavior and oracle matrix](./matrix.md).
