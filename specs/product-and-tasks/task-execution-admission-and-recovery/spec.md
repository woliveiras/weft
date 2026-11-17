---
id: TASK-007
title: "Task Execution Admission and Recovery"
summary: "Start a reviewed workflow copy once, preserve its scoped execution state and recover without inventing success."
status: implemented
scope: ["task execution admission and recovery"]
navigation: ["specs/README.md","specs/product-and-tasks/task-execution-admission-and-recovery/matrix.md"]
documentation: ["docs/task-workflows.md","docs/execution-model.md","docs/state-machines.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Task Execution Admission and Recovery

Start a reviewed workflow copy once, preserve its scoped execution state and recover without inventing success.

## Behavior and invariants

- A reviewed task starts a version-pinned execution copy using the recorded input and result bindings. Retry, concurrent start or uncertain preparation does not redispatch the task or stop the source workflow.

## Acceptance criteria

### AC-001 — Versioned one-time admission

A reviewed task starts a version-pinned execution copy using the recorded input and result bindings. Retry, concurrent start or uncertain preparation does not redispatch the task or stop the source workflow.

### AC-002 — Environment and preparation gates

The chosen workflow, native or sandbox environment is revalidated. Sandbox preparation and reviewed execution start remain separate; missing capabilities and failed preparation are visible and block execution.

### AC-003 — Pause and input admission

Durable pause state is checked before and after a queue wait. Paused input waits for resume or returns on cancellation; storage failures defer execution.

### AC-004 — Settled outcome

Progress tracks owned executions and pending work. Retained output, failed/expired gates and failed preparation remain distinguishable from successful completion.

### AC-005 — Task-to-workflow contract

A workflow is a saved graph definition, a task is a durable request against a
reviewed workflow version, and a registered project identifies the source
repository for task workspaces. Admission captures the selected graph version,
entry point and input bindings. The task owns its execution and result links,
which remain available after refresh. Concurrent reuse preserves independent
execution scope and uses the existing engine.

## Explicit exclusions

- Final installed acceptance is PROD-04. Task consent and project worktree ownership are TASK-006 and TASK-008.

## Open decisions

- Early FLOW-03 checkpoints mark isolated admission and bindings as pending; later checkpoints qualify them. Preserve the chronology and the boundary of each run.

## History

- 2026-10-18. The pause race was corrected using durable admission state around queue waits.
- 2026-10-19. FLOW-03b/03d added execution copies and structured ingress. FLOW-05/06 added execution-location review, separate preparation and failure visibility.

## Reconciliation

- [task-workflows.md](../../../docs/task-workflows.md), [execution-model.md](../../../docs/execution-model.md), [state-machines.md](../../../docs/state-machines.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
