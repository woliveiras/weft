# Approvals and human decisions

**Work → Approvals** (`#/approvals`) brings pending requests and previous
outcomes into a table. Search, status, request-type and workflow filters narrow
the loaded scope. Newest requests appear first. Filtering a bounded recent
result is not a complete audit export; ordinary approval results and application
reviews have their respective API visibility rules.

## Review and decide

1. Open the inbox or follow a task/workflow link to its pending decision.
2. Find the request by title/ID, type, workflow, agent or reviewer. Inspect its
   status and creation time before acting; a historical approved row is not a
   current authorization request.
3. Open the request details. Review the actual tool/action, resource scope,
   arguments and execution context, rather than deciding from the title alone.
4. Approve or reject through the supported decision control. Operator/admin
   roles decide ordinary approvals; task-wide authorization and certain
   administrative operations have separate admin-only controls.
5. Wait for the response and inspect the recorded reviewer/time/outcome. Failure
   is not a successful decision, and a concurrently resolved request cannot be
   approved a second time as though it were still pending.
6. Return to the task/workflow. A decision permits the reviewed operation to
   proceed subject to current constraints; it does not prove that operation
   succeeded. Inspect subsequent execution/tool/delivery evidence.

For **human.ask** requests, supply the requested answer as well as any optional
decision comment. Environment and task-environment approvals specifically
require admin even though ordinary tool/message/turn decisions allow operators.

## Application candidate review

When application review is configured, its records appear in the same table for
operators/admins. Open the row to inspect the candidate revision, review
instructions, agent-reported checks and preview generation/session state.

1. With a ready pending session, **Open preview** opens the managed application.
   The browser must permit the popup and complete its access handshake.
2. Alternatively use the external environment (explicitly unverified), test the
   branch locally, or deliberately choose acceptance without manual execution.
   Record the method actually used; agent-reported checks are not independent
   test evidence.
3. **Application logs** helps diagnose a failed session. Pending failed/expired/
   stopped sessions can offer **Restart preview**; ready sessions can be
   extended. Access revocation is a separate explicit operation.
4. HTTP-client preview credentials can be explicitly generated when needed;
   handle them privately and separately from the application's authorization.
   They expire and are not durable integration tokens.
5. Choose the review method, enter feedback and **Approve candidate**, **Request
   changes** or **Reject**. The decision uses the review revision and records
   the reviewer/method/outcome. Inspect the next workflow step after deciding.

Preview/session availability and candidate approval are separate states. A
failed preview does not mean the candidate was rejected, and opening a preview
does not approve it.

## Different decisions in one task journey

| Decision | Where it belongs |
| --- | --- |
| Task authorization mode | Saved task's Settings & access/review, matching its revision |
| Environment preparation approval | Task environment proposal and build/setup scope |
| Tool call approval | Pending invocation with its actual arguments/constraints |
| Output/human review gate | Workflow's configured review boundary |
| PR creation approval | Managed source-control delivery request |

Full task access can avoid repeated prompts for already granted tools, but does
not remove mandatory gates. Rejecting/cancelling work does not undo completed
external operations. Stale reviews or changed task configuration require renewed
review rather than reuse of an unrelated historical approval.

Empty results should be interpreted with the selected filters and load state.
Refresh failures leave the need for a new successful read; repeatedly clicking
an action cannot supply permissions missing from the current identity.

## Implementation and verification map

- [ApprovalsView](../../../web/src/components/ApprovalsView.tsx) and
  [its tests](../../../web/src/components/ApprovalsView.test.tsx): unified inbox,
  filters, details and decision handling.
- [ReviewsView](../../../web/src/components/ReviewsView.tsx) and
  [its tests](../../../web/src/components/ReviewsView.test.tsx): application
  candidate review, preview sessions and decisions.
- [TaskDecisions](../../../web/src/components/TaskDecisions.tsx),
  [TaskAuthorization](../../../web/src/components/TaskAuthorization.tsx) and
  [TaskEnvironment](../../../web/src/components/TaskEnvironment.tsx): contextual
  task review surfaces.
- [API roles/routes](../../../server/internal/api/server.go),
  [authorization contract](../../task-authorization.md), [permissions](../../tool-permissions.md).
