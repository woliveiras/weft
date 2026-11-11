---
id: ENV-006
title: "Sandbox Capacity and Idle Resume"
summary: "Bound preparation and execution resources and resume idle containers through admission."
status: implemented
scope: ["sandbox capacity and idle resume"]
navigation: ["specs/README.md","specs/workspaces-and-environments/sandbox-capacity-and-idle-resume/matrix.md"]
documentation: ["docs/sandbox-runtime.md","docs/environments.md","docs/sandbox-acceptance.md"]
dependencies: ["ENV-005"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Sandbox Capacity and Idle Resume

Bound preparation and execution resources and resume idle containers through admission.

## Behavior and invariants

- CPU, memory and active counts are reserved atomically across controllers and applicable helper/proxy groups. Queued, cancelled and failed requests report their state and release capacity according to ownership.

## Acceptance criteria

### AC-001 — Shared resource admission

CPU, memory and active counts are reserved atomically across controllers and applicable helper/proxy groups. Queued, cancelled and failed requests report their state and release capacity according to ownership.

### AC-002 — Suspend and readmit

An eligible idle container stops without losing files or identity. Resume reacquires capacity and checks current policy before execution. A paused task stays paused and active turns remain protected.

### AC-003 — Companion groups

A reviewed dependency group starts and releases its owned resources as a bounded unit. Suspension preserves the companion data within the supported runtime subset.

## Explicit exclusions

- Idle suspension does not snapshot process memory. Unsupported Compose features remain outside ENV-007.

## Open decisions

- Capacity qualification for a selected production topology remains separate from the local dedicated-daemon evidence.

## History

- 2026-10-18 Release 1 checkpoint. The sandbox and environment sections record CPU/memory/count admission, helpers and idle continuation.

## Reconciliation

- [sandbox-runtime.md](../../../docs/sandbox-runtime.md), [environments.md](../../../docs/environments.md), [sandbox-acceptance.md](../../../docs/sandbox-acceptance.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
