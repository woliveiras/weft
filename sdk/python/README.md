# weft — Python Agent SDK

Write Weft agents in Python. The SDK implements **Runtime Protocol v1**
(see [Runtime Protocol v1](../../docs/runtime-protocol.md)): the platform's engine owns the loop (claiming
messages, acking, retries, stop conditions) and calls your agent once per
turn over HTTP. Your agent reaches tools, models, state and humans only
through the per-turn callback, so the platform's grants, approvals, audit
log and tracing apply to everything it does.

Standard library only. Python ≥ 3.10.

## Quickstart

```python
from weft import Agent, serve

class Summariser(Agent):
    name = "summariser"

    def turn(self, ctx):
        for m in ctx.inputs:
            text = ctx.llm.complete(f"Summarise: {m.payload.get('text', '')}")
            ctx.tools.call("filesystem.write", path=f"summaries/{m.id}.md", content=text)
            ctx.emit("summary.ready", {"source": m.id, "summary": text})
        return f"summarised {len(ctx.inputs)} items"

serve([Summariser()], port=8000)
```

Point a graph node at it:

```yaml
- id: summariser
  runtime: { kind: external, agent: summariser, endpoint: http://agents:8000 }
  tools: [filesystem.write]
  loop: { mode: event }
```

## The turn context

| | |
|---|---|
| `ctx.inputs` | messages claimed for this turn (`id, type, source, target, payload, metadata`) |
| `ctx.prompt`, `ctx.system` | node prompt (parameters substituted) and platform preamble |
| `ctx.operator_messages`, `ctx.neighbors`, `ctx.pending_stop` | human messages, ring-neighbour notes, stop handshake |
| `ctx.parameters`, `ctx.config`, `ctx.replica`, `ctx.replicas`, `ctx.iteration` | graph parameters, node `config`, replica info |
| `ctx.llm.chat(messages, system=, json=, tools=, model=, temperature=)` | LLM gateway; returns `ChatResponse` (`.text`) |
| `ctx.llm.complete(prompt)` / `ctx.llm.json(prompt, schema_hint=, default=)` | text / robustly parsed JSON (fences and prose stripped; `default` on failure) |
| `ctx.tools.call(name, **args)` | tool gateway; raises `PermissionDenied` (HTTP 403) when not granted |
| `ctx.tools.try_call(...)`, `ctx.tools.list()`, `ctx.tools.granted(name)` | |
| `ctx.emit(type, payload, target=, channel=, metadata=)` | queue an output; published when the turn succeeds |
| `ctx.emit_now(...)` | hand an output to the engine immediately (long turns) |
| `ctx.state.get/set/get_json/set_json` | durable agent memory |
| `ctx.output(text)` | narration for the UI log |
| `ctx.ask_human(question)` | block until an operator answers |
| `ctx.declare_done()`, `ctx.confirm_stop()` | auto-stop handshake (`no_further_work` → `confirm_stop`) |
| `ctx.broadcast(text)` | line for ring neighbours |
| `ctx.retry_later()` | nack the inputs; they are redelivered with backoff |

A returned string becomes the turn summary. Raising an exception fails the
turn (HTTP 500): the inputs are nacked and redelivered, and dead-lettered
after `maxDeliver` attempts.

## Server

`serve(agents, host, port)` exposes `GET /v1/health` and `POST /v1/turn`.
Turns run concurrently, one thread each. Settings (environment):

| Variable | Meaning |
|---|---|
| `WEFT_RUNTIME_SECRET` | when set, `/v1/turn` requires header `X-Weft-Runtime-Secret` |
| `WEFT_MAX_BODY` | max request size in bytes (default 32 MiB) |
| `WEFT_LOG_LEVEL` | log level; logs are JSON lines on stdout |

## Framework adapters

The adapters import their framework only when you use them (`pip install weft[langgraph]`,
`[autogen]`, `[crewai]`). Model and tool calls always go through the
platform gateways.

* **LangGraph** — `LangGraphAgent(name, build_graph)`: `build_graph(ctx)` returns a
  compiled graph. It is invoked with `{"inputs": [...], "prompt": ...}`; `emits` and `summary` in the
  final state are emitted and reported. Helpers: `weft_tools_for_langchain(ctx)` (granted tools as
  `StructuredTool`s) and `weft_chat_model(ctx)` (a `BaseChatModel` on the LLM gateway). Tested with
  langgraph 1.2 / langchain-core 1.6.
* **AutoGen** (AgentChat ≥ 0.4) — `AutoGenAgent(name, build)`, with `build(ctx, model_client, tools)`
  returning an agent or team with `.run(task=...)`. `weft_model_client(ctx)` implements
  `ChatCompletionClient.create` on the gateway (no structured function calling; tools are AutoGen
  `FunctionTool`s that take a single `args_json` string). Not tested against a live AutoGen install.
* **CrewAI** (≥ 0.80) — `CrewAIAgent(name, build_crew)`, with `build_crew(ctx, llm, tools)`
  returning a `Crew`. It uses `BaseLLM` (top-level or `crewai.llms.base_llm`) and `crewai.tools.BaseTool`.
  Not tested against a live CrewAI install.

## Testing agents

```python
from weft.testing import FakeBackend, message, run_agent

fb = FakeBackend(grants=["filesystem.*"], llm=['{"tasks": []}'])
res = run_agent(MyAgent(), fb, inputs=[message("feature.request", {"title": "x"})])
assert fb.files["plans/..."]
```

`FakeBackend` enforces grants (raising `PermissionDenied`), keeps an
in-memory filesystem for the `filesystem.*` tools, records `git.*` calls,
scripts `shell.exec`, LLM replies and human answers, and by default answers
LLM calls like the server's `mock` provider does.

Run the tests: `python -m unittest discover -s tests -v`

The [managed-provider validation guide](../../docs/provider-langgraph-validation.md)
shows how to run the real LangGraph adapter through the Go gateway and a
browser-configured provider, using deterministic local inference and artificial
credentials. Run `make test-e2e-providers` from the repository root after
installing the `langgraph` extra and browser prerequisites.
