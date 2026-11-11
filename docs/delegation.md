# Delegation Profiles and Policy

Native and external agents can request a temporary native analysis execution through the tool gateway.
Grant the exact `delegation.request` tool explicitly; wildcard grants do not enable
it. Existing gateway constraints and approval rules still apply. External parents
use their active execution callback; standalone gateway calls cannot delegate.

```json
{"template":"analysis","objective":"Review the supplied design and return concerns"}
```

Only these two fields are accepted. The selected installation profile uses the
parent's resolved provider/model. The default analysis profile has no tools or
workspace access and uses one inference request. Profiles can select an explicit
subset of parent capabilities as described below. No profile gets durable memory,
parent conversation history or message emission. Recursive delegation requires
an inherited delegation grant and an increased installation depth limit. Children
never edit the permanent graph.

## Installation profiles and policy

Administrators manage **Administration → Delegation**. Viewers and operators can
read settings but cannot modify them. Saves require review in the console and
compare-and-set revisions in storage; conflicts never retry silently.

Profiles contain an ID, name, guidance, enabled flag, output allowance and optional
`tools` capability names. The
built-in `analysis` profile is available at revision zero until customized.
Custom profiles are selected through the tool's `template` field. Mention the
profile ID in the parent's instructions when assigning a specialization. Guidance
cannot add tools, model overrides, credentials, runtimes or workspace access.
The effective output allowance is the minimum of policy, profile and parent limits.

Policy exposes enabled, approvalMode (`required` or `automatic`) and the limits
below. Defaults preserve the previous mandatory-approval behavior. Disable
profiles rather than deleting them; historical execution snapshots remain intact.

| API | Minimum role | Contract |
|---|---|---|
| GET /api/v1/delegation-policy | viewer | Policy plus revision and updating actor. |
| PUT /api/v1/delegation-policy | admin | Full policy; If-Match required, including 0 for defaults. |
| GET /api/v1/delegation-profiles | viewer | Built-in and custom profiles, including disabled profiles. |
| POST /api/v1/delegation-profiles | admin | Create a custom profile; duplicate IDs conflict. |
| GET /api/v1/delegation-profiles/{id} | viewer | Profile and revision. |
| PUT /api/v1/delegation-profiles/{id} | admin | Full profile, matching ID, required If-Match. |

Updates do not mutate running children. Each child stores the policy and profile
snapshot with their revisions and objective in its protected execution record.
A waiting request checks revisions and enabled flags again before inference;
changed configuration produces an error and requires a new request/decision.
Already-started work retains its snapshot; disabling is not a kill operation.
Capacity admission counts existing children, even after the limit is reduced.
This works through shared storage leases across service instances; filesystem
storage retains its existing single-instance restriction.

## Approval and result

In required mode, each child waits for an operator/admin decision in Approvals.
Automatic mode skips this delegation decision only; explicit grants and any
configured generic tool approval remain mandatory. The request shows
its objective, parent/child IDs, model, effective capabilities and their inherited
constraints, and deadline.
OAuth consent and existing tool grants cannot approve delegation automatically.
An additional generic tool approval applies if configured on the tool grant.

After approval, the server checks the deadline and capacity ownership again.
The parent waits for a structured tool result containing childExecutionId,
parentExecutionId, status, result and usage, then continues its native turn.
Rejection, expiration, provider failure, missing/truncated/oversized results and
ungranted/failed child tool calls return errors to the parent. No partial result is
reported as success. Provider diagnostic payloads are not returned.

Children are persisted as executions with parentExecutionId, delegationTemplate,
delegationModel and approvalId. Runs labels them and provides an Execution tree tab. Objectives are in protected
approval/execution records and results in execution
records, not general delegation telemetry.
`delegation.requested`, `delegation.started` and `delegation.completed` events
carry parent ID, template and status with normal execution/trace correlation.

## Inherited capabilities

An administrator may set **Inherited capabilities** in a profile (up to 32 exact
names). Each must match an explicit parent grant. All matching declarations are
copied with their paths, commands, hosts, per-call approval and rate limits;
wildcards are neither accepted in profiles nor expanded from parent grants.
Missing grants reject admission before child creation. Guidance cannot grant
capabilities. MCP names remain exact per-tool connection names.

Calls pass through the existing gateway with the child's execution ID and the
parent's graph/node/replica identity. They retain workspace read-only restrictions,
installation policy, sandbox, connection quotas and shared agent/tool rate limits.
The workspace is the parent's workspace, not a new isolated worktree. Tool grants
are not a promise of independent filesystem isolation. Every child call is recorded
against the child; its attempts also consume the parent's remaining tool allowance.

The child loop permits at most 16 calls (or the smaller remaining parent limit),
uses a cumulative output allowance and reserves every additional model round in
the root tree ledger. Empty, failed, truncated or oversized tool results stop the
child rather than becoming a successful partial result. Tool-free profiles retain
their single-call behavior. `environment.status`, `environment.propose` and `review.request` cannot be
inherited: durable environment/review continuations need separate lifecycle support.
Platform memory and emit operations are never available to children.

## Bounded limits and recovery

| Setting | Default | Allowed range |
|---|---:|---:|
| maxDepth | 1 | 1–4 (root is depth zero) |
| maxChildrenPerExecution | 3 | 1–32 |
| maxConcurrentChildren | 4 | 1–32 |
| maxDurationSeconds | 120 | 1–600 |
| maxOutputTokens | 1024 | 1–4096 |
| maxObjectiveBytes | 4096 | 1–16384 |
| maxResultBytes | 16384 | 1–65536 |

The profile output allowance is also 1–4096 tokens. Durations include approval
and respect the shorter parent deadline. Failed/rejected admitted requests count
against the child count. Capacity includes approval waits and rejects exhausted
requests without inference. Children cannot override installation limits.
No automatic retry or durable continuation is provided.

Parent cancellation propagates through the request context. Pending approvals
are expired on cleanup; restart recovery interrupts unfinished children and
expires their pending approvals. Leases expire after the bounded deadline plus
10 seconds if cleanup cannot run. Providers must honor request cancellation;
this is not process isolation for a non-cooperating provider.

Reported child usage is recorded separately and included in parent usage. Do not
sum parent and child totals. Native gateway inference is counted once, not again
as runtime-local usage. Existing historical usage rows are unchanged. Admission
uses a conservative byte-based input estimate and remaining parent token allowance;
reported output over the limit is rejected. These are not a guaranteed monetary
or provider-billing ceiling. Cumulative token admission is described below; hard billing ceilings require
provider-specific enforcement and remain planned.

External child runtimes/model overrides, idempotent delegation requests and
enforceable monetary ceilings remain planned. External parents can request native
children through authenticated callbacks as described below. Native cumulative token admission is implemented below.
No installation activation or
live-provider acceptance is claimed by the automated fixture tests.

## Configuration audit

`delegation.policy.updated` records revision, enabled and approvalMode;
`delegation.profile.updated` records profileId, revision and enabled. Both carry
the administrator actor. Instruction text and objectives are omitted from these
events. Stored settings include updating actor and timestamp; execution snapshots
retain the applied profile content. As elsewhere in the platform, event-bus
publication is best effort; settings/execution persistence errors fail the call.


Execution records now include an immutable access snapshot: runtime, effective
structured grants (an explicit empty list for analysis), workspace/recursive
access and the actual output-token ceiling. Required approvals copy only the
status, deciding actor and time into the child record, including rejection and
expiry; free-text answers/comments are not duplicated. Automatic admission is
labelled `automatic` with its admission time and policy revision, not as a human
approval. Runs exposes this under **Delegation audit**, alongside the objective,
resolved provider/model and parent link. Historical missing snapshots are labelled
unknown; they are never reconstructed from current permissions. Reported child
usage remains inclusive in the parent total. Tree budget estimates and reservations
remain separately visible in the tree; reported usage is not a billing total.

## Execution tree and interruption

Open **Runs → an execution → Execution tree** from either a parent or child.
The tree shows persisted relationships, status, agent/profile, start/finish time,
duration and inclusive reported token usage. It is independent of the recent
100-execution list and event-history window. Queries reject inconsistent families,
cycles or more than 256 nodes / 16 levels rather than presenting a truncated tree.
Native recursion is separately bounded to depth 1–4 (default 1), at most 256
persisted tree nodes, and the existing shared child capacity. Every generation
must inherit an explicit `delegation.request` grant to delegate further; profile
selection never adds a missing capability. Each request follows the current
approval policy. Nested calls share the original root token ledger and consume
ancestor tool/output allowances. Capacity exhaustion rejects immediately rather
than deadlocking ancestors that are waiting for children.

Viewers can read; operators and administrators can request interruption after
reviewing the target and scope. Approval or OAuth consent does not grant this
operator capability to agents.

| API | Minimum role | Contract |
|---|---|---|
| GET /api/v1/executions/{id}/tree | viewer | Root/selected IDs and bounded nodes containing execution and optional cancellation metadata. |
| POST /api/v1/executions/{id}/cancel | operator | Body `{"scope":"child","confirm":true}` or `{"scope":"tree","confirm":true}`; `child` requires a delegated execution. |

A child stop affects that child and its active descendants; its parent receives the tool error and may
continue. A tree stop resolves the root even when requested from a child and
cancels the root plus active descendants. Requests use immutable execution IDs;
other trees, saved graph configuration, grants and future runs are unchanged.
Inputs of an explicitly cancelled root are terminated rather than automatically
requeued, and consumed operator messages are not automatically resubmitted.
Already-published effects cannot be rolled back.

HTTP 202 acknowledges a persisted request, not completed cancellation. Repeated
requests preserve the first actor/time. HTTP 200 with `alreadyTerminal:true`
means no new request was necessary, including a retry after the worker has
finished an earlier interruption request. The original actor/time remain intact.
The worker owns the final execution status;
the console polls and shows requested versus terminal outcomes separately.
Pending synchronous approvals close as expired on cancellation, even when their
timeout policy would approve. Child cleanup releases its capacity slot.

Workers poll shared storage every 250 ms and check cancellation at gateway
boundaries; callback operations inherit the execution context. Storage read
failures fail closed. This is cooperative interruption: providers/runtimes must
honor context cancellation; this is not a hard process kill or an undo operation.
A late successful runtime result is not reported as success after interruption.
Persisted markers survive service restart; existing interrupted-execution recovery
still applies. Markers are retained with installation settings; automatic marker
retention/garbage collection is not implemented. Filesystem storage remains
single-instance; live multi-process PostgreSQL validation is pending.

## Cumulative tree token budget

Administration → Delegation configures `maxTreeTokens` (1–10,000,000; default
65,536). Existing stored policies that predate this field use the default;
new policy writes must specify it. Native parents with an exact
`delegation.request` grant share one durable budget with their analysis children.
Non-delegating runs are unchanged. External parents with an exact delegation
grant also reserve gateway inference against the root ledger. The tighter parent
`limits.maxTokens` also applies. The first reservation fixes the tree ceiling;
later accepted reservations can lower it, but a policy increase cannot replenish
an existing tree. Child policy/profile snapshots are still checked before start.

Each model call reserves the serialized request byte length plus 512 framing
units and its output allowance before inference. A child reserves before waiting
for approval. Parent requests without an output allowance use the policy's finite
output allowance. This is conservative admission accounting, not a vendor-specific
tokenizer, a guarantee of provider billing, or a monetary ceiling.

A CAS ledger in installation settings (`delegation/budget/{rootExecutionId}`)
serializes concurrent reservations and settlement across storage clients. Each call
has a unique entry; settling the same entry twice cannot double-charge it, and a
settled entry cannot be dispatched again. The ledger counts direct model calls,
not the sum of inclusive parent and child execution usage. Reported overages are
recorded in full and prevent further calls once the ceiling is reached.

Rejection, capacity failure, invalidated configuration and cancellation before
dispatch release the entire reservation. After dispatch, known positive usage is
charged and unused reservation released. Missing, zero or invalid usage is unknown:
the reservation becomes an estimated charge, including provider errors and
cancellation. Admission to the model gateway is the dispatch boundary, so waiting
inside a provider concurrency gate is conservatively considered uncertain too.
Failure to persist settlement is returned to the caller and leaves the reservation
held; it must never appear to free budget.

Startup recovery and a once-per-minute reconciliation sweep inspect saved root
executions. Only a terminal root allows ledger closure: pre-dispatch holds are
released, dispatched holds without persisted settlement become estimated charges,
and known settled usage is preserved. Closure, dispatch and settlement share CAS,
so a concurrent or delayed worker cannot dispatch refunded credit or reopen a
closed tree. Repeated sweeps do not change settled amounts or ledger revisions.
The engine first marks its abandoned executions interrupted on restart, or marks
stale executions interrupted after replica lease takeover. Active roots (including
other instances), missing roots, unreadable state and corrupt ledgers never imply
a refund. Failed writes retain the old ledger and are retried on the next sweep;
errors are logged with the root identifier. Other valid trees can still recover.
A dead foreign worker whose execution has not yet been fenced by the engine keeps
its reservations; elapsed time alone is not sufficient evidence of termination.

Thirty days after a terminal root's recorded completion, the sweep compacts its
settled per-call detail into a closed summary containing the original limit,
charged and estimated totals. Undated or active executions are not compacted.
The durable summary is retained indefinitely to reject late admission and preserve
historical UI totals; delayed settlements after compaction fail without changing
it. This bounds per-tree detail, not the total number of historical summaries.
Execution, approval and cancellation history is not removed. No destructive
migration or installation policy change is required. Legacy ledgers participate
when their root execution supplies the required terminal evidence. When upgrading
a shared installation, stop older workers before starting this version: older
binaries do not understand the closed summary and must not write its ledgers.

Runs → Execution tree exposes `limit`, `charged`, `reserved` and `estimated` to
existing viewers. `estimated` is included in `charged`, not an additional amount.
Historical trees without a ledger omit the budget. No prompts, credentials or
reservation identities are included in this summary. API and operator permissions
remain unchanged. Ledger size is bounded to 1,024 call entries per tree.

Telemetry adds `weft_tree_budget_operations_total{operation,outcome}` and
`weft_tree_budget_operation_duration_seconds{operation}` for CAS operations
(`reserve`, `dispatch`, `settle`, `reconcile`; outcomes `ok`, `exhausted`, `error`), with matching
`delegation.token_budget.*` trace spans. Payloads and execution IDs are not metric
labels. Existing model-usage metrics retain their reported-usage meaning.

## External parents through Runtime Protocol v1

External runtimes can request a **native, server-managed** child using their
execution callback: `POST /tools/delegation.request` with `template` and
`objective`. The parent needs the exact grant, a configured managed model and
an active execution. Echo/unknown runtimes and wildcard-only grants remain
unsupported. Children use the parent's configured model and the approved profile;
this does not launch external child processes or accept arbitrary runtime settings.

The call waits for approval and completion. Child failures use the existing tool
error envelope; authorization failures remain HTTP 403. The execution-scoped token
expires with the parent. Cancellation or callback disconnection propagates to the
child; do not retry a request with an uncertain outcome. Review the recorded tree
first. Idempotent request keys remain future work.

Python agents can call `ctx.delegate(objective, template="analysis")`. Configure
the callback timeout to cover the installation's approval/child duration and keep
the parent turn deadline consistent. The helper does not retry or bypass grants.
Gateway model calls and delegated children share the root reservation ledger.
Runtime-local inference outside the gateway is not controlled or billed by that
ledger. Provider cancellation and monetary guarantees retain the existing limits.

The SDK no longer reports gateway model usage or gateway tool attempts again as
runtime-local totals. Returned chat usage remains available to the agent;
`ctx.add_usage` is reserved for genuinely runtime-local work. This aligns with the
Runtime Protocol contract and avoids double-counting existing external callbacks.
Historical totals are not rewritten.
