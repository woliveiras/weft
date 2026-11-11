---
id: INT-004
title: "Monetary Usage and Budget Controls"
summary: "Record model spend and enforce applicable budgets with revisioned pricing and conservative reservations."
status: implemented
scope: ["monetary usage and budget controls"]
navigation: ["specs/README.md","specs/providers-and-integrations/monetary-usage-and-budget-controls/matrix.md"]
documentation: ["docs/cost-control.md","docs/api.md","docs/delegation.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Monetary Usage and Budget Controls

Record model spend and enforce applicable budgets with revisioned pricing and conservative reservations.

## Behavior and invariants

- Gateway calls retain reported or estimated usage and the applied catalogue version. Local pricing defaults to zero; unpriced models follow configured policy. Runtime-reported usage is kept separate from gateway charges.

## Acceptance criteria

### AC-001 — Usage and price records

Gateway calls retain reported or estimated usage and the applied catalogue version. Local pricing defaults to zero; unpriced models follow configured policy. Runtime-reported usage is kept separate from gateway charges.

### AC-002 — Reservation and settlement

Applicable budgets reserve capacity before dispatch, settle once after usage, and block or pause at configured hard thresholds. A reviewed raise is audited. Storage failure cannot free unresolved credit or allow a covered call to bypass admission.

### AC-003 — Budget visibility

Authorized operators see spend, applicable remaining budget, estimates and relevant warning or hard-limit state. Admin changes retain review and scope.

## Explicit exclusions

- The delegation tree token ledger is RUN-002. The per-tree monetary ceiling and production catalogue/terms are separate decisions.

## Open decisions

- The earlier local-first plan says COST closes the delegation monetary ceiling. The release index retains an enforceable delegation-wide ceiling as open. Confirm the remaining policy and accounting scope before qualifying it.

## History

- 2026-10-30. The local-first cost phase selected zero default Ollama price and synthetic prices for enforcement tests.
- 2026-11-11. COST-01–05 are recorded as delivered. The optional native Ollama journey exercised local cost recording.

## Reconciliation

- [cost-control.md](../../../docs/cost-control.md), [api.md](../../../docs/api.md), [delegation.md](../../../docs/delegation.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
