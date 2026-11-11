---
id: PROD-13
title: Coding model acceptance
summary: "Choose a production coding model and demonstrate an agreed solve rate over repeated evaluations."
status: draft
scope: [production readiness]
risk: production qualification
risk_domains: [quality, cost]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-13/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: ["approved production model and agreed evaluation bar"]
---

# Coding model acceptance

Choose a production coding model and demonstrate an agreed solve rate over repeated evaluations.

## Behavior and invariants

The native runtime is the coding agent. The local-first plan records 0/4 with llama3.2 and 4/4 exploratory Gemini results on the small suite. The suite reached its ceiling with that exploratory model; production approval and the acceptance threshold remain open.

## Acceptance criteria

- **AC-001 — Measured result.** The selected model runs the agreed suite for the chosen repetitions. Results identify model, workload and run conditions, record outcomes and usage, and show whether the agreed solve rate was met.
- **AC-002 — Below bar or unavailable.** A run fails or the score falls below the approved minimum. Failures and incomplete trials stay visible. A fixture result or exploratory free-tier result does not qualify a production model.

## Open decisions

- Model, suite difficulty and cases, number of repetitions, scoring rule, minimum solve rate and cost/time trade-offs.

## Reconciliation

- Governing inputs: `local-first evaluation and native coding runtime`. [Production hardening](../../../docs/production-hardening.md#capacity-and-resilience-testing) and [deployment](../../../docs/deployment.md). [Native runtime evaluation](../../../docs/native-coding-runtime.md) and [local-first results](../../../wip/OLLAMA-PLAN.md#progresso-2026-11-11).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
