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

Before resuming a graph after a raise or monthly rollover, the controller
checks the current spend and reached state of every applicable budget. A
reached budget retains the pause, and `budget.graph_transition` records the
blocking scope. The controller audits removal of holds for stopped, deleted,
finished or replaced runs, unbound legacy holds, and current-month holds
whose definition was removed or no longer matches the graph. The same
applicability and budget check gates reapplying a hold to a running graph.

Manual graph resume, graph start, and node resume also check applicable
ledgers. Starting a stopped task execution graph checks its existing
graph-keyed task-run ledger and the applicable monthly ledgers for the new
lifecycle run. A blocked request reports the ledger, preserves any hold, and
audits the operator's attempt. Increasing a monthly definition with enough
remaining capacity schedules an audited resume subject to the other
applicable ledgers. A hold with remaining capacity and a clear reached
state does not block manual resume.

An edit to active budgets during admission invalidates a reservation using
the previous definition. Partial reservations are released before retrying.
A monthly definition change preserves the floor set by an explicit Raise.
A concurrent edit or Raise invalidates a stale reconciler decision before
it can change the graph state or retire the hold.

- A reached applicable monthly ledger refuses a new task before graph,
  worktree, environment, or input dispatch. The response identifies the
  blocking ledger and the same draft can be started after a raise.
- Starting a node that would start a stopped graph checks the same ledgers
  as starting the graph, before changing its run ID or node controls.
- Reducing a limit preserves a projected refusal. An increased definition
  schedules resume only with capacity above charged and reserved spend.
- A hold whose ledger no longer applies transfers to another reached
  applicable ledger when one exists. The audit identifies that blocker.

### AC-003 — Budget visibility

Authorized operators see spend, applicable remaining budget, estimates and relevant warning or hard-limit state. Admin changes retain review and scope.

The individual ledger GET and `GET /api/v1/cost/ledgers` share the optional
`notResumed` field for pending graph IDs. The list includes monthly ledgers
for the requested UTC period and reached or paused task-run ledgers.
Closed monthly ledgers are read-only and expose historical spend without
active pause flags. Invalid ledgers in scope are identified by revision
alongside healthy ledgers. The task interface uses task progress to suppress
pause and raise controls for terminal tasks.

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
