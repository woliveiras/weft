---
id: RUN-010
title: Eino runtime migration and Python-free distribution
summary: Run native turns and bundled workflow agents with Eino in Go and qualify the Weft distribution without Python or LangGraph.
status: implemented
scope: [native turns, external agents, factory workflows, Go extension tooling, packaging, regression qualification]
risk: high
risk_domains: [tool authorization, budget accounting, message delivery, workflow compatibility, isolation]
reversibility: Rebuild a fresh installation from the selected revision. Back up and restore durable state created by the current installation.
change_surfaces: [server/internal/runtime, server/internal/engine, agents, sdk, examples, scripts, Makefile, Dockerfile, Dockerfile.sandbox, docker-compose.yml, deploy]
contracts: [native runtime kind, Runtime Protocol v1, factory message contracts, execution-scoped callbacks, installation prerequisites]
navigation: [specs/README.md, specs/runtime-and-delivery/eino-runtime-migration/matrix.md]
documentation: [docs/adr/0014-eino-agent-runtime.md, docs/adr/0015-eino-console-chat-turns.md, docs/runtime-protocol.md]
dependencies: [RUN-003, TASK-006, TASK-007, TASK-012, ENV-004, INT-002, INT-004, OPS-001, OPS-002]
authority:
  granted: [specification, behavior and oracle matrices, local implementation, local commits]
  withheld: [deployment, release, remote push]
---

# Eino runtime migration and Python-free distribution

An operator can keep using published agents, inline workflow nodes and the bundled software factory while Weft moves agent orchestration to Go. The maintainer confirmed a single-user installation with no projects depending on the Python SDK or the interpreter in the default sandbox. The maintainer accepted the direct cutover on 2026-11-27. The [behavior matrix](./matrix.md#final-cutover-qualification) records the fixtures, topologies, commands and observed outcomes. [Agent architecture](../../../docs/adr/0014-eino-agent-runtime.md) · [Chat architecture](../../../docs/adr/0015-eino-console-chat-turns.md)

## Ownership and invariants

| Boundary | Contract during migration |
| --- | --- |
| Native workflow node | One in-process Eino invocation implements one Weft turn. Weft chooses which replica runs, enforces grants and budgets, routes outputs and settles input delivery. [RUN-003](../native-coding-runtime/spec.md) owns the coding behavior. |
| External agent | The Go service executes a turn through Runtime Protocol v1. Model keys, storage, broker and sandbox authority stay with Weft. Independently supplied HTTP runtimes can use any language. [Runtime protocol](../../../docs/runtime-protocol.md) |
| Console Chat | A separate in-process Eino graph receives the current operator's bounded history, read catalog and model-call function. It returns an answer, clarification or typed proposal. Weft owns sessions, confirmation and the domain action. [TASK-012 qualification](../../product-and-tasks/conversational-console/matrix.md#eino-turn-qualification) |
| Durable state | Workflow definitions, approvals, reviewed candidates, message history and accounting retain their Weft identities. A paused business decision can outlive the graph invocation. Branching from completed executions and persisting Eino checkpoints are outside this migration. |

## Acceptance criteria

### AC-001 — Existing native agents run without reconfiguration

A published catalogue version bound to a workflow and an inline node saved on the current installation both execute through Eino under `runtime.kind: native`. Neither requires republishing, rebinding or an HTTP agent service. Preserve prompts, parameters, guidance, context compaction, streaming, small-model correction, tool order, model-round and tool-call limits, stop/blocked signals, delegation accounting and the restricted PR-analysis path. A failed or cancelled turn retains its existing observable outcome. The runtime never publishes partial model output as a successful result. Weft gateways continue to enforce and record the actual actions. [Native coding contract](../native-coding-runtime/spec.md) · [Delegation contract](../dynamic-delegation-and-execution-trees/spec.md)

### AC-002 — Go extensions complete one external turn through Protocol v1

Provide Go client, server and test utilities for agent authors. A compiled Eino service answers health checks and dispatches a turn by agent name. A failed graph compilation leaves the service unready. The service uses the originating controller's execution-scoped callbacks for model, tools and state. Concurrent turns have distinct state and capabilities. Preserve tool-call IDs, opaque provider metadata, errors and cancellation. Gateway calls are counted once. A failed or invalid result produces no success emit.

The summary pilot reads a granted document and emits one `summary.proposed` with the source message ID. The channel holds it for approval. Rejection prevents delivery. After approval, downstream message history establishes delivery. A candidate review yielding mid-turn stops remaining calls and discards buffered outputs. The decision can drive a later turn with a fresh execution identity. [Protocol v1](../../../docs/runtime-protocol.md) · [ENV-004](../../workspaces-and-environments/durable-human-review-and-protected-previews/spec.md)

### AC-003 — The bundled factory completes and reworks real inputs

Port planner, researcher, developer, reviewer and QA to the Go service under their existing agent names. Preserve planner state between research turns, task correlation fields, channel types and payloads. The developer's offline scaffold, QA command, granted commands and sandbox toolchain must agree on a workload runnable without Python.

For each `code.ready` input, the reviewer reads the listed files and requests a model verdict in `analyse`. In `decide`, missing files, empty or unreadable content, or a TODO require changes even when the model approves. Preserve the existing heuristic for a completed analysis with no usable model verdict. Carry the original item, verdict and comments into `review.approved` or `review.changes_requested`. Review rejection and QA failure reach the developer. Passing QA reaches the existing human gate before handoff. Qualify the external factory and the seeded native factories separately. [Factory graph](../../../examples/graphs/software-factory.yaml) · [Native factory acceptance](../native-coding-runtime/spec.md#ac-005--factory-stages-and-role-guidance)

### AC-004 — A clean Weft distribution operates without Python or LangGraph

Build, test, lint, install and run the controller, native profile and bundled factory with Python, pip and LangGraph absent. Retire the shipped Python SDK and optional Python framework adapters, Python agent image, controller interpreter package, Python-based default sandbox and required Python scripts. Ship the Go service with working health checks at the existing Compose and Kubernetes service addresses. A project can supply Python in its own explicitly selected environment.

For an external model callback failure, Weft records a redacted diagnostic against the execution. The agent receives a generic error. A typed upstream failure can supply its HTTP status and retryability to the controller history. Provider response bodies and credentials stay out of the event and the runtime response.

Retain equivalent coverage for provider selection and redaction, SDK behavior, event-schema validation, Compose and wrapper contracts, workspace journal, native profile recovery, sandbox isolation and preview probes. A skipped check or a successful build alone is insufficient. Record which installation profiles, artifacts and commands were actually exercised. Keep historical LangGraph evidence attributed to the original runtime. [OPS-001](../../installation-and-operations/compose-installation-and-operations/spec.md) · [OPS-002](../../installation-and-operations/standalone-installation-and-recovery/spec.md) · [INT-002](../../providers-and-integrations/model-provider-adapters-and-conformance/spec.md)

### AC-005 — Restarts preserve pending work on the current installation

The maintainer selected a fresh installation. Importing an earlier Forge or Weft profile, rolling durable state between binary versions and migrating a running installation are outside the cutover scope. Qualify native turns, external services and Chat against the current release. After a restart with the same store and broker, a held message retains its identity and can settle against its original approval. Interrupted work follows the existing redelivery and idempotency rules. A partial publication or a failed approved delivery remains visible in message and execution history. The native profile preserves graphs and archived Chat sessions through current-version backup and restore, and rejects an unknown format before changing its files.

## Qualification

Start with deterministic model fixtures, scoped tools and the real execution/store boundaries. Exercise the summary pilot before replacing the bundled factory image. Compare model calls, usage, latency, binary/image size and memory under recorded concurrency and fixture conditions. Record the Eino version and topology. The conversation sets no performance threshold and selects no production model. Chat-specific parity and rollback belong to [TASK-012](../../product-and-tasks/conversational-console/spec.md). The completed qualification covers the fresh native installation, disposable Compose and single-node Kubernetes. Performance at production concurrency and multi-node shared storage remain outside the measured results.
