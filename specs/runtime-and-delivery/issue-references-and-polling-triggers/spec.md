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
  granted: [specification, implementation, fixture verification, local commits]
  withheld: [live tracker writes, push, deployment, production qualification]
---

# Issue References and Polling Triggers

Read reviewed issue references and start eligible labeled work through consent-bound polling.

## Behavior and invariants

- Issue content reaches the task as data through the exact configured entry-agent tool grant. Saving or reviewing the reference performs no fetch.

- New polling work uses the RUN-007 readiness policy before claim by default.
- Each relabel operation changes only the selection or lifecycle labels it
  owns. Use specific reviewed gateway operations to preserve unrelated
  labels, including concurrent additions.
- The 2026-11-12 maintainer decision supersedes the previous selection
  default, lifecycle labels and Jira transitions across all trackers.
  Prior trigger formats and labels have no prototype compatibility contract.

## Acceptance criteria

### AC-001 — Reviewed issue reference

Issue content reaches the task as data through the exact configured entry-agent tool grant. Saving or reviewing the reference performs no fetch.

### AC-002 — Versioned polling and eligibility

Polling leases admit one eligible item per trigger, deduplicated by source, issue and workflow. Stale consent blocks execution; missed slots are coalesced. Preview and skipped reasons stay visible. The selection label defaults to
`weft:todo` and can be configured per trigger.
Create, edit and resume validate the authorization mode against the workflow's
effective isolation and grants using TASK-006. Incompatible modes produce an
`authorizationMode` field error. If stored consent is incompatible, the
runner pauses the trigger with the same reason before polling the tracker,
claiming an issue or creating a task draft.
An edit referencing a removed workflow, entry agent or explicitly selected
result agent returns HTTP 422 with the corresponding field error and preserves
the saved trigger and consent.

### AC-003 — Gateway write-back

Claim and completion use reviewed tracker tools and report task or PR links.
Claim replaces the selection or hold label with `weft:wip`. Failed start,
failed work, cancellation or missing required delivery uses
`weft:needs-attention` with a failure comment explaining the next action
and pauses the trigger for review. Readiness uses the same label with a
comment distinguishing a question from a failure. Successful Weft delivery
with an open PR replaces `weft:wip` with `weft:done` while human review is
pending.

### AC-004 — Issue closure on merge

For a GitHub issue, the PR created by Weft includes the GitHub closing
reference in its body so that the merge closes the issue. Use `Closes #N`
when the PR and issue share a repository, and `Closes owner/repo#N` when
they differ. Derive the repository and issue number from the admitted
tracker identity. Auto-closing Jira
issues and other non-GitHub trackers on PR merge is excluded.

## Explicit exclusions

- Jira status transitions at any stage of the trigger lifecycle are excluded.
- Authenticated Jira/GitHub webhooks are deferred. Live real-account qualification is PROD-14.

## Open decisions

- The local-first fixtures cover claim and completion. The documented live issue-to-PR scenario needs authorized external resources.

## History

- 2026-11-12. The maintainer approved the RUN-007 readiness policy and the
  `weft:todo`, `weft:wip`, `weft:needs-attention` and `weft:done` cycle.

- 2026-10-19. FLOW-04 added explicitly reviewed issue references.
- 2026-10-30. Phase C selected polling with existing scheduled loops and leases. TRIG-01–07 were recorded complete with fixtures by 2026-11-11.

## Reconciliation

- [task-workflows.md](../../../docs/task-workflows.md), [atlassian-bitbucket-setup.md](../../../docs/atlassian-bitbucket-setup.md), [tool-permissions.md](../../../docs/tool-permissions.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
