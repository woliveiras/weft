---
id: PROD-13
title: Coding model acceptance
summary: "Choose a production coding model and demonstrate an agreed solve rate over repeated evaluations."
status: planned
scope: [production readiness]
risk: production qualification
risk_domains: [quality, cost]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-13/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: ["approved production model and agreed evaluation bar"]
---

# Coding model acceptance

Choose a production coding model and demonstrate an agreed solve rate over repeated evaluations.

## Behavior and invariants

Use the [historical benchmark record](../../runtime-and-delivery/coding-evaluation-and-model-benchmarks/evidence.md) to choose a workload and a model with an informative acceptance threshold.

## Acceptance criteria

- **AC-001 — Measured result.** An approved model meets the chosen production threshold over the agreed workload and repetitions, using the linked RUN-004 verdict and reporting criteria.
- **AC-002 — Below bar or unavailable.** A below-threshold, incomplete or unavailable evaluation leaves the production model unqualified and records the failed qualification decision.

## Open decisions

- Model, suite difficulty and cases, number of repetitions, scoring rule, minimum solve rate and cost/time trade-offs.

## History

- 2026-11-11. The production checklist made model selection and a repeated benchmark threshold explicit after the exploratory runs.

## Capability criteria

- [RUN-004 AC-001](../../runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md#ac-001--task-verdict).
- [RUN-004 AC-002](../../runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md#ac-002--repeatable-benchmark-report).

## Reconciliation

- Supporting references: [Native runtime evaluation](../../../docs/native-coding-runtime.md#evaluation-suite-nat-01) and [capacity testing](../../../docs/production-hardening.md#capacity-and-resilience-testing).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
