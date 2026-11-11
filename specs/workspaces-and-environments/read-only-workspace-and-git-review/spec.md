---
id: ENV-002
title: "Read-only Workspace and Git Review"
summary: "Inspect authorized files and Git evidence and hand off a selected workspace for human review."
status: implemented
scope: ["read only workspace and git review"]
navigation: ["specs/README.md","specs/workspaces-and-environments/read-only-workspace-and-git-review/matrix.md"]
documentation: ["docs/tool-permissions.md","docs/task-workflows.md","docs/standalone.md","docs/api.md"]
dependencies: ["TASK-008"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Read-only Workspace and Git Review

Inspect authorized files and Git evidence and hand off a selected workspace for human review.

## Behavior and invariants

- An authorized reader can list and view bounded files and inspect tracked diff, status and bounded history. Direct mutation methods are denied; paths and Git metadata stay confined to the selected workspace.

## Acceptance criteria

### AC-001 — Read-only inspection

An authorized reader can list and view bounded files and inspect tracked diff, status and bounded history. Direct mutation methods are denied; paths and Git metadata stay confined to the selected workspace.

### AC-002 — Scope and attribution

The view shows actual workspace, branch and replica and discards late responses after scope changes. Operation context and the current diff remain distinguishable from proven authorship.

### AC-003 — Review and editor handoff

A reviewed workspace can be opened in VS Code only when the native capability is available. Evidence distinguishes recorded tests and local commits from verified pushes and reports unavailable or stale state.

### AC-004 — Agent read-only workspace

Native read-only workspace policy denies mutation despite a grant. Docker also mounts it read-only and refuses incompatible writable-container reuse.

## Explicit exclusions

- Actual editor launch and human review remain part of PROD-04. Arbitrary external runtime confinement is outside this boundary.

## Open decisions

- Universal attribution is tracked in ENV-003. Task completion alone carries no remote verification.

## History

- 2026-10-18. File inspection was corrected to ignore stale reads after a scope change.
- 2026-10-19. FLOW-07 added task-local inspection, selected workspace handoff and distinct verification states.

## Reconciliation

- [tool-permissions.md](../../../docs/tool-permissions.md), [task-workflows.md](../../../docs/task-workflows.md), [standalone.md](../../../docs/standalone.md), [api.md](../../../docs/api.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
