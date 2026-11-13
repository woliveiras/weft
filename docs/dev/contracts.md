# Cross-component contracts (historical working reference)

> Superseded by the formal specifications: [api/openapi.yaml](../../api/openapi.yaml), [docs/api.md](../api.md),
> [docs/runtime-protocol.md](../runtime-protocol.md) and [schemas/](../../schemas). Kept as the original build-time
> agreement between components; where it differs, the formal specs (and the code) win.
>
> Cross-component design contracts for capabilities not yet implemented are in
> [product-contracts.md](../product-contracts.md); do not treat proposed operations there as shipped API.

The original agreement used the camelCase JSON field names from the Go structs
in `server/internal/domain`, `server/internal/runtime`, `server/internal/llm`
and `server/internal/tools`.

## 1. Public HTTP API (`/api/v1`)

Auth: `Authorization: Bearer <token>` on every request. EventSource (SSE)
cannot set headers, so `GET /api/v1/events/stream` also accepts
`?access_token=<token>`. Roles: `viewer` (GET), `operator` (+ start/stop,
inject, approve, operator messages), `admin` (+ create/update/delete graphs).
Errors: `{"error": "message", "code": "not_found|conflict|invalid|forbidden|unauthorized|internal", "details": [...]}`.

| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/api/v1/info` | | `{version, instance, queue, storage, providers:[...], runtimes:[...], defaultProvider, grafanaUrl}` |
| GET | `/api/v1/graphs` | | `GraphSummary[]` |
| POST | `/api/v1/graphs` | `Graph` (JSON, or YAML with `Content-Type: application/yaml`) | `GraphSummary` (201) |
| POST | `/api/v1/graphs/validate` | `Graph` | `{valid, errors:[{path,message}], warnings:[{path,message}]}` |
| GET | `/api/v1/graphs/{id}` `?version=N` | | `GraphSummary` |
| PUT | `/api/v1/graphs/{id}` (header `If-Match: <version>` optional) | `Graph` | `GraphSummary` (409 on version conflict) |
| DELETE | `/api/v1/graphs/{id}` | | 204 (409 if running) |
| GET | `/api/v1/graphs/{id}/versions` | | `[{version, createdAt, author}]` |
| GET | `/api/v1/graphs/{id}/export` `?format=yaml|json` | | the document |
| POST | `/api/v1/graphs/{id}/start|stop|pause|resume|kill` | | `GraphStatus` |
| GET | `/api/v1/graphs/{id}/status` | | `GraphStatus` |
| POST | `/api/v1/graphs/{id}/nodes/{node}/start|stop|pause|resume|kill` | | `GraphStatus` |
| PUT | `/api/v1/graphs/{id}/nodes/{node}/scale` | `{replicas}` | `GraphStatus` |
| GET | `/api/v1/graphs/{id}/nodes/{node}/chat` | | `OperatorMessage[]` |
| POST | `/api/v1/graphs/{id}/nodes/{node}/chat` | `{text, replica?}` (replica -1/absent = any) | `OperatorMessage` |
| POST | `/api/v1/graphs/{id}/channels/{channel}/messages` | `{type, payload, target?, metadata?}` | `Message` (201; held if channel has approval) |
| GET | `/api/v1/graphs/{id}/messages` `?channel=&node=&correlationId=&status=&limit=` | | `MessageRecord[]` (newest first) |
| GET | `/api/v1/messages/{messageId}` | | `MessageRecord[]` |
| GET | `/api/v1/graphs/{id}/executions` `?node=&status=&limit=` | | `Execution[]` |
| GET | `/api/v1/executions/{id}` | | `Execution` |
| GET | `/api/v1/graphs/{id}/events` `?after=&node=&types=a,b&limit=` | | `Event[]` (ascending by seq) |
| GET | `/api/v1/events/stream` `?graph=&after=` | | SSE: `id: <seq>`, `event: <type>`, `data: <Event JSON>` |
| GET | `/api/v1/approvals` `?graph=&status=pending` | | `Approval[]` |
| POST | `/api/v1/approvals/{id}/approve|reject` | `{comment?, answer?}` | `Approval` |
| GET | `/api/v1/providers` | | `[{name, type, baseUrl, defaultModel, default}]` |
| GET | `/api/v1/providers/{name}/models` | | `ModelInfo[]` |
| GET | `/api/v1/tools` | | `[{name, capability, description, schema}]` |
| GET | `/api/v1/graphs/{id}/files` `?path=&node=&replica=` | | `[{name, path, dir, size, modTime}]` |
| GET | `/api/v1/graphs/{id}/files/content` `?path=&node=&replica=` | | file bytes (text/plain) |
| GET | `/healthz`, `/readyz`, `/metrics` | | |

```
GraphSummary = { graph: Graph, record: GraphRecord }
GraphStatus  = {
  graphId, state, desiredState, runId, version,
  nodes: [{ nodeId, name, state, desiredReplicas, replicas: ReplicaStatus[], pending, disabled }],
  channels: [{ channelId, type, from, to: [..], subject,
               groups: [{ group, node, replica?, pending, inFlight, dead }] }],
  approvalsPending
}
```

Node `state` in GraphStatus is the "most alive" replica state, in the
order running > awaiting_approval > starting > idle > paused > stopping >
failed > completed > stopped.

## 2. Runtime Protocol v1 (external agents)

An external runtime is an HTTP server (any language) exposing:

* `GET /v1/health` → `{"status":"ok","agents":["planner", ...],"framework":"..."}`
* `POST /v1/turn` body `TurnInput` → `TurnResult`. The agent is chosen by
  `TurnInput.agent`. Non-2xx = turn failed (inputs are redelivered).

During a turn the agent reaches the platform ONLY through
`TurnInput.callback.url` with header `Authorization: Bearer <callback.token>`:

| Method | Path (relative to callback.url) | Body | Response |
|---|---|---|---|
| POST | `/tools/{toolName}` | tool arguments object | `{content, data?, isError?, truncated?}` (403 when denied) |
| GET | `/tools` | | `[{name, description, parameters}]` (granted tools only) |
| POST | `/llm/chat` | `ChatRequest` (`model` optional: node default) | `ChatResponse` |
| POST | `/emit` | `Emit` | `{"queued": true}` |
| GET | `/state/{key}` | | raw bytes (404 if absent) |
| PUT | `/state/{key}` | raw bytes | 204 |
| POST | `/output` | `{text}` | 204 |
| POST | `/ask` | `{question}` | `{answer}` (blocks until a human answers) |

callback.url looks like `http://weftd:8080/internal/v1/executions/<executionId>`.
The token is valid only while the execution is running.

## 3. Turn semantics

* The engine claims up to `loop.batchSize` input messages, builds
  `TurnInput`, calls the runtime, then: publishes `TurnResult.emits` (plus
  anything emitted via callback) through the router, acks the inputs, and
  persists the execution. If the turn fails or returns `retry: true`, inputs
  are nacked with exponential backoff and redelivered (dead-lettered after
  `maxDeliver`).
* Emits are routed: `channel` set → that channel; else `target` set →
  outgoing channels whose consumers include target; else every outgoing
  channel whose `types` filter and `when` expression match. No match → the
  message is recorded as `dropped`.
* `signal: "no_further_work"` with `stop.policy: auto` arms the stop
  handshake; the next turn has `pendingStop: true` and must either do work
  or return `signal: "confirm_stop"`, which completes the replica.
* `broadcast` (ring clusters) is stored and shown to the two ring
  neighbours next turn as `neighbors`.

## 4. Native platform tools

Native agents get, in addition to their granted gateway tools, four
platform tools that need no grant: `emit {type, payload, target?, channel?}`,
`state_get {key}`, `state_set {key, value}`, `finish {signal, summary?, broadcast?}`.
Wire names use `__` for `.` (see `llm.WireToolName`).

### Delegation through the execution callback

Runtime Protocol v1 external parents may invoke `delegation.request` through the
existing tool callback with only `template` and `objective`. An exact grant and
managed model are required. The request waits for the installation's approval
policy and a server-managed native child result. Error envelopes, callback expiry,
cooperative cancellation and gateway admission limits apply. Do not retry uncertain
requests: no delegation idempotency key is defined. External child process launch
and model/runtime overrides are unsupported.

Gateway model usage and tool attempts are counted by the server. `TurnResult.usage`
and `toolCalls` represent only runtime-local work, never mirrored callback totals.
The Python SDK follows this contract; `ctx.delegate` returns a `ToolResult`, while
`ctx.add_usage` remains available for genuinely local inference.
