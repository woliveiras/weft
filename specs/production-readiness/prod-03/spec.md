---
id: PROD-03
title: Production model providers and budgets
summary: "Qualify production inference under approved data terms, real prices and enforced budgets."
status: draft
scope: [production readiness]
risk: production blocker
risk_domains: [data protection, secrets, cost]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-03/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: ["existing cost catalogue and project/user budget controls"]
---

# Production model providers and budgets

Qualify production inference under approved data terms, real prices and enforced budgets.

## Behavior and invariants

The installation catalogue and budget controls already exist. The Ollama plan records a free-tier Gemini evaluation and local runs, which remain exploratory. A configured adapter or a successful inference call does not establish data terms or production qualification.

## Acceptance criteria

- **AC-001 — Qualified inference.** An installation admits a production model. The model uses a paid service covered by approved data-protection terms. A free tier that trains on inputs cannot qualify.
- **AC-002 — Pricing.** A qualified model processes requests with known usage. The catalogue holds verified prices for that model and the gateway uses them to record spend.
- **AC-003 — Budget exhaustion.** A project or user reaches its configured hard budget. The existing reservation and pause controls prevent further admission beyond the hard threshold and surface the reason.

## Open decisions

- Provider, service tier, model and region; approved data terms; verified prices and update ownership; project/user amounts and any unpriced-model policy.

## Reconciliation

- Governing inputs: `hardening cost controls; local-first evaluation; existing cost controls`. [Production hardening](../../../docs/production-hardening.md#cost-controls) and [deployment](../../../docs/deployment.md). [Cost control](../../../docs/cost-control.md) and [local-first evaluation](../../../wip/OLLAMA-PLAN.md#progresso-2026-11-11).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
