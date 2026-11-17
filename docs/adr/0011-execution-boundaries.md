# 0011 — Explicit native and sandbox execution boundaries

**Status:** Accepted

## Context

A native installation serves the browser locally and can run a reviewed
worktree as the OS user. A Compose installation can also serve a local
browser. Installation location alone is insufficient to explain the
isolation of an individual task.

## Decision

Review and retain the task's execution location separately from installation
topology. Native worktree execution is explicit host execution using the OS
user's permissions. Docker isolation uses the dedicated daemon under
[ADR 0009](0009-dedicated-sandbox-daemon.md). Check the reviewed capability again
at admission. Surface unavailable choices and preserve the intended isolation.

## Consequences

- Native and Docker remain independently supported.
- A missing sandbox runtime blocks work requiring that sandbox. It does not
  turn a sandbox request into host execution.
- [Task admission](../../specs/product-and-tasks/task-execution-admission-and-recovery/spec.md)
  and [standalone qualification](../../specs/installation-and-operations/standalone-installation-and-recovery/spec.md)
  report the boundary actually exercised.

## Alternatives

- **Infer isolation from installation type:** obscures the boundary when the
  installation supports more than one execution location.
- **Require Docker for every native task:** removes the independent
  standalone path.
