---
id: RUN-007
title: Issue Readiness Before Work
summary: Evaluate issues selected by polling triggers and request missing information before claiming or starting work.
status: implemented
scope: [GitHub and Jira polling triggers, issue clarification, trigger state, Activity]
risk: high
risk_domains: [prompt injection, authorization, external write-back, budget, replay, compatibility]
reversibility: Pause the trigger under RUN-006; review held issues before changing the readiness policy or resuming.
change_surfaces: [trigger configuration and runner, tracker adapters, trigger state, Activity, model gateway]
contracts: [polling eligibility, reviewed trigger consent, bounded pre-task evaluation, issue state write-back]
review_policy: The maintainer approved D-01 through D-09 on 2026-11-12. Qualify against fixtures.
navigation: [specs/README.md, specs/runtime-and-delivery/issue-readiness-before-work/matrix.md]
documentation: Update trigger setup, API, Activity and authorization docs with the approved behavior when delivered.
authority:
  granted: [specification, implementation, fixture verification, local commits]
  withheld: [unreviewed grants, live tracker and provider calls, push, deployment, release, live qualification]
dependencies: [RUN-006, RUN-005, TASK-006, TASK-004, INT-004, REPO-GUIDANCE]
---

# Issue Readiness Before Work

Give polling triggers a way to stop an underspecified issue before starting
coding work. Ask for the information needed to proceed and make the hold
visible to the operator until the issue can be evaluated again.

## Behavior before this change

[RUN-006](../issue-references-and-polling-triggers/spec.md) selects an open,
labeled, unblocked issue with an eligible assignee and no prior admission for
that issue and workflow. The runner checks consent before polling, leases
each trigger, and claims the candidate before creating a task. Claim uses
`weft:in-progress`. Completion uses `weft:review` on success and
`weft:failed` on start failure, task failure or cancellation. Jira uses
the *To Do* status category for eligibility and attempts the existing
*In Progress* and *In Review* transitions when offered.
[Trigger API and lifecycle](../../../docs/api.md#issue-triggers).

The trigger's optional `owner` is a tracker account used for assignment and
eligibility. It can be a bot and is distinct from the administrator who
consented to the trigger. The current tracker adapter searches issues and
writes comments and state through the gateway. It does not read clarification
threads. [Tracker adapter](../../../server/internal/trigger/issue.go).

## Behavior and invariants

- For an enabled trigger, evaluate the selected eligible issue after the
  consent check and before claim. The evaluation uses a bounded snapshot of
  the issue and any eligible replies. The decision applies to that snapshot
  and reviewed trigger configuration. If either changes before claim, repeat
  the applicable checks. A ready issue proceeds through ordinary admission.
- Missing, materially conflicting or ambiguous information holds the issue.
  Ask one or more concrete questions about the unmet conditions. The
  workflow posts the questions on the issue and replaces its selection label
  with `weft:needs-attention`.
- An evaluating or waiting issue has no task draft, task run, assignment
  change by Weft, workspace, sandbox, execution budget reservation or
  processed admission. Holds consume no active task slot. At ten holds per
  trigger, skip new issues and report the cap while still checking for
  eligible signals on held issues.
- A reply is new, substantive information tied to an outstanding question
  and received after that round opened. Use the tracker identity of the
  issue author/reporter or configured human trigger owner. Ignore Weft
  comments, bots, pre-round comments, duplicates, acknowledgements and
  claimed identities in text. A verified human maintainer adding
  `weft:todo` by hand requests reevaluation of the current issue, including
  for a trigger that uses a custom selection label.
- Resume through polling with current consent, eligibility and a fresh
  verdict. Find held issues by their saved tracker identity even after
  removal from search. A verified return signal restores eligibility with
  respect to the selection label. It cannot bypass closure, blocking,
  reassignment, the trigger scope or prior admission. Once a hold is
  resolved and claimed, retire its waiting marker and any requeue label.
- Issue bodies, comments and replies are untrusted data. They can describe
  desired behavior within the existing scope. They confer no tool grant,
  approval, credential, repository access or change of trigger consent.
  The evaluator has no execution tools. Trusted trigger code performs only
  reviewed tracker operations against the configured issue through the
  gateway. A model verdict cannot supply a tool name, destination
  or argument that changes authorization. Questions are bounded and limited
  to clarifying the issue. They must not quote sensitive payloads, ask for
  credentials, or forward directives embedded in tracker text.
- Persist the hold, reason, round and write-back outcome so a restart or
  another poll can recover. Distinguish incomplete information from an
  unavailable evaluation, consent block or failed notification. Timeouts,
  malformed output, exhausted allowance and tracker or store failures leave
  the issue unadmitted. Retry only within the reviewed bounds. Reconcile
  confirmed comment and state changes before retrying. If delivery is
  uncertain, surface it for reconciliation and keep the issue held until the
  publication outcome is resolved.

## Acceptance criteria

### AC-001 — Readiness before claim

An eligible issue proceeds through ordinary claim and admission only after a
current ready verdict. A clear objective and verifiable acceptance are
required. Scope must be delimited when ambiguity is material. Each unmet
condition blocks claim and identifies the missing information. Compare the
evaluated snapshot with current issue content and eligible replies before
claim. An edited issue cannot start on a verdict for an older snapshot. The
bounded evaluated content is available to the admitted task as
untrusted context under the reviewed input scope.

### AC-002 — Clarification hold and capacity

Weft durably records a hold, posts concrete questions as a comment on the
issue and replaces the selection label with `weft:needs-attention`. Preserve
unrelated labels. Unchanged input and repeated polling produce one request
for the same round. A hold creates no task, assignment change, workspace,
sandbox, execution budget reservation or processed admission. Other issues
can be evaluated and admitted while holds exist. At ten held issues the
trigger stops evaluating new ones and exposes the capacity reason.

### AC-003 — Response and reevaluation

A fresh substantive comment by the issue author or configured human owner,
or a verified manual addition of `weft:todo` by a human maintainer, schedules
a fresh evaluation. Signals for the same round and content share one
reevaluation. Ineligible actors, bots, stale or duplicate replies and
unrelated label edits leave the hold intact. A partial reply can leave
questions outstanding. After two rounds of questions, a still-incomplete
issue stays in `weft:needs-attention` with a comment requesting operator
intervention. Reopening always requires current eligibility, readiness and
consent.

### AC-004 — Operator visibility and recovery

Activity and the trigger state show the same held issue, its tracker identity
and link where available, the reason, outstanding questions or their
location, the current round and any publication error or next action.
Ordinary waiting and an evaluation or write-back failure are distinguishable
after a reload or restart. Access follows the existing console read roles.
The views and audit expose bounded diagnostics without copying raw tracker
payloads or secrets. Structured logs and evaluation outcome and latency
metrics use bounded categories and carry no issue content. Partial tracker
writes or failed persistence cannot report successful publication or allow
admission.

### AC-005 — Consent and evaluation limits

Readiness uses only the model with a separate fixed installation allowance.
Each evaluation makes at most one model call with up to 16 KiB of issue and
reply input, up to 512 output tokens, a 10-second deadline and a monetary
check before the call. If a limit prevents a reliable verdict, hold the
issue and show which limit applied. The readiness policy, tracker reply
reads and comments, and limits are part of the reviewed trigger consent.
Changes invalidate consent through the existing check. The runner rejects or pauses for an out-of-scope policy or stale consent before
the affected tracker or provider call and checks again before claim after a
wait. Injected issue text or replies, including claims of approval, cause no
authorization change, tool dispatch, shell, file or SCM activity. The selected
Ask, Workspace or Full mode governs admitted work under TASK-006. RUN-005
continues to govern delivery.

### AC-006 — Tracker and lifecycle compatibility

Readiness is enabled by default for all triggers and can be disabled per
trigger. Disabling keeps outstanding holds stopped until explicit
resolution. Issues without a hold follow RUN-006 while readiness is disabled.
Preview is read-only and can display saved readiness state. GitHub and Jira
both use an issue comment and the same label cycle owned by RUN-006. Jira
status transitions are excluded. Prior trigger formats have no compatibility
contract in the prototype.

## Maintainer decisions

The maintainer approved these decisions on 2026-11-12. They are the
spec-derived oracles for implementation.

- **D-01 — Evaluator and allowance (AC-001, AC-004, AC-005).** Use the model
  alone. Make one call per evaluation with up to 16 KiB of issue and reply
  input, 512 output tokens, 10 seconds and a fixed installation monetary
  limit checked before dispatch under INT-004. Hold with the relevant reason
  when a bound prevents a reliable verdict.
- **D-02 — Channel (AC-002–AC-004).** Post questions as comments on the
  issue and read answers from the same issue.
- **D-03 — Return and rounds (AC-003, AC-004).** Reevaluate after a comment
  from the human issue author or configured human owner, or a human
  maintainer's manual `weft:todo` addition. Bots do not count. Ask at most
  twice. Use the tracker API through reviewed gateway operations to verify
  label event authorship and maintainer authority. If still incomplete, keep `weft:needs-attention` and ask for operator
  intervention in a comment. Reopening repeats readiness and consent.
- **D-04 — Default and disable (AC-005, AC-006).** Enable readiness by
  default on every trigger with a per-trigger switch. Disabling preserves
  holds until explicitly resolved. There is no legacy-trigger compatibility.
- **D-05 — Jira (AC-002, AC-003, AC-006).** Use comments and labels. Exclude
  status transitions.
- **D-06 — Consent (AC-005).** Bind readiness policy, reply reads, comments
  and fixed limits to current trigger consent. Include the specific tracker
  API operations and connection scope in that review. Changing the scope invalidates
  consent. Issue replies confer no consent or approval.
- **D-07 — Hold label (AC-002, AC-003, AC-006).** Replace the selection label
  with `weft:needs-attention` to remove the issue from the queue. Keep
  unrelated labels. Use specific gateway operations to add and remove labels
  without replacing the full list.
- **D-08 — Rubric (AC-001, AC-002).** Require a clear objective and
  verifiable acceptance. Require bounded scope when ambiguity is material.
  Use the conditions and example questions below to guide clarification.
- **D-09 — Capacity (AC-002, AC-006).** A hold reserves no trigger slot.
  Continue assessing and admitting other eligible issues until the trigger
  has ten holds. At the cap, stop assessing new issues and surface the
  capacity reason.

| Condition | Sufficient when | Example question if missing |
| --- | --- | --- |
| Clear objective | The affected behavior or area and the desired outcome are identifiable. A defect describes the observed and expected outcomes. | What should happen after the user saves the form, and what happens today? |
| Verifiable acceptance | At least one observable condition distinguishes success from failure. A reproducible example, expected result, or check is sufficient. | Which input should we use to verify the fix, and what result should it produce? |
| Bounded scope | The requested change identifies its target and resolves material ambiguity about which behavior or surfaces may change. Explicit exclusions are needed only where they settle such an ambiguity. | Should the change apply to imports as well as entries created in the console? |

## Explicit exclusions

- Manually created tasks, direct issue references and other non-polling
  entry points. Authenticated tracker webhooks remain deferred under RUN-006.
- Repository checkout, discovery of repository guidance, environment
  preparation, execution, code verification and SCM delivery during
  readiness. Repository guidance remains governed by REPO-GUIDANCE after
  admission. The evaluator also leaves linked pages and attachments unopened.
- Inferring or rewriting requirements, expanding scope or authorization,
  accepting clarification as approval, and treating readiness as proof that
  the work is feasible or will pass its checks.
- Live tracker or model qualification, deployment and release.
- Jira status transitions and console clarification or approval as a
  substitute for issue replies.

## Reconciliation

- [Behavior and oracle matrix](./matrix.md) and [verification evidence](./evidence.md).
  Qualify tracker and console paths against local fixtures, the real
  gateway and durable state.
- Reconcile [trigger API](../../../docs/api.md#issue-triggers),
  [operator setup](../../../docs/getting-started.md), Activity and
  [task authorization](../../../docs/task-authorization.md) on delivery.
