---
id: PROD-15
title: Organizational approval
summary: "Obtain required organizational approval before company use involving external inference."
status: planned
scope: [production readiness]
risk: production qualification
risk_domains: [data protection, governance]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-15/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: ["company use and organizational approval authority"]
---

# Organizational approval

Obtain required organizational approval before company use involving external inference.

## Behavior and invariants

Local-first work assumed no company infrastructure, credentials or approvals. Company acceptance must be recorded separately from an individual's test account or a successful fixture.

## Acceptance criteria

- **AC-001 — Approved company use.** A company deployment intends to send source code to an external provider. Authorized records cover source-code transfer, provider data processing terms and acceptable use for the intended workload.
- **AC-002 — Approval pending or limited.** A decision is missing, denied or narrower than the proposed use. Company use outside the approval remains pending.

## Open decisions

- Applicable organizations and policies, approval owners, documented data classes and scope, and review cadence.

## History

- 2026-11-11. The production checklist made organizational approval a condition of company use.

## Reconciliation

- Supporting references: [Production provider qualification](../prod-03/spec.md) and [production LLM safety](../../../docs/production-hardening.md#llm-safety).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
