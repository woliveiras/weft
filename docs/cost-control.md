# Cost control

Status: **partly delivered.** This page records the target contract for
monetary cost visibility and enforcement. Only what the
[current state](#current-state) section lists is shipped.

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

Everything in the design is delivered except reconciling against vendor
invoices (a non-goal). Spend views read `cost.recorded` events, so they follow
the event history retention.

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

What happens when a threshold is reached:

| Level | Effect |
|---|---|
| Warning | Event, notification in the console, note on running tasks. Nothing is blocked. |
| Hard | New calls under that budget are refused. A task-run budget **pauses** the run in a visible waiting state until a human raises the budget or stops it. Other scopes refuse new tasks and pause running ones. |
| Interrupt | The running turn is cancelled, work is kept as it is, and the run pauses. |

Raising a task-run budget is an audited human decision. It is never automatic and
cannot be requested by the model.

## Visibility

- Console: spend per task, user, project, workflow, provider and model; budget bars
  with warning and hard levels; unpriced and estimated spend shown separately.
- New Task and task views show the remaining budget before start and during the run.
- Events: `cost.recorded`, `budget.warning`, `budget.reached`, `budget.raised`,
  `budget.reservation_failed`.
- Metrics: spend and reservation outcomes by provider and model, with bounded
  labels only (no execution, task or user IDs as labels).

## Failure semantics

- The cost store being unavailable blocks calls covered by a monetary budget; it
  does not block calls with no applicable monetary budget.
- A price change during a run applies to the next reservation, not to calls
  already reserved.
- Startup recovery and periodic reconciliation close reservations of terminal
  executions, as the delegation ledger does today.

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
