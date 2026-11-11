---
id: TASK-006
title: "Task Consent and Gateway Authorization"
summary: "Bind task authorization to reviewed scope and preserve gateway grants and mandatory approvals."
status: implemented
scope: ["task consent and gateway authorization"]
navigation: ["specs/README.md","specs/product-and-tasks/task-consent-and-gateway-authorization/matrix.md"]
documentation: ["docs/task-authorization.md","docs/tool-permissions.md","docs/security.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Task Consent and Gateway Authorization

Bind task authorization to reviewed scope and preserve gateway grants and mandatory approvals.

## Behavior and invariants

- Ask for approval, Workspace access and Full access apply within the effective grants and mandatory approvals. Workspace access denies native shell. Task consent is revision-fenced and binds the actor, workflow, policy and reviewed capability scope.

## Acceptance criteria

### AC-001 — Reviewed authorization modes

Ask for approval, Workspace access and Full access apply within the effective grants and mandatory approvals. Workspace access allows shell only in a Docker sandbox. Process execution, including the current Kubernetes mode in the weftd pod, is refused in Workspace. Ask requires approval for granted shell calls there. Task consent is revision-fenced and binds the actor, workflow, policy and reviewed capability scope.

### AC-002 — Live reduction and stale consent

An active task may reduce automatic authorization. Pending and future actions recheck the revision; expansion or stale consent is denied and decisions retain their identity.

### AC-003 — Decision visibility

The task and global inbox expose the same pending approval and terminal decision. Role gates apply to approval, rejection and task cancellation.

## Explicit exclusions

- Task consent confers no new grants. Cancellation does not roll back completed external effects.

## Open decisions

- Production defaults for newly created workflows and live credential qualification remain with PROD-07 and PROD-04.

## History

- 2026-10-19. AUTH-01 defined precedence, AUTH-02 added reviewed consent and AUTH-03 added suggestions and live reduction. Project suggestions remained distinct from consent.

## Reconciliation

- [task-authorization.md](../../../docs/task-authorization.md), [tool-permissions.md](../../../docs/tool-permissions.md), [security.md](../../../docs/security.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
