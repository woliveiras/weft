---
id: RUN-007
title: Issue Readiness Before Work
summary: Evaluate issues selected by polling triggers and request missing information before claiming or starting work.
status: planned
scope: [GitHub and Jira polling triggers, issue clarification, trigger state, Activity]
risk: high
risk_domains: [prompt injection, authorization, external write-back, budget, replay, compatibility]
reversibility: Pause the trigger under RUN-006; review held issues before changing the readiness policy or resuming.
change_surfaces: [trigger configuration and runner, tracker adapters, trigger state, Activity, conditional Approvals and model gateway]
contracts: [polling eligibility, reviewed trigger consent, bounded pre-task evaluation, issue state write-back]
review_policy: Maintainer review of the spec, open decisions and matrix before tests or implementation.
navigation: [specs/README.md, specs/runtime-and-delivery/issue-readiness-before-work/matrix.md]
documentation: Update trigger setup, API, Activity and authorization docs with the approved behavior when delivered.
authority:
  granted: [draft specification, matrix, index, local commit]
  withheld: [implementation, new grants, external writes, push, deployment, release, live qualification]
dependencies: [RUN-006, RUN-005, TASK-006, TASK-004, INT-004, REPO-GUIDANCE]
---

# Issue Readiness Before Work

Give polling triggers a way to stop an underspecified issue before starting
coding work. Ask for the information needed to proceed and make the hold
visible to the operator until the issue can be evaluated again.

## Current behavior

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
  requested tracker workflow posts the questions on the issue and marks its
  waiting state (for example, `weft:needs-info`). D-02 records alternatives
  for review, including one that would change the requested comment behavior.
- While evaluating or waiting, the issue has no task draft, task run,
  assignment change by Weft, worktree, sandbox or task budget reservation.
  The evaluation alone may use its reviewed, bounded allowance. A hold
  does not mark the issue as processed. D-09 leaves trigger slot accounting
  and polling of other eligible issues open.
- A reply is new, substantive information tied to an outstanding question
  and received after that round opened. The tracker or console supplies the
  actor identity. Candidate responders are the issue author/reporter, the
  trigger's configured human tracker owner, or an explicitly authorized
  console responder where applicable. D-02 and D-03 select the allowed set.
  Weft's own comments, bot echoes, pre-round comments, duplicate events,
  acknowledgements and text claiming to speak for someone else do not count.
  A partial reply can trigger evaluation and leave questions outstanding.
  If enabled, a manual label signal requests reevaluation of the current
  issue content. Removing a label alone supplies no missing information.
- Resume through polling with current consent and current eligibility.
  A held issue remains discoverable by its saved tracker identity, even if
  its waiting label or status removes it from the ordinary search. It cannot
  start after closure, blocking, reassignment outside the trigger scope,
  loss of the required selection signal or prior admission. D-05 and D-07
  must define which signal restores ordinary eligibility after a state change.
- Issue bodies, comments and replies are untrusted data. They can describe
  desired behavior within the existing scope. They confer no tool grant,
  approval, credential, repository access or change of trigger consent.
  The evaluator has no execution tools. Trusted trigger code performs only
  reviewed tracker operations against the configured issue through the
  gateway. A model verdict, if used, cannot supply a tool name, destination
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

An eligible issue that satisfies the rubric selected in D-08 proceeds through
ordinary claim and admission. Each unmet requirement of that rubric, or
material ambiguity that prevents checking it, produces an explicit reason
and blocks claim.
An edited issue cannot start on a verdict for an older snapshot. The content
that justified a ready verdict is available to the admitted task as bounded,
untrusted context under the reviewed input scope.

### AC-002 — Clarification hold

For an incomplete issue, Weft records the hold and publishes specific
questions and the waiting state through the selected channel and tracker
mapping. In the tracker workflow the issue shows the comment and the status
label or Jira waiting state. The wait creates no task, assignment change,
worktree, sandbox, execution budget reservation or processed admission.
Unchanged input and repeated polling produce one request for the same round.
D-09 governs slot occupancy and whether the trigger can advance other issues
during a hold.

### AC-003 — Response and reevaluation

A new eligible response or authorized requeue signal schedules a fresh
evaluation. Signals for the same round and content share one reevaluation.
A partial answer can leave the issue held with updated questions.
Ineligible actors, stale, empty or duplicate replies and irrelevant label
changes leave the hold intact. A signal never directly starts a task. Current
eligibility, readiness and consent are required again. When the selected
round limit is reached, the issue remains held and the console identifies
the permitted next action.

### AC-004 — Operator visibility and recovery

Activity and the trigger state show the same held issue, its tracker identity
and link where available, the reason, outstanding questions or their
location, the current round and any publication error or next action.
Ordinary waiting and an evaluation or write-back failure are distinguishable
after a reload or restart. Access follows the existing console read roles.
The views and audit expose bounded diagnostics without copying raw tracker
payloads or secrets. Partial tracker writes or failed persistence cannot
report successful publication or allow admission.

### AC-005 — Consent and evaluation limits

Evaluation consumes only its own bounded allowance. When model inference is
selected it uses reviewed provider and budget policy under INT-004. The
runner rejects or pauses for an out-of-scope policy or stale consent before
the affected tracker or provider call and checks again before claim after a
wait. Injected issue text or replies, including claims of approval, cause no
authorization change, tool dispatch, shell, file or SCM activity. The selected
Ask, Workspace or Full mode governs admitted work under TASK-006. RUN-005
continues to govern delivery.

### AC-006 — Tracker and lifecycle compatibility

The waiting state is distinct from claim, review and failure. A missing
answer or failed evaluation never produces `weft:in-progress`,
`weft:review` or `weft:failed`. Ordinary claim retires the waiting marker
when the issue becomes admissible. Existing labels, assignment, completion,
lease and deduplication semantics apply from claim onward. A missing or
denied Jira waiting transition has an explicit outcome under D-05. Issues
without a hold follow RUN-006 when readiness is disabled. Already admitted
tasks continue on the RUN-006 path. Preview stays read-only and can show
recorded readiness without running a new evaluation.

## Open decisions

No option below is selected. The matrix marks policy-dependent cases for
completion during maintainer review.

### D-01 — Evaluator and allowance (AC-001, AC-004, AC-005)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Deterministic rules | Predictable and repeatable, with zero inference spend. | Structural checks can accept vague prose or reject good issues that use an unfamiliar format. |
| Model | Can interpret prose and conflicting evidence and generate focused questions. | Variable verdicts, latency and inference spend. Sending issue content to the provider requires reviewed scope. |
| Rules plus model | Cheap checks can handle clear cases and reserve inference for ambiguous ones. | Two paths need a clear rule for disagreement and independent qualification. |

Decide ceilings for tracker content and reply count, wall time, attempts and,
if used, model tokens and money. One starting point for review is 16 KiB of
input, 10 seconds and one model call per evaluation, with a 512-token output
cap and a reviewed monetary cap checked before dispatch. A fixed installation
ceiling is predictable. Per-trigger limits under an installation ceiling
accommodate different issue formats at the cost of more settings and review.
If a bound prevents a reliable verdict, report that outcome and hold the
issue. A deterministic path still needs input and time limits.

### D-02 — Question and response channel (AC-002, AC-003, AC-004)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Issue comment | The issue author can answer where the work was requested. Meets the requested tracker workflow. | Requires reviewed tracker writes and reply reads, and questions share the issue's audience. |
| Console Approvals | Keeps questions within console access and can use authenticated console identities. | Authors may lack console access. Requires an informational request distinct from execution approval. Console-only questions would require explicit approval to change the requested tracker-comment behavior. |
| Per-trigger channel | Supports different tracker audiences and console access policies. | Adds setup and cross-channel deduplication if both are allowed. Decide whether console-selected triggers also post questions on the issue or only publish the waiting marker. |

### D-03 — Requeue signal, responder and round limit (AC-003, AC-004)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Comment from issue author/reporter, configured human tracker owner, or either | Low friction and a reply linked to the questions. Author-only keeps the request with its originator. Owner-only gives the designated maintainer control. Either reduces delays. | Requires reviewed comment reads and verified actor identities. A bot or unset owner cannot serve as the sole human responder. |
| Manual label change by a verified maintainer | Works with description edits and gives maintainers explicit control. | Adds a manual step and requires reliable actor attribution for the change. A label snapshot alone cannot establish who changed it. |
| Both tracker signals, or authenticated response when using the console | Supports each selected channel and edited issue descriptions. | Requires deduplication when more than one signal arrives for the same content, and a console role choice. |

For console replies, decide between Operators and Admins under the existing
ordinary-decision roles, or Admins only for tighter control. Tracker responder
identity carries no console authority. Choose a fixed round cap (for example,
two clarification requests before operator intervention) or a per-trigger cap
within an installation maximum. A fixed cap is simpler to explain. A
configurable cap accommodates different teams with more policy to audit.
Decide who can reopen a held issue for another round and what reopening
requires. Reopening always runs the same readiness and consent checks.

### D-04 — Opt-in and defaults (AC-005, AC-006)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Off until enabled per trigger for both new and existing triggers | Avoids surprise writes or spend. | New triggers can still start work from incomplete issues unless configured. |
| Off for existing triggers, on for new triggers after review | Preserves existing setups and covers newly configured triggers. | Defaults differ by trigger age. New setup needs a compatible tracker and budget policy. |
| Explicit choice for new triggers, existing triggers keep current behavior until reviewed | Makes the scope and cost a deliberate part of setup. | Adds a choice to each new trigger and leaves existing coverage to rollout. |

Decide whether disabling with outstanding holds is blocked until they are
resolved, or is allowed while preserving those holds for explicit resolution.
The first avoids mixed policy within one trigger. The second lets operators
stop new evaluations promptly and requires a way to reconcile held issues.

### D-05 — Jira waiting and return transitions (AC-002, AC-003, AC-006)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Comment and waiting label, with a configured waiting transition when offered | Retains a visible marker across different Jira workflows. | Needs an explicit fallback outcome if the transition is unavailable. A waiting status outside *To Do* needs a defined return path. |
| Comment and required mapped waiting/return transitions | Matches the Jira board lifecycle and detects invalid mapping during setup or use. | Requires project-specific mappings and holds on a missing or denied transition. |
| Comment and waiting label in *To Do*, with transition support deferred | Keeps polling eligibility simple. | Jira boards show no separate waiting status. This narrows the requested transition behavior and needs maintainer approval. |

Choose whether an authorized manual transition or the trigger's reviewed
return transition restores *To Do*. A transition never overrides the other
eligibility checks. The existing *In Progress* and *In Review* transitions
remain part of the admitted lifecycle.

### D-06 — Ask, Workspace, Full and trigger consent (AC-005)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Include readiness policy, operations and limits in the existing reviewed trigger consent | One consent review and clear invalidation when the effective scope changes. | Readiness policy edits can require a new review of the whole trigger. |
| Separately review readiness scope and bind its revision to trigger consent | Makes tracker reply access, outbound comments and model exposure explicit and independently revocable. | More review state and a need to validate both scopes before their respective actions. |

For writes that are permitted but still require human approval, decide whether
to wait for the existing approval in the console or require a compatible
reviewed policy at enablement and leave incompatible triggers blocked. The
first permits per-write review and adds latency. The second fits unattended
polling but limits compatible setups. Both preserve mandatory gateway
approvals. Ask continues to gate applicable execution calls after admission.
Workspace continues to reject native shell and allow granted shell only in
Docker isolation. Full stays within reviewed grants and mandatory gates.
Replies and console clarification do not serve as consent or approval.

### D-07 — Waiting label and existing lifecycle (AC-002, AC-003, AC-006)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Add a waiting label alongside the selection label | Issue remains findable in the normal label search. | The selector must explicitly exclude held issues until an eligible signal, even if the selection label stays present. |
| Replace the selection label with the waiting label | The tracker clearly separates waiting issues from the queue. | Requires lookup of held issues and reviewed restoration of the selection signal before claim. |

Decide the marker name and who may restore the selection signal. Under
either option, replacing or clearing the waiting marker must avoid removing
unrelated labels. A manual label change must not bypass readiness or restart
an already admitted issue. The meanings of `weft:in-progress`, `weft:review`
and `weft:failed` stay with RUN-006.

### D-08 — Minimum readiness rubric (AC-001, AC-002)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Require all three candidate conditions | Establishes a consistent threshold across triggers. | Can hold a small issue whose scope is already apparent from its objective. |
| Require a clear objective and verifiable acceptance, with scope required when materially ambiguous | Covers short, actionable issues while prompting about consequential scope gaps. | Requires a reviewed rule for what makes scope ambiguity material. |
| Use a reviewed rubric per trigger | Lets workflows set criteria suited to their issue types. | Adds configuration and consent review. Requires decisions about which installation-wide conditions, if any, are mandatory. |

Candidate conditions for review. Decide which are required and what counts as
sufficient evidence in ordinary prose or eligible replies.

| Condition | Sufficient when | Example question if missing |
| --- | --- | --- |
| Clear objective | The affected behavior or area and the desired outcome are identifiable. A defect describes the observed and expected outcomes. | What should happen after the user saves the form, and what happens today? |
| Verifiable acceptance | At least one observable condition distinguishes success from failure. A reproducible example, expected result, or check is sufficient. | Which input should we use to verify the fix, and what result should it produce? |
| Bounded scope | The requested change identifies its target and resolves material ambiguity about which behavior or surfaces may change. Explicit exclusions are needed only where they settle such an ambiguity. | Should the change apply to imports as well as entries created in the console? |

### D-09 — Trigger capacity and polling while an issue is held (AC-002, AC-006)

#### D-09.1 — Does a hold reserve the trigger slot?

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Reserve no active-task slot for a held issue | Keeps the active-task slot available for an admitted task. | Requires a separate bound on outstanding holds if the trigger can continue polling. |
| Reserve the trigger's one slot until the hold resolves | Keeps one issue in flight per trigger, including pre-task waiting. | A slow reply can leave the trigger idle. Slot accounting must represent a hold with no task resources. |

#### D-09.2 — What happens to other eligible issues?

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Continue evaluating other eligible issues during a hold and admit ready work when slot policy permits | Allows independent issues to progress. | Requires ordering, separate hold limits and a policy for prioritizing newly clarified issues. |
| Wait for the hold to resolve before advancing the queue | Preserves oldest-first processing across holds. | An unresolved issue delays later candidates. |

Review the two choices together. If a hold reserves the slot and polling
continues, decide whether evaluation can proceed without claim while the
slot is occupied.

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
- Implementation, migrations, live tracker or model qualification,
  deployment and release in this review increment.

## Reconciliation

- [Behavior and oracle matrix](./matrix.md). Record approved options and
  update conditional oracles before writing tests. Then review fail-first
  unit and integration evidence before implementation. Qualify tracker and
  console paths against local fixtures, the real gateway and durable state.
- Reconcile [trigger API](../../../docs/api.md#issue-triggers),
  [operator setup](../../../docs/getting-started.md), Activity and
  [task authorization](../../../docs/task-authorization.md) on delivery.
