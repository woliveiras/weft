---
id: TASK-011
title: "Failed Task Retry"
summary: "Prepare a fresh linked draft from a failed manual task without replaying execution or consent."
status: implemented
scope: [manual task recovery, expired approval guidance]
risk_domains: [persistence, authorization, duplicate execution]
change_surfaces: [task API, task configuration, console]
dependencies: [TASK-005, TASK-006, TASK-007, TASK-009]
navigation: [specs/README.md, specs/product-and-tasks/failed-task-retry/matrix.md]
documentation: [docs/features/tasks/README.md]
authority:
  granted: [implementation, tests, subagent review, local stack update, staged commits]
  withheld: [starting a real retry, remote push, production deployment]
---

# Failed Task Retry

An operator recovering from a failed manual task can select **Try again** to
open a fresh, linked task draft. This is a new attempt, not continuation at the
failed tool call. The motivating case is a required approval that expired while
the operator was away.

## Acceptance criteria

### AC-001 — Fresh pinned draft

Only a persisted `failed` manual task is eligible. Copy its complete editable
input, including request, workflow ID/version, agent and channel bindings,
project/base commit, execution mode, issue source and SCM connection name.
Record `retryOfTaskId` and `retryOfRevision` as server-owned provenance.
Do not copy resolution, submission, authorization, approvals, execution copy,
launch errors or results. Saving or resolving the child preserves provenance.
Old workflow versions remain pinned; normal review rejects stale configuration
until the operator explicitly repairs it. Do not silently select newer content.

### AC-002 — Explicit, repeatable creation

`POST /api/v1/tasks/{tid}/retry` requires operator/admin access, strict
`{"confirm":true}` and a positive `If-Match` revision matching the failed source.
Return HTTP 201 for a created child or 200 for replay, with its ETag and task
Location. One source snapshot has at most one child: concurrent clicks, a lost response,
restart or replay return that same child, including edits or execution it has
subsequently acquired. A deleted child causes conflict and is never resurrected.
Invalid bodies, stale revisions, nonfailed sources and trigger-owned tasks are
rejected without mutation. Unknown sources remain not found. Storage and audit
failures are exposed; a committed but unacknowledged write remains replayable.

### AC-003 — Original and execution boundaries

Creation does not mutate, cancel, delete or redispatch the original. It does not
invoke a model, build an environment, create a workspace, grant access, push or
open a PR. The child goes through ordinary configuration review, fresh consent,
mandatory approvals, budget and execution admission. When started explicitly,
it owns a separate execution/workspace and costs. Existing failed-task history,
plans and costs remain available. Trigger recovery stays in its existing flow.
Audit retry intent with source, child and revision identifiers and actor, without
request bodies or credentials. No transaction or schema migration is required.

### AC-004 — Discoverable recovery and feedback

The failed manual-task header offers **Try again** to an operator/admin and
explains that it creates a draft with fresh approvals/workspace and leaves the
original intact. Readers cannot create retries. Pending progress must not be
presented as a confirmed failure. While creating, show loading and prevent
repeated local submissions; failures stay visible with a retry path. Navigate
to the returned child's task route; it may already have been edited or started
by another request. Show a link to the original on the saved draft and tracking
screen, retained after reload. Ordinary review does not start execution.

### AC-005 — Expired approval is actionable

Use recorded decision status `expired` to explain an expired approval on a
failed task as a recorded fact, without inferring it was the sole failure cause. State that the prior approval cannot be granted retroactively and
a new attempt needs its own approvals. Do not diagnose expiry from free text,
claim all failures were caused by expiry or suggest changing models/connections
for this specific case. Other failure diagnostics remain available. Use semantic
buttons/links, visible keyboard focus and announced asynchronous status/errors;
controls and copy wrap without clipping at narrow widths.

## Design and compatibility

Deduplicate by a deterministic server-generated child ID derived from source
identity/revision and a create-only storage CAS. Existing records have absent
optional provenance fields. The original stays unchanged even when an old
execution copy has been retained; draft creation alone has no runtime effect.
A child that fails is itself a new retry source. This is not an automatic retry
policy, an approval extension or a guarantee that external side effects can be
repeated safely: the next agent must inspect retained external state.

## Evidence and exclusions

The supplied screenshots show an expired `scm.security.alerts` approval, failed
task status and generic repair links. No usability measurements are asserted.
Live remediation runs, automatic retries, exact-step resume, retries of cancelled
or blocked tasks and retries of issue-trigger tasks are excluded.
