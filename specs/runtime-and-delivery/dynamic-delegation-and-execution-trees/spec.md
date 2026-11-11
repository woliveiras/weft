---
id: RUN-002
title: "Dynamic Delegation and Execution Trees"
summary: "Run bounded native children under inherited grants and preserve execution-tree control and token accounting."
status: in-progress
scope: ["dynamic delegation and execution trees"]
navigation: ["specs/README.md","specs/runtime-and-delivery/dynamic-delegation-and-execution-trees/matrix.md"]
documentation: ["docs/delegation.md","docs/state-machines.md","docs/runtime-protocol.md","docs/cost-control.md"]
dependencies: ["TASK-006","INT-004"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Dynamic Delegation and Execution Trees

Run bounded native children under inherited grants and preserve execution-tree control and token accounting.

## Behavior and invariants

- An explicit delegation grant and current installation profile govern native children. Required policy waits for human review; automatic mode retains finite limits and independent approvals. The default child has no tools or workspace; selected inherited grants preserve all constraints.

## Acceptance criteria

### AC-001 — Reviewed delegation and grants

An explicit delegation grant and current installation profile govern native children. Required policy waits for human review; automatic mode retains finite limits and independent approvals. The default child has no tools or workspace; selected inherited grants preserve all constraints.

### AC-002 — Bounded child and callback lifecycle

Native recursion uses the reviewed depth and exact inherited delegation at each step. Supported external parents call the execution-scoped gateway and receive native children. Terminal failure, timeout or expiry returns a bounded result to the parent without modifying the saved graph.

### AC-003 — Execution tree and interruption

Persisted links and approvals identify each execution. An authorized child or tree interruption is durable and cooperative, expires relevant waits and releases owned capacity. Viewer reads confer no control.

### AC-004 — Tree token accounting

A shared ledger reserves bounded calls before dispatch, settles once and charges uncertain dispatched usage conservatively. Terminal recovery and retention preserve totals, fail closed on missing evidence, and reject a late attempt to reopen a closed ledger.

### AC-005 — Delegation decision audit

Runs shows the durable parent/child relationship, objective, model and profile
configuration, effective grants and bounds, and final approval outcome, actor
and time. Automatic admission is distinct from a human decision. Reported
usage and tree reservations are distinct, and legacy missing evidence is labeled.

## Explicit exclusions

- External child launch, arbitrary model/runtime overrides, cancellation rollback and an enforceable tree-wide monetary ceiling are outside the delivered scope.

## Open decisions

- Earlier checkpoints describe depth one and no external parents. Later bounded recursion supports reviewed depth up to four and external parents can request native children. Do not infer arbitrary external child support.
- Real multi-process crash recovery, cancellation-marker retention, external-runtime propagation, alternate APIs/idempotent request keys and a monetary tree ceiling remain open.

## History

- 2026-10-18. Foundation, reviewed profiles and execution controls were followed by tree-token accounting, terminal recovery and retention, then scoped grant inheritance, recursion and external-parent callbacks.

## Reconciliation

- [delegation.md](../../../docs/delegation.md), [state-machines.md](../../../docs/state-machines.md), [runtime-protocol.md](../../../docs/runtime-protocol.md), [cost-control.md](../../../docs/cost-control.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
