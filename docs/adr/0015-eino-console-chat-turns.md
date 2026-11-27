# 0015 — Use Eino to orchestrate model-assisted turns in Console Chat

**Status:** Proposed · **Date:** 2026-11-27 · **Decision owner:** Weft maintainers

## Context

An operator can ask “Which tasks are ready?” and then “Start the second one.” The first answer needs current, authorized records. The follow-up needs the previous selection, a verified target and a review the operator can confirm later. Today the API claims the submitted message, prepares role-filtered history and calls `chat.RunTurn`. That Go function coordinates model calls and reads, then returns a grounded answer or a typed plan. Weft resolves the plan and stores the outcome in the owner's session. Explicit commands and review decisions already have direct handlers. [Chat request handler](../../server/internal/api/chat_sessions.go) · [Current turn](../../server/internal/chat/turn.go) · [Conversational Console contract](../../specs/product-and-tasks/conversational-console/spec.md)

Eino supplies graph composition, tool orchestration and interrupt/resume. Chat already runs in Go and has no Python or LangGraph execution dependency. The [workflow-agent ADR](0014-eino-agent-runtime.md) covers native turns, the bundled external agents and the removal of Python tooling. Console Chat operates under the signed-in operator's identity and can prepare reviews of installation-wide actions.

## Decision

Use a compiled Eino `compose.Graph` **inside the Go server** at the existing model-assisted turn boundary. Give each invocation the bounded conversation, the current request's authorized read tools and Weft's model-call function. It returns the existing `chat.Interpretation` and releases its state. **A turn that proposes an operation ends when it returns the plan.** Weft subsequently prepares and persists the review. A later confirmation applies to that stored review, even after a controller restart. [Eino compose API](https://pkg.go.dev/github.com/cloudwego/eino@v0.9.23/compose) · [Review decision handler](../../server/internal/api/chat_agents.go)

| Stage | How the proposed flow works |
| --- | --- |
| Receive a message | The API loads or appends to the owner's session, claims the request and supplies the history visible to the current role. Explicit lookup, choice, slash-command and review-decision paths retain their existing routing. Only a request reaching today's model branch invokes Eino. |
| Interpret or consult | A graph node calls Weft's selected Chat model. A read branch validates the entire tool batch, invokes only registered request-scoped reads, and returns projected observations to the model. Preserve tool-call IDs and provider metadata, the current three-read limit, bounded argument correction and request deadline. A grounded answer requires observed data. |
| Propose or clarify | A proposal branch validates the model's operation and returns a typed plan. For “Start the second one,” Weft resolves the referenced task against verified context and current records. An ambiguous target produces a clarification. For an eligible task, the existing handler prepares the review with the actor, session, task revision and reviewed scope. The session records the review before the UI offers confirmation. [Task-start preparation](../../server/internal/api/chat_task_start.go) |
| Confirm, decline or recover | The operator acts on the stored review. Weft verifies identity, role and review binding, claims the decision, rechecks the relevant revisions and consent, and records the actual domain outcome. Repeated confirmations and lost replies follow the existing claim and reconciliation paths. A decline, expiry or stale revision cannot start the task. [Stored reviews](../../server/internal/api/chat_agents.go) · [Task-start reconciliation](../../server/internal/api/chat_task_start.go) |

Weft's session store remains authoritative for conversation history, reference resolution and pending decisions. Eino's per-turn state contains only the context needed for the current request. Waiting for approval requires neither a suspended graph nor an Eino session store. A future requirement to resume *within* an interrupted model turn would warrant a separate persistence decision. [Session storage](../../server/internal/chat/sessions.go) · [Eino interrupt/resume](https://www.cloudwego.io/docs/eino/core_modules/eino_adk/agent_hitl/)

## Implementation boundaries and consequences

- **Server wiring.** Add the Eino dependency to the server module for Chat and isolate its imports in the turn implementation. Compile the graph once during wiring. Pass model and read functions at invocation time so concurrent sessions cannot share an actor, history or tool catalog. Feed model calls through the existing callback to preserve model-revision checks, usage accounting, operation telemetry and cancellation. [Server module](../../server/go.mod) · [Model-call wiring](../../server/internal/api/chat_sessions.go)
- **Behavior and authorization.** Reuse Weft's operation schemas, validated plan decoding, read projections and domain review handlers. A model-selected operation describes intent. The only executable tools in the graph are the reads supplied for that authenticated request. Provider failures, truncated output and exhausted limits go through existing Chat recovery. The UI continues to consume the same answers, clarification prompts, review cards and recorded outcomes. [Operation schemas](../../server/internal/chat/operation_tools.go) · [Turn validation](../../server/internal/chat/turn.go)
- **Deployment and ownership.** Chat and native agent turns use the same pinned Eino dependency in `weftd`, with separate graph definitions and request-scoped capabilities. Chat maintainers own Chat compatibility and must measure binary growth, turn latency and model-call counts. The console model path runs in process. Its adoption can be qualified independently of the external agent service.

## Alternatives

Keeping the existing Go loop has the lowest dependency and migration cost. It remains the baseline if Eino cannot express the same behavior clearly and pass the same evaluations. Reusing the external agent service from ADR 0014 would require translating operator-scoped reads and session context into a new remote contract. Using Eino checkpoints for review would introduce a second durable record to reconcile with Weft's decision, expiry and recovery rules.

## Confirmation and rollback

[TASK-012](../../specs/product-and-tasks/conversational-console/spec.md) owns Chat behavior. Its [Eino qualification rows](../../specs/product-and-tasks/conversational-console/matrix.md#eino-turn-qualification) define the observable results for this change.

1. Exercise both turn implementations with the same scripted model and read catalog. Compare observable answers, plans, calls and errors for a grounded query, an ambiguous follow-up, a mixed read/proposal batch, invalid arguments, a provider failure and cancellation. Run concurrent turns with disjoint histories and permissions. [Turn unit tests](../../server/internal/chat/turn_test.go)
2. Through the real Chat API and a durable local store, ask for a task list, propose a start and verify that the task remains a draft. Reopen the store, confirm the review and verify one admission when confirmation is retried. Exercise decline, role loss, a different session owner and a changed task revision. Verify explicit commands still avoid model calls. [Session integration tests](../../server/internal/chat/sessions_integration_test.go) · [Task-start binding tests](../../server/internal/chat/task_start_review_test.go)
3. Qualify the Eino path in a local installation with `make eval-chat`, `make test`, `make lint` and `make build`. Compare results and resource measurements with the Go loop before selecting Eino by default. Select one implementation per request. Roll back new turns to the qualified Go loop while the usual Weft handlers continue to settle pending reviews. [Project commands](../../Makefile)
