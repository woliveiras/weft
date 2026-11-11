---
id: PROD-03
title: Production model providers and budgets
summary: "Qualify production inference under approved data terms, real prices and enforced budgets."
status: planned
scope: [production readiness]
risk: production blocker
risk_domains: [data protection, secrets, cost]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-03/matrix.md]
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
- **AC-002 — Pricing.** The selected catalogue entry uses independently verified production prices and the observed charge matches the reviewed provider terms.
- **AC-003 — Budget exhaustion.** The production qualification applies the approved project and user budget configuration and records the linked INT-004 admission outcomes.

## Open decisions

- Provider, service tier, model and region; approved data terms; verified prices and update ownership; project/user amounts and any unpriced-model policy.

## History

- 2026-11-11. The production checklist separated approved provider terms and real prices from local and exploratory evaluation.

## Capability criteria

- [INT-002 AC-003](../../providers-and-integrations/model-provider-adapters-and-conformance/spec.md#ac-003--qualification-scope).
- [INT-004 AC-001](../../providers-and-integrations/monetary-usage-and-budget-controls/spec.md#ac-001--usage-and-price-records).
- [INT-004 AC-002](../../providers-and-integrations/monetary-usage-and-budget-controls/spec.md#ac-002--reservation-and-settlement).

## Reconciliation

- Supporting references: [Production hardening](../../../docs/production-hardening.md#cost-controls), [cost control](../../../docs/cost-control.md), and [coding evaluations](../../../docs/native-coding-runtime.md#evaluation-suite-nat-01).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
