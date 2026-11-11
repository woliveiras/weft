# Runtime Protocol v1

The Runtime Protocol is how agents written in **any language or framework** (LangGraph, AutoGen,
CrewAI, plain code) take part in a Weft graph. It is two HTTP endpoints on the agent side and
eight callback endpoints on the platform side. The Python SDK (`sdk/python`, package `weft`)
implements it; this page is the specification for everyone else.

Schemas: [`schemas/turn.schema.json`](../schemas/turn.schema.json) (TurnInput, TurnResult, Emit),
[`schemas/message.schema.json`](../schemas/message.schema.json), and the `runtime-callback` tag of
[`api/openapi.yaml`](../api/openapi.yaml). Go source of truth: `server/internal/runtime/runtime.go`.

## Division of responsibility

| The engine (platform) | The runtime (your agent) |
|---|---|
| Decides when a turn starts (loop mode, schedule, stop policy, pause, approvals) | Turns `TurnInput` into a `TurnResult` |
| Claims input messages, extends their leases, acks/nacks/dead-letters them | Never touches the broker |
| Routes and publishes emitted messages | Says *what* to emit (type, payload, optional target/channel) |
| Executes tools under the node's grants (tool gateway) | Asks for tools through the callback |
| Holds model credentials; calls providers (LLM gateway) | Asks for completions through the callback |
| Persists memory (KV), executions, history, events | Reads/writes memory through the callback |

Consequences: an agent process holds **no secrets**, can be shared by any number of replicas (the
protocol is stateless per turn), and cannot break delivery guarantees by being buggy.

## Agent-side endpoints

### `GET /v1/health`

`200 {"status":"ok","agents":["planner","reviewer"],"framework":"custom"}`. The engine polls it
(every 1 s, 3 s timeout) when a replica starts: for 30 s for `runtime.endpoint` services, 90 s for
runtimes it launched itself. Any non-200 counts as not ready.

### `POST /v1/turn`

Request body: `TurnInput`. Response: `200` with `TurnResult`. Dispatch on `TurnInput.agent`
(the node's `runtime.agent`, default the node id).

| Header (request) | Meaning |
|---|---|
| `Content-Type: application/json` | always |
| `X-Weft-Runtime-Secret` | sent when the platform has `runtimes.sharedSecretEnv` set; reject turns without the right value (constant-time compare) |
| `traceparent` / `tracestate` | W3C trace context of the `agent.turn` span; continue it to see your spans inside the platform trace |

#### TurnInput (abridged)

| Field | Type | Meaning |
|---|---|---|
| `executionId` | string | Id of this turn; also in the callback URL |
| `graphId`, `graphName`, `nodeId`, `nodeName` | string | Where you are |
| `replica`, `replicas` | int | Your index and the configured cluster size |
| `iteration` | int | Lifetime turn counter of this replica (survives restarts) |
| `agent` | string | Agent to dispatch to |
| `prompt` | string | Node prompt with `@parameters` substituted |
| `system` | string | Platform preamble (situation, inputs, outputs, tools, stop protocol) — prepend it to your model prompt |
| `inputs` | Message[] | Messages claimed for this turn (they are leased to you) |
| `operatorMessages` | string[] | Human messages addressed to this replica |
| `neighbors` | `{replica,text,at}[]` | Ring clusters: latest broadcast of the two neighbours |
| `outputs` | `{channel,type,targets,types?,when?}[]` | Where emits can go |
| `tools` | `{name,description,parameters}[]` | Tools you are granted (JSON Schema parameters) |
| `maxToolCalls` | int, optional | The node's tool-call budget for this turn (`limits.maxToolCalls`); the gateway enforces it, use it to size your own loop |
| `model` | `{provider,name,temperature?,maxTokens?}` | The node's model reference |
| `pendingStop` | bool | Last turn declared `no_further_work`: find work or return `confirm_stop` |
| `workdir`, `branch` | string | The replica's workspace (as the *platform's* tools see it) and git branch |
| `parameters`, `config` | object | Graph parameters; node `config` verbatim |
| `callback` | `{url, token}` | How to reach the platform during this turn |

#### TurnResult

| Field | Type | Meaning |
|---|---|---|
| `emits` | Emit[] (≤ 1000 per turn, with callback emits) | Output messages; published only if the turn succeeds |
| `summary` | string | Shown in the console and stored on the execution (≤ 4000 chars kept) |
| `broadcast` | string | Ring clusters: the line neighbours read next turn |
| `signal` | `""` \| `no_further_work` \| `confirm_stop` | Auto-stop handshake (nodes with `stop.policy: auto`) |
| `usage` | `{inputTokens, outputTokens}` | Tokens you spent *outside* the LLM callback (callback usage is counted already) |
| `toolCalls` | int | Tool calls made outside the callback (informational) |
| `retry` | bool | "Not now": the inputs are nacked and redelivered with back-off instead of acked |

`Emit = {type, payload, target?, channel?, metadata?}`. Routing: `channel` forces one of the node's
outputs; otherwise `target` narrows to outputs reaching that node; otherwise every output whose
`types` filter and `when` condition match. Reserved metadata keys (`graph`, `hops`, `traceparent`,
…) are ignored.

## Callback endpoints

Base URL: `TurnInput.callback.url` (e.g. `http://weftd:8080/internal/v1/executions/exe_…`).
Every request: `Authorization: Bearer <callback.token>`.

| Method & path | Body → Response | Errors |
|---|---|---|
| `GET /tools` | → `ToolSchema[]` (granted only) | |
| `POST /tools/{name}` | tool args object → `{content, data?, isError?, truncated?}` | 403 denied/rejected, 404 unknown tool, 429 rate limited. Tool *failures* are `200` with `isError: true` |
| `POST /llm/chat` | `ChatRequest` (omit `model` for the node's) → `ChatResponse` | 502 `llm_error` (provider failure, token budget exhausted) |
| `POST /emit` | `Emit` → `{"queued":true}` | 400 invalid emit |
| `GET /state/{key}` | → raw bytes | 404 absent |
| `PUT /state/{key}` | raw bytes (≤ 1 MiB) → 204 | |
| `POST /output` | `{text}` → 204 | |
| `POST /ask` | `{question}` → `{answer}` (blocks until a human answers) | 409 `no_answer` (rejected/expired) |

State keys are URL path-escaped (they may contain `/` and `:`); state is shared by **all replicas
of the node** (namespace `<graph>/<node>`), which is what a cluster needs for shared counters.

### Token scoping

The token is 192 random bits, created when the turn starts and destroyed when it ends. It
authorises exactly: tools under this node's grants, the node's model (any model name on the node's
provider), emits into this turn, this node's state, narration and questions for this replica.
A token for a finished or unknown execution gets `401`. Tokens are held in memory by the instance
running the turn, so `callback.url` always points at that instance (`server.internalUrl`).

## Timeouts, failures and retries

| Situation | Outcome |
|---|---|
| Turn exceeds `limits.turnTimeout` (default 30 m) | Request cancelled; turn **failed**; inputs nacked with back-off |
| Non-2xx from `/v1/turn`, connection error, invalid JSON | Turn **failed**; inputs nacked with exponential back-off (`retryBackoff × 2^(attempt-1)`, ≤ 5 m) |
| Output construction or publication fails after the runtime returns | Turn **failed**; stop remaining emits; no input is acked or marked processed. Inputs are nacked with back-off, or dead-lettered at `maxDeliver`. Outputs already published are not rolled back. |
| `retry: true` | Same as failed, without counting as an error in your code path |
| Inputs reach the channel's `maxDeliver` (default 5) | Dead-lettered; visible as `dead_lettered` in history |
| Operator `kill` | Request cancelled; turn **cancelled**; inputs redelivered immediately |
| Platform crash mid-turn | Execution marked **interrupted** by whoever takes the replica over; inputs redelivered after the broker lease (`ackWait`) |

Publication success includes durable acceptance at a human approval gate; the producer does not wait for the human decision. Intentional routing drops (no matching route or hop limit) are not publication failures.

**Idempotency expectations.** Delivery is at-least-once: the same input message can reach your
agent more than once (after a failed turn, a crash, or a broker redelivery). The engine deduplicates
messages already acked by the same consumer group, but side effects you performed *during* a failed
turn (files written, external calls) are not rolled back. A publication failure can also leave
partial outputs delivered; a retried turn emits new message IDs, so downstream agents must
tolerate duplicates. Write turns so that re-running them is
safe — key outputs by input message id or `metadata.correlation_id`, overwrite rather than append.

Callbacks: `GET /tools`, `GET/PUT /state`, `POST /output` are safe to retry. `POST /tools/{name}`
and `POST /llm/chat` may have side effects/cost — retry only on connection errors before a response,
and only for idempotent tools. `POST /emit` is not idempotent (each call queues a message).

## Versioning policy

* The path prefix carries the major version (`/v1/turn`, `/internal/v1`).
* Within v1, fields are only **added**. Agents must ignore unknown fields; the platform ignores
  unknown fields in `TurnResult`.
* A breaking change creates `/v2/…`; the platform will serve both for at least one minor release.

## Minimal agent without the SDK (Node.js, no dependencies)

```js
// echo-agent.mjs — forwards every input as "<type>.seen", asks the model for a one-line summary.
import http from "node:http";

const call = (cb, method, path, body) =>
  fetch(cb.url + path, {
    method,
    headers: { Authorization: `Bearer ${cb.token}`, "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  }).then(async (r) => { if (!r.ok) throw new Error(`${path}: ${r.status}`); return r.status === 204 ? null : r.json(); });

http.createServer(async (req, res) => {
  const send = (code, obj) => { res.writeHead(code, { "Content-Type": "application/json" }); res.end(JSON.stringify(obj)); };
  if (req.method === "GET" && req.url === "/v1/health") return send(200, { status: "ok", agents: ["echo"], framework: "custom" });
  if (req.method !== "POST" || req.url !== "/v1/turn") return send(404, { error: "not found" });
  if (process.env.WEFT_RUNTIME_SECRET && req.headers["x-weft-runtime-secret"] !== process.env.WEFT_RUNTIME_SECRET)
    return send(401, { error: "bad secret" });
  let raw = ""; for await (const chunk of req) raw += chunk;
  const turn = JSON.parse(raw);
  try {
    const chat = await call(turn.callback, "POST", "/llm/chat", {
      system: turn.system,
      messages: [{ role: "user", content: `${turn.prompt}\n\n${JSON.stringify(turn.inputs.map((m) => m.payload))}` }],
    });
    await call(turn.callback, "POST", "/output", { text: chat.message.content });
    const emits = turn.inputs.map((m) => ({ type: `${m.type}.seen`, payload: { summary: chat.message.content, of: m.id } }));
    send(200, { emits, summary: `handled ${turn.inputs.length} message(s)` });
  } catch (e) {
    send(500, { error: String(e) }); // the engine nacks the inputs and retries with back-off
  }
}).listen(Number(process.env.PORT ?? 8000), process.env.HOST ?? "0.0.0.0");
```

Wire it into a graph:

```yaml
nodes:
  - id: echo
    runtime: {kind: external, agent: echo, endpoint: http://echo-agent:8000}
    # or launched by the platform: {kind: external, command: ["node", "echo-agent.mjs"]} (process isolation)
    # or {kind: external, image: my/echo-agent:1.0} with isolation: {mode: docker}
```

Launched runtimes receive `PORT`, `HOST`, `WEFT_URL` and `WEFT_RUNTIME_SECRET` in a minimal
environment (no platform secrets), are shared by all replicas of the node, and are stopped when the
last replica stops.

## Instruction snapshots

`TurnInput.instructions` is an optional immutable manifest (see [schema](../schemas/instructions.schema.json)). It is untrusted data, never permissions. The Python SDK retains it in `TurnInput.instructions`. Opted-in guidance precedes the original node prompt; operator/task text is preserved. Graphs without opt-in retain their original prompt and operator messages. See [composition, budgets and failure behavior](instructions.md).

### Yielding for durable human review

`ToolResult.yield` is an optional boolean. When true, the runtime must finish its
current turn immediately, without more tools/model calls, completion signals or
outputs. Native runtime implements this directly; external runtimes should honor
it. The Python SDK exposes the wire field as `ToolResult.yield_turn`; custom
runtime loops must check it and return without further work. This field does not
automatically interrupt an arbitrary framework loop. The engine independently rejects subsequent callbacks and discards outputs
from that turn, including outputs submitted before the gate. It acknowledges the
input and persists the pending review without holding a runtime request open.

A later turn carries `Human review result: <json>` in operator context, with
`reviewId`, `state`, `candidate`, `method`, and `comment`. Request changes requires
new work and a new review. A decision grants no merge/deploy/push permissions.
The outbox inserts its continuation message once, including across a controller
restart; side effects in a retried turn still need ordinary idempotency handling.
