# Issue triggers, lifecycle synchronization and PR review routes

These connect external work to saved workflows. They are independent resources:
an issue trigger creates tasks, a lifecycle mapping translates provider states,
and a review route dispatches a workflow after a Weft-created PR.

## Configure an issue trigger

1. Prepare the issue integration, appropriate saved workflow, model for readiness
   and optional project/source-control delivery connection.
2. Open **Workflow → Settings → Triggers**
   (`#/g/<id>/settings/triggers`) and add an issue trigger as administrator.
3. Select source/connection, repository or project scope, selection label and
   optional query restrictions. Configure polling interval, entry/result agents,
   issue readiness and optional project/SCM/mapping bindings.
4. Review Ask/Workspace/Full execution consent and the exact workflow version.
   Save the trigger independently from the graph draft. Activation is not
   implied by merely editing its form.
5. Preview matching issues as operator. Preview does not claim or execute them.
   Inspect source permissions and readiness failures before enabling work.
6. Activate/resume with reviewed consent. Polling checks selected issues; source
   search indexing can delay their appearance. This is not instantaneous webhook
   delivery.

GitHub uses its supported MCP integration. Jira Cloud selects an Atlassian MCP
authority, with typed tracker operations using the explicitly configured
separate REST access when selected; MCP readiness is not a REST permission test.
Jira Data Center uses its native REST integration. Cloud lifecycle profiles pin
the connection revision/site/project rather than storing credentials themselves.
Discover/map actual transition IDs; never infer that every provider has an
**In Review** state.

## Automatic task journey

Polling selects eligible issues, assesses readiness, claims the issue through
the provider's supported lifecycle operations and records the created task.
The task then follows admission, environment, authorization and delivery rules.
Claim labels/comments and processed records prevent an ordinary poll from
creating the same work repeatedly. Processing is serialized within the trigger;
this is not a promise of unlimited parallel issue execution.

Unready issues, permission failures, uncertain external writes and changed
configuration can require attention or a durable hold. Inspect the recorded
reason before retrying. Pause prevents new intake; it is not rollback of work
already dispatched. Readiness/interpretation may consume model budget even
before an agent starts.

## Recover failed intake

1. Inspect trigger status, processed association, task state and issue labels/
   comments or transitions.
2. Stop the prior task when recovery requires preventing concurrent work and
   confirm its stored cancellation/execution state.
3. Use the confirmed `retry-failed` operation for that exact source key/task,
   or the documented hold-resolution path when the outcome is uncertain.
4. Restore the source's eligibility only when the recovery contract permits it.
   Retry is not permission to remove arbitrary issue history or bypass consent.
5. Follow the next poll and verify one new claim/task rather than assuming the
   UI refresh caused intake.

## Route a newly delivered PR to review

Review routes are currently configured through API/declarative YAML, rather
than the issue-trigger form. Prepare a reviewer workflow with compatible event
ingress and explicit SCM review grants. Preview/save the route for the selected
SCM connection and target workflow/version.

`scm.pull_request_opened` is emitted by Weft's managed PR delivery. This route is
not a generic webhook subscription to every PR created outside Weft. Dispatch
records a receipt tied to the delivered PR/revision. Inspect receipts and use
reconcile/retry only for the recorded failure/uncertain outcome.

The reviewer reads the exact PR evidence and can publish a summary/findings when
its grants and instructions permit. Merge is a separate capability with
provider-specific protection limits; enabling a route does not itself authorize
automatic merge. Jira lifecycle synchronization likewise needs explicit mapping,
credential and outcome receipts, not guessed column names.

## Implementation and verification map

- [TriggersSection](../../../web/src/components/TriggersSection.tsx) and
  [trigger UI tests](../../../web/src/components/TriggersSection.test.tsx).
- [Trigger engine](../../../server/internal/trigger),
  [review routes](../../../server/internal/reviewroute),
  [API operations/roles](../../../server/internal/api/server.go).
- [Task workflows](../../task-workflows.md),
  [Atlassian/Bitbucket setup](../../atlassian-bitbucket-setup.md),
  [declarative configuration](../../declarative-configuration.md),
  [portable review evidence](../../portable-review-evidence.md).

See [connections](../connections/README.md), [tasks](../tasks/README.md) and
[approvals](../approvals/README.md) for the handoffs this flow depends on.
