---
id: TASK-008
title: "Registered Projects and Task Worktrees"
summary: "Register repositories and create isolated task worktrees with reviewed cleanup."
status: implemented
scope: ["registered projects and task worktrees"]
navigation: ["specs/README.md","specs/product-and-tasks/registered-projects-and-task-worktrees/matrix.md"]
documentation: ["docs/task-workflows.md","docs/standalone.md","docs/tool-permissions.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Registered Projects and Task Worktrees

Register repositories and create isolated task worktrees with reviewed cleanup.

## Behavior and invariants

- Registration verifies a server-side repository and full base commit. Admitted tasks receive distinct branches and worktrees, preserving the source checkout and dirty files.

## Acceptance criteria

### AC-001 — Reviewed worktree creation

Registration verifies a server-side repository and full base commit. Admitted tasks receive distinct branches and worktrees, preserving the source checkout and dirty files.

### AC-002 — Project environment suggestion

An administrator saves a revisioned project environment preference. It is a suggestion revalidated at task admission and never reuses a prior task's consent.

### AC-003 — Reviewed cleanup

Cleanup applies to the stopped task's reviewed worktree only. Path, branch, HEAD and cleanliness are rechecked, and a changed, locked or dirty worktree is preserved.

## Explicit exclusions

- Remote import and native editor handoff belong to ENV-001 and ENV-002. Completion does not automatically delete a worktree.

## Open decisions

- Early worktree evidence predates the delivered native profile. Additional operating systems and an actual editor handoff remain part of final native qualification.

## History

- 2026-10-19. FLOW-03c added registered projects and version-pinned worktrees. Later native increments added cleanup review and remembered environment suggestions.

## Reconciliation

- [task-workflows.md](../../../docs/task-workflows.md), [standalone.md](../../../docs/standalone.md), [tool-permissions.md](../../../docs/tool-permissions.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
