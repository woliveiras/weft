---
id: PROD-10
title: Production SLOs and alert ownership
summary: "Set production SLOs, route alerts to an on-call owner and maintain response runbooks."
status: draft
scope: [production readiness]
risk: production qualification
risk_domains: [reliability, operations]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-10/matrix.md]
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
- **AC-002 — Recovery.** The condition clears or the monitoring path fails. The drill records recovery and any blind spots. Journal capacity and collector alerts retain their existing coverage.

## Open decisions

- SLO targets and windows, severity thresholds, destinations, escalation and named on-call ownership.

## Reconciliation

- Governing inputs: `hardening observability; operations workspace journal and triage`. [Production hardening](../../../docs/production-hardening.md#observability) and [deployment](../../../docs/deployment.md). [Journal alerts](../../../docs/operations.md#journal-alerts).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
