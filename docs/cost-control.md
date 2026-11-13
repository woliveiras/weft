# Cost control

This page describes monetary usage, prices and budget enforcement. See the
[capability specification](../specs/providers-and-integrations/monetary-usage-and-budget-controls/spec.md)
for acceptance criteria and [production qualification](../specs/production-readiness/prod-03/spec.md)
for outstanding deployment-specific decisions.

## Goals

- Every model call has a recorded cost, computed from the usage the provider
  reported and the price that applied at the time of the call.
- Operators can set monetary budgets at several scopes and Weft enforces them
  before a call is dispatched, not only after the bill arrives.
- Reaching a budget pauses work in a recoverable, visible state; it never fails
  silently and never appears to free budget it cannot account for.
- No prompts, completions or credentials are stored for cost purposes.

Non-goals: reconciling against a vendor's invoice, currency conversion at call
time, or billing users.

## Current state

| Delivered | Where |
|---|---|
| Provider usage (`inputTokens`, `outputTokens`, optional `cacheReadTokens`/`cacheWriteTokens`) on every `ChatResponse`, persisted on executions | `llm.Usage`, `domain.Usage` |
| Versioned price catalogue (`GET/PUT /api/v1/cost/prices`), local plugins zero by default, unpriced policy | `server/internal/cost/prices.go`, [API](api.md#model-prices) |
| A `cost.recorded` event for every call the gateway dispatches (native turns, external callbacks, delegation children), with reported or estimated usage, and a separate unpriced `external` record for `TurnResult.usage` | `llm.Registry` observer, `server/internal/cost/record.go` |
| Per-node `limits.maxTokens` | graph spec |
| Cumulative token admission for delegation trees: a CAS ledger with reservation before dispatch, settlement after, and recovery | [delegation.md](delegation.md#cumulative-tree-token-budget) |
| Usage reported by external runtimes for work outside the LLM callback | [runtime-protocol.md](runtime-protocol.md) (`TurnResult.usage`) |
| Task-run budget: policy default, CAS reservation before dispatch (native, callback, delegation), settlement, pause at the hard level, audited human raise that resumes the run, warning event, startup reconciliation of open reservations | `server/internal/cost/budget.go`, `server/internal/engine/cost_budget.go`, [API](api.md#monetary-budgets) |
| Monthly installation, user, project and workflow budgets (UTC calendar month), warning level, optional interrupt level, refusal of new task starts at the hard level, multi-budget reservation with rollback | `server/internal/cost/definitions.go`, [API](api.md#monetary-budgets) |
| Console: monthly spend (by provider/model, user, project, workflow, task run), budget bars with warning and hard levels and a raise for administrators, price and budget editing, the budgets a new task is held to, and a paused run's budget with its raise on the task | **Models → Costs**, Tasks |

Cost records are events, so they follow the event history retention. A call
the provider rejected (HTTP 4xx other than 408/429) was not served and is not
recorded; a failure after dispatch, a timeout or zero reported usage is
recorded at the request estimate (`estimated: true`).

## Concepts

| Concept | Meaning |
|---|---|
| **Usage** | What the provider reported for one call: input, output, cache-read and cache-write tokens (each optional), plus provider-specific units when a model is not priced per token. |
| **Price** | A versioned rate card per provider type, provider name and model: price per million input, output, cache-read and cache-write tokens, and a currency. |
| **Cost record** | One row per dispatched call: execution, task, node, user, project, provider, model, usage, price version, computed cost, and whether usage was reported or estimated. |
| **Budget** | A monetary limit on a scope and period, with warning and hard thresholds. |
| **Reservation** | The maximum cost a call can reach, held against every applicable budget before dispatch and settled afterwards. |

## Usage metering

1. Extend `llm.Usage` with optional `cacheReadTokens` and `cacheWriteTokens`.
   Providers that report them map them; others leave them empty.
2. The LLM gateway, not the provider, writes the cost record, so every provider
   and every runtime path (native, external callback, delegation) is covered by
   one implementation.
3. Missing, zero or invalid usage is **unknown usage**: the call is charged at its
   reservation and marked `estimated`. This follows the existing delegation ledger
   rule that uncertainty must never free budget.
4. Usage an external runtime reports outside the callback (`TurnResult.usage`) is
   recorded as a separate `external` cost record, priced only when the runtime also
   names the provider and model; otherwise it is recorded unpriced and shown as such.

## Price catalogue

- Stored in installation settings, editable by administrators in the console and
  through the API. Every change creates a new version; cost records keep the
  version they were computed with, so later edits never rewrite history.
- A model without a price is **unpriced**. Policy decides whether unpriced calls
  are allowed (default: allowed and shown as unpriced, so offline and local models
  keep working), or refused while a monetary budget applies to the call.
- Local providers (Ollama, llama.cpp, vLLM, mock) default to a zero price.
- Weft ships no vendor prices. Rates change often and differ by contract and
  region; operators enter the prices that apply to them.

## Budgets

| Scope | Typical use |
|---|---|
| Installation | Overall ceiling for the whole deployment |
| User | Monthly allowance per person |
| Project | Spend per repository or team |
| Workflow | Spend per reusable workflow |
| Task run | Ceiling for one run, raised by a human when reached |

- Periods: calendar month (UTC) or per run.
- A changed monthly definition applies to the current period, subject to the
  [floor set by a manual raise](#monthly-budget-floor).
- Thresholds: a **warning** level (default 80%) and a **hard** level (100%).
  An optional **interrupt** level (for example 110%) stops a turn that is still
  running.
- A call is checked against every budget that applies to it. The tightest one
  decides.
- Delegation children count toward their root's task-run budget, as tokens already
  do in the delegation ledger.

## Enforcement

The enforcement point is the LLM gateway, before dispatch:

1. Compute the reservation: serialized input estimate plus the requested output
   allowance, at the current price version. If no output allowance is set, use the
   node or policy limit; a call without any finite allowance cannot be admitted
   while a monetary budget applies.
2. Reserve against every applicable budget atomically, reusing the CAS ledger
   pattern from delegation. Concurrent calls therefore cannot overshoot a budget.
3. On success, settle with the reported usage and release the rest. On unknown
   usage, keep the reservation as an estimated charge.
4. On rejection, cancellation or configuration change before dispatch, release the
   whole reservation.
5. Failure to persist a reservation or settlement is returned to the caller and
   fails closed.

Zero-cost calls, including unpriced calls allowed by the price policy, pass the
reservation check even at the limit. With `unpriced: "refuse"`, the gateway
refuses unpriced calls while a budget applies.

What happens when a threshold is reached:

| Level | Effect |
|---|---|
| Warning | Event, notification in the console, note on running tasks. Nothing is blocked. |
| Hard | Calls that would charge a reached budget are refused. A hard budget block **pauses** the affected run until it can resume or a human stops it. Reached monthly budgets also block new task admission. |
| Interrupt | The running turn is cancelled, work is kept as it is, and the run pauses. |

An administrator raises the budget. The new limit is compared with charged
and reserved spend. A limit at or below that total retains the holds without
scheduling resumptions. The warning state is recalculated at the new
threshold. Before resuming a graph, the controller checks the current
task-run, workflow, project, user and installation ledgers that apply to it.
If a budget is reached, the graph stays paused under that ledger and the
transition audit records `budget_reached` and the blocking scope and ID.
When a graph lease is busy, the ledger retains the pending transition for the
background controller. A resume records an intent ID with the graph state so
the controller can recognize a successful write after a timeout.

Task admission checks the effective monthly limits before creating the
execution graph, worktree, or environment and before dispatching input.
A reached budget returns `409 budget_admission_blocked` with its scope and
ledger ID. Raise it in Models → Costs, then retry the saved task.

Manual graph Resume, Run all, node Resume, and starting a node of a stopped
graph check the same applicable ledgers. Starting a stopped task execution graph creates a new lifecycle run
ID and keeps the task-run ledger keyed by graph ID. Monthly budgets for the
current UTC month still apply. A blocked request returns
`409 budget_resume_blocked`, preserves the current state and any hold, and
records the operator's attempt with the blocking ledger. The console shows
where an administrator can raise the budget.

Increasing a monthly definition through `PUT /api/v1/cost/budgets` lets the
controller apply the new limit and attempt a durable, audited resume for held
graphs. The controller checks the other applicable ledgers before resuming.
Reductions preserve a reached hold, including a refusal from a projected
reservation. Increases to or below charged and reserved spend keep the run
paused. A hold with capacity cannot by itself block a manual resume. If a
held ledger no longer applies and another applicable ledger is reached,
the controller transfers the hold and audits the blocking ledger.

The budget controller counts exhausted reconciliation conflicts in
`weft_cost_budget_operations_total{scope="all",operation="reconcile_graph",outcome="conflict"}`.
It logs the number of conflicted actions at most once per minute per process.
Warnings about invalid node controls are limited to once per graph per minute.

Budget reconciliation commits graph state and stale-hold retirement only
while the checked definitions and ledger revisions still match. A conflicting
write leaves the operation for the next sweep. Manual actions also check
applicable budgets before changing state. Each priced call still reserves
capacity before dispatch. If definitions or policy change during admission,
the reservation releases partial holds and retries with current settings.

### Monthly budget floor

An explicit **Raise** sets a floor for that monthly ledger. The effective limit
is the higher of the current definition and the floor. A later definition can
lower the effective limit back to the floor. Existing raised ledgers without a recorded
Raise amount keep their stored limit as the floor until an explicit Raise
records the new value.

### Hold retirement and period rollover

The controller retires holds for deleted, stopped or replaced runs, and for
runs whose enabled fixed replicas have completed. A current-month hold
whose definition was removed or no longer matches the graph is retired with
`not_applicable`. An individual turn may end
while the graph still has work. Holds written by older versions without a
run ID are retired as `legacy_unbound`. An administrator can review and
resume a graph that remains paused after such a hold is retired.

Monthly ledgers close at the UTC month boundary. Their spend stays available
in history. A closed ledger cannot be raised and its historical view suppresses
pause and resume-pending flags. A still-paused run enters a durable resume
attempt against the new month's budgets. If a current budget is reached,
that budget takes over the hold.

## Visibility

- Console: spend per task, user, project, workflow, provider and model; budget bars
  with warning and hard levels; unpriced and estimated spend shown separately.
- New Task and task views show the remaining budget before start and during the run.
  The costs and task views show pending resumptions from the ledger across page
  reloads.
- Events: `cost.recorded`, `budget.warning`, `budget.reached`, `budget.raised`,
  `budget.graph_transition`, `budget.reservation_failed`.
- Metrics: spend and reservation outcomes by provider and model, with bounded
  labels only (no execution, task or user IDs as labels).

## Failure semantics

- The cost store being unavailable blocks calls covered by a monetary budget; it
  does not block calls with no applicable monetary budget.
- A price change during a run applies to the next reservation, not to calls
  already reserved.
- Startup recovery and periodic reconciliation close reservations of terminal
  executions, as the delegation ledger does today.

### Ledger recovery

The cost ledger endpoints identify invalid rows with `invalid: true` and
their current `revision`. The console shows the scope and ID. The audit
event `budget.ledger_invalid` identifies the affected revision without
including the raw value. Other graphs can resume if the invalid ledger
does not apply to them. Isolated invalid ledgers are reported without
changing the normal five-second transition sweep. Model admission under an invalid applicable ledger
fails closed until it is repaired.

1. Record the scope, ID and revision and identify the setting key
   `cost/budget/<scope>/<id>`. Use the [backup and restore procedure](operations.md)
   to take a consistent backup before repair.
2. In an isolated copy, recover the setting value using a verified backup and
   reconcile any later charges and outstanding reservations against the
   execution records. Preserve unresolved reservations. Escalate to the
   installation's datastore operator if the ledger cannot be reconstructed.
3. With controllers quiesced, have the datastore operator replace only the
   affected setting using the observed revision as a precondition and
   increment its revision. The SQL stores use the `settings` table. The
   filesystem store uses the corresponding record under `settings/` with
   the value encoded as Base64 in the JSON wrapper. Preserve the backup and
   audit history.
4. Verify the repaired view and its totals with
   `GET /api/v1/cost/budgets/{scope}/{budget}`, then confirm the controller
   processes any retained transition. A regular raise cannot repair a corrupt
   value.

## Tests required

- Unit: price lookup and versioning, cost computation including cache tokens and
  unpriced models, threshold evaluation, reservation and settlement arithmetic.
- Integration: concurrent reservations against one budget with real storage;
  pause and raise of a task run; unknown usage; restart recovery; delegation
  children counted against the root.
- Browser: price editing, budget configuration, paused-run raise flow, spend views.

## Delivery order

1. Usage extension, cost records and the price catalogue (visibility only).
2. Task-run budget with pause and raise.
3. User, project, workflow and installation budgets.
4. Interrupt level and console spend views.

### Hidden reasoning tokens

OpenAI-compatible endpoints differ in how they report reasoning. OpenAI counts
it inside `completion_tokens`. Gemini's compatible endpoint leaves its thinking
tokens out of `completion_tokens`, but bills them and includes them in
`total_tokens`. The OpenAI plugin therefore records as output whatever part of
`total_tokens` is neither prompt nor completion, so budgets and prices see the
real cost.
