---
id: RUN-004
title: "Coding Evaluation and Model Benchmarks"
summary: "Measure coding workflows with independent task checks and retain the conditions and limits of each benchmark."
status: implemented
scope: ["coding evaluation and model benchmarks"]
navigation: ["specs/README.md","specs/runtime-and-delivery/coding-evaluation-and-model-benchmarks/matrix.md"]
documentation: ["docs/native-coding-runtime.md","docs/production-hardening.md"]
dependencies: ["RUN-003"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Coding Evaluation and Model Benchmarks

Measure coding workflows with independent task checks and retain the conditions and limits of each benchmark.

## Behavior and invariants

- A case runs through the engine, runtime and gateway. A solve requires both the independent repository check and turn completion. Scripted correct solutions pass, idle runs and plausible wrong fixes fail.

## Acceptance criteria

### AC-001 — Task verdict

A case runs through the engine, runtime and gateway. A solve requires both the independent repository check and turn completion. Scripted correct solutions pass, idle runs and plausible wrong fixes fail.

### AC-002 — Repeatable benchmark report

A run records model, workflow, case and attempt outcomes, tool calls/errors, usage and time. Repeat counts preserve the per-attempt denominator; fixtures and live opt-in report separately.

### AC-003 — Comparable measurements

Keep observed scores and run conditions distinct across changed cases, workflows
and harness versions. Annotate a suspected harness failure without adjusting
the observed score. Record a corrected rerun as a separate result.

## Explicit exclusions

- The production model and minimum accepted rate belong to PROD-13. The exploratory cloud tier supplies benchmark history only.

## Open decisions

- The original local-model Factory acceptance target was left unset after a 0/4 run. Choose repetitions and workload with enough headroom before using solve rate as a production gate.

## History

- 2026-11-10. NAT-01 established four baseline coding cases and measured the local models before context changes.
- 2026-11-11. The benchmark expanded to nine cases and repeat support after the four-case hosted runs hit the ceiling.

## Reconciliation

- [native-coding-runtime.md](../../../docs/native-coding-runtime.md), [production-hardening.md](../../../docs/production-hardening.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
