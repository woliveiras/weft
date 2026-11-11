---
id: RUN-006
title: "Issue References and Polling Triggers"
summary: "Read reviewed issue references and start eligible labeled work through consent-bound polling."
status: implemented
scope: ["issue references and polling triggers"]
navigation: ["specs/README.md","specs/runtime-and-delivery/issue-references-and-polling-triggers/matrix.md"]
documentation: ["docs/task-workflows.md","docs/atlassian-bitbucket-setup.md","docs/tool-permissions.md"]
dependencies: ["TASK-006","TASK-007","INT-005","RUN-005"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Issue References and Polling Triggers

Read reviewed issue references and start eligible labeled work through consent-bound polling.

## Behavior and invariants

- Issue content reaches the task as data through the exact configured entry-agent tool grant. Saving or reviewing the reference performs no fetch.

## Acceptance criteria

### AC-001 — Reviewed issue reference

Issue content reaches the task as data through the exact configured entry-agent tool grant. Saving or reviewing the reference performs no fetch.

### AC-002 — Versioned polling and eligibility

Polling leases admit one eligible item per trigger, deduplicated by source, issue and workflow. Stale consent blocks execution; missed slots are coalesced. Preview and skipped reasons stay visible.
Create, edit and resume validate the authorization mode against the workflow's
effective isolation and grants using TASK-006. Incompatible modes produce an
`authorizationMode` field error. If stored consent is incompatible, the
runner pauses the trigger with the same reason before polling the tracker,
claiming an issue or creating a task draft.

### AC-003 — Gateway write-back

Claim and completion use reviewed tracker tools and report task/PR links, label and supported transition changes, or a visible failure that pauses the trigger for review.

## Explicit exclusions

- Authenticated Jira/GitHub webhooks are deferred. Live real-account qualification is PROD-14.

## Open decisions

- The local-first fixtures cover claim and completion. The documented live issue-to-PR scenario needs authorized external resources.

## History

- 2026-10-19. FLOW-04 added explicitly reviewed issue references.
- 2026-10-30. Phase C selected polling with existing scheduled loops and leases. TRIG-01–07 were recorded complete with fixtures by 2026-11-11.

## Reconciliation

- [task-workflows.md](../../../docs/task-workflows.md), [atlassian-bitbucket-setup.md](../../../docs/atlassian-bitbucket-setup.md), [tool-permissions.md](../../../docs/tool-permissions.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
