---
id: PROD-10
title: Production SLOs and alert ownership
summary: "Set production SLOs, route alerts to an on-call owner and maintain response runbooks."
status: in-progress
scope: [production readiness]
risk: production qualification
risk_domains: [reliability, operations]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-10/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: ["selected topology for final alert-routing qualification"]
---

# Production SLOs and alert ownership

Set production SLOs, route alerts to an on-call owner and maintain response runbooks.

## Behavior and invariants

Workspace journal metrics, capacity and collector alerts, and a recovery runbook exist. The published SLO numbers are examples. Production targets, routing and on-call ownership remain open.

## Acceptance criteria

- **AC-001 — Operational monitoring.** A representative failure or SLO breach occurs in the production configuration. The existing metrics and applicable rules make it visible and route the actionable alert to the assigned owner with runbook guidance.
- **AC-002 — Recovery.** The production drill verifies recovery and documents monitoring blind spots and escalation for the linked journal signals.

## Open decisions

- SLO targets and windows, severity thresholds, destinations, escalation and named on-call ownership.

## History

- 2026-11-11. Local Prometheus evaluated journal alerts; production thresholds and ownership remained open.

## Capability criteria

- [ENV-003 AC-006](../../workspaces-and-environments/workspace-change-journal-and-attribution/spec.md#ac-006--capacity-and-collector-monitoring).

## Reconciliation

- Supporting references: [Production observability](../../../docs/production-hardening.md#observability) and [journal alerts](../../../docs/operations.md#journal-alerts).
- [Local journal rollout and checks](../../workspaces-and-environments/workspace-change-journal-and-attribution/evidence.md).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
