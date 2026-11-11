---
id: TASK-004
title: "Execution Activity and Run Diagnostics"
summary: "Expose execution state, pending decisions and bounded health diagnostics through Activity and Runs."
status: implemented
scope: ["execution activity and run diagnostics"]
navigation: ["specs/README.md","specs/product-and-tasks/execution-activity-and-run-diagnostics/matrix.md"]
documentation: ["docs/interface-navigation.md","docs/events.md","docs/product-contracts.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Execution Activity and Run Diagnostics

Expose execution state, pending decisions and bounded health diagnostics through Activity and Runs.

## Behavior and invariants

- Run details use persisted execution and message identities. A tool failure, a terminal run outcome and a graph-wide review remain separately visible.

## Acceptance criteria

### AC-001 — Execution identity and outcome

Run details use persisted execution and message identities. A tool failure, a terminal run outcome and a graph-wide review remain separately visible.

### AC-002 — Current activity and pending work

Activity reports running, waiting, failed and finished work in its window and links actionable approvals and reviews to their original records.

### AC-003 — Health windows

Health aggregates bounded agent and workflow duration, failures and cost for the supported 24-hour and seven-day windows.

## Explicit exclusions

- Execution-tree cancellation belongs to RUN-002. Production alert routing belongs to PROD-10.

## Open decisions

- The deferred developer walkthrough still needs to qualify comprehension of execution state and tool outcomes without facilitation.

## History

- 2026-10-17. UX-02 and UX-03 addressed event layout and ambiguity in tool-call counts.
- 2026-10-19. FLOW-07c added scoped execution evidence to the task page.
- 2026-11-11. OBS-01 and OBS-02 delivered the installation-wide Activity and Health views.

## Reconciliation

- [interface-navigation.md](../../../docs/interface-navigation.md), [events.md](../../../docs/events.md), [product-contracts.md](../../../docs/product-contracts.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
