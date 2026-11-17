---
id: RUN-008
title: Agent Context Usage Breakdown
summary: Show how each native agent's model context is spent, by category, for every model call of an execution.
status: planned
scope: [native runtime context accounting, agent.context events, execution API, Runs and execution tree views]
risk: medium
risk_domains: [data exposure, event volume, compatibility, accuracy of estimates]
reversibility: The breakdown is additive telemetry. Stop recording it and hide the view; existing agent.context, usage and budget semantics are unchanged.
change_surfaces: [server/internal/runtime/native, server/internal/runtime, server/internal/engine, server/internal/api, web/src/components]
contracts: [agent.context event payload, execution read API, native context budget (RUN-003 AC-001)]
review_policy: Maintainer review of the spec, open decisions and matrix before tests or implementation.
navigation: [specs/README.md, specs/runtime-and-delivery/agent-context-usage/matrix.md]
documentation: Update docs/events.md, docs/api.md and docs/native-coding-runtime.md with the approved behavior when delivered.
authority:
  granted: [draft specification, matrix, index, local commit]
  withheld: [implementation, push, deployment, release]
dependencies: [RUN-003, RUN-002, RUN-001, INT-005, TASK-004, REPO-GUIDANCE]
---

# Agent Context Usage Breakdown

Let an operator see, for each agent in an execution, how much of the model's
context window each part of the request uses: the system preamble, the task
and loaded guidance, tool schemas, conversation, tool results and the
compaction summary. Show the free space and the reserve kept for the answer,
so it is clear why an agent elided or compacted its conversation, or why a
call failed for lack of space.

## Current behavior

The native runtime estimates each request with `llm.EstimateTokens` (about
four bytes per token) and keeps it within a budget derived from the
provider's context window. It assumes 32,768 tokens when the provider does
not report a window, and caps the working limit at 65,536 tokens on larger
windows. It reserves `llm.DefaultAnswerTokens` for the answer.
[Turn context management](../../../docs/native-coding-runtime.md#turn-context-management-nat-02).

When it elides or compacts, the runtime publishes `agent.context` with
`operation`, `outcome`, `messages`, `tokensBefore`, `tokensAfter` and
`window`. [Event catalogue](../../../docs/events.md). Executions expose total
`inputTokens` and `outputTokens`, and the execution tree adds child usage to
the parent total ([RUN-002](../dynamic-delegation-and-execution-trees/spec.md)).
Nothing records what makes up a request. No view shows how full the context
is before a call.

## Behavior and invariants

- Before each model call of a native turn, after the context budget has been
  applied, the runtime measures the request that it will send. It splits the
  estimate into the fixed categories below. Compaction summary calls are
  measured the same way and are marked as compaction calls.

  | Category | Contents |
  | --- | --- |
  | `system` | The platform preamble from `buildSystem`, including small-model guidance |
  | `task` | The task text and operator messages in the task message |
  | `guidance` | Guidance composed into the task message: project steering, the skill catalogue and preamble, and loaded skill bodies and references (RUN-001, REPO-GUIDANCE) |
  | `tools.builtin` | Schemas of Weft's built-in tools |
  | `tools.mcp` | Schemas of MCP tools (`mcp.*`, INT-005) |
  | `conversation` | Assistant messages, including tool call names and arguments |
  | `results.builtin` | Results of built-in tool calls, including elision receipts and `guidance.load` acknowledgements |
  | `results.mcp` | Results of MCP tool calls |
  | `summary` | The compaction summary and the retained plan text |

- The categories are exhaustive and disjoint. Their sum equals the estimate
  for the same request that the context budget uses. Every byte of the
  request belongs to exactly one category.
- Each measurement also reports the provider window, the working limit, the
  answer reserve, whether the window was reported or assumed, and the free
  space (`limit` minus the sum, which may be negative). After the call, the
  provider-reported `inputTokens` and cache tokens for that call are attached
  when the provider reports them. Estimated and reported values are labelled
  separately and never mixed into one figure.
- Measurements contain counts and category names only. They never contain
  prompt text, tool arguments, tool results, guidance bodies or MCP payloads.
  D-04 decides whether MCP schemas and results are further split by
  connection.
- A measurement is tied to the graph, node, replica, execution and turn, and
  to the model call's sequence number within the turn. Delegated children
  record their own measurements. Context is per agent: the view never adds
  the context of parent and child executions together.
- Recording is observational. It does not change what is sent, when the
  runtime elides or compacts, budget reservations, cost accounting or the
  outcome of the turn. If a measurement cannot be recorded, the turn
  continues. The failure is counted in a metric and logged once per turn
  with the execution identifier. The view shows that the measurement is
  missing, never zero.
- Runtimes other than native (`echo`, `external`) do not manage a model
  context. Their executions show that a breakdown is not available rather
  than an empty or estimated one.
- Reads follow the existing execution read roles. Metrics use bounded
  labels (category and outcome); agent, node and execution identifiers
  never appear as metric labels.

## Acceptance criteria

### AC-001 — Category breakdown per model call

For each model call of a native turn, including compaction calls, Weft
records one measurement with every category in the table, the window, the
working limit, the answer reserve, the free space and whether the window was
assumed. The category sum equals the estimate of the request as sent. A
request with no MCP tools reports zero for both MCP categories. A skill body
or reference loaded with `guidance.load` increases `guidance` in the next
call, where it is rendered into the task message; only the short
acknowledgement counts in `results.builtin`.

### AC-002 — Estimated and reported tokens

When the provider reports usage for the call, the measurement includes the
reported input tokens, and cache read and write tokens where reported, next
to the estimate. When it does not, the reported values are absent, not zero.
A failed or interrupted call keeps its estimate and records no reported
usage. Existing execution totals, spend records and budgets are unchanged.

### AC-003 — Context view per agent

The execution detail in Runs shows a stacked bar of the latest measurement
for the execution, with a legend that gives tokens and percentage of the
window for each category, the free space and the answer reserve. The
operator can select an earlier call of the execution. Elision and compaction
events from `agent.context` are shown on the same timeline. Each execution
in the execution tree shows its own latest measurement and a link to its
detail. An assumed window and estimated values are labelled as such.

### AC-004 — Live updates and history

While an execution is running, a new measurement appears in the open view
without a reload. After the run, and after a server restart, the same
measurements are available for the retention period of execution events.
A measurement over the working limit or the window is shown as exceeding it
and is not clipped.

### AC-005 — No content exposure

Measurement payloads, API responses, logs and metrics contain no prompt,
message, tool argument, tool result, guidance or MCP content, and no
credential. A caller without read access to the execution cannot read its
measurements.

### AC-006 — Compatibility and non-native runtimes

Existing `agent.context` elide and compact payloads and consumers keep
working unchanged. Recording failures do not change the turn outcome or
the context budget decisions of RUN-003 AC-001. Executions of `echo` and
`external` runtimes show that the breakdown is not available.

## Open decisions

### D-01 — Granularity and storage (AC-001, AC-004)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| One event per model call (a new `operation: measure` of `agent.context`, or a new `agent.context.usage` type) | Full history per call, live through the existing event stream, no schema migration. | Adds one event per call. Long turns produce many events; retention and the Activity feed must not be flooded. |
| Latest measurement stored on the execution only | Small and cheap. Enough for the "how full is it now" view. | No history of how context grew, and no correlation with elisions. |
| Both: events for history, latest copy on the execution | Fast tree view and full history. | Two writes to keep consistent. |

If a new event type is chosen, decide whether Activity hides it by default.

### D-02 — Estimator (AC-001, AC-002)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Keep the four-bytes-per-token heuristic | Same number the budget uses, so the bar explains its decisions. Provider-independent. | Can differ noticeably from the provider's count, especially for code and non-Latin text. |
| Scale categories proportionally to the reported input tokens after the call | Totals match the provider. | Only after the call; categories remain approximate and no longer match the budget estimate. |
| Provider tokenizer where available | Most accurate. | Per-provider code, extra latency, and differs from the budget unless the budget also changes, which is a RUN-003 change. |

### D-03 — Where the view appears (AC-003)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Runs execution detail and the execution tree only | Focused on diagnosis, minimal navigation change. | Not visible while following a task from Activity. |
| Also a compact meter on the task's Activity row | Visible during live work. | More UI surface; needs care for small screens and many replicas. |

### D-04 — MCP detail (AC-001, AC-005)

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| One MCP category for schemas and one for results | Bounded, simple. | Does not show which connection is expensive. |
| Split by MCP connection name | Shows the costly connection. | Connection names become part of the payload. They are configuration, not content, but are unbounded per installation; keep them out of metric labels. |

## Explicit exclusions

- Changing context limits, elision or compaction thresholds, or the
  estimator used by the budget (RUN-003 AC-001).
- Monetary cost per category and budget enforcement (INT-004).
- Context accounting for runtimes other than native.
- Showing prompt or tool content in the view.
- Implementation, deployment and release in this review increment.

## Reconciliation

- [Behavior and oracle matrix](./matrix.md). Record the approved options and
  update the conditional oracles before writing tests.
- Reconcile [events](../../../docs/events.md), [API](../../../docs/api.md)
  and [native runtime](../../../docs/native-coding-runtime.md) on delivery.
