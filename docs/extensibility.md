# Extensibility

Weft is built around a handful of small interfaces. Every vendor- or
technology-specific piece sits behind one of them, so adding a backend never
touches the engine. The composition root is `server/internal/app/app.go`: it
is the only place that knows which concrete implementations exist.

| Extension | Interface | Registered in | Conformance |
|---|---|---|---|
| Model provider | `llm.Provider` | `llm.RegisterPlugin` with a manifest in an `init()` + blank import in `llm/providers` | httptest-style tests |
| Message broker | `queue.Broker` | `queue/brokers.Open` switch | `queue/conformance.Run` |
| Storage backend | `storage.Store` | `storage/stores.Open` switch | `storage/conformance.Run` |
| Agent runtime | `runtime.Runtime` | `runtimes` map in `app.New` | engine tests |
| Tool | `tools.Tool` | `tools.Registry.Register` | tools tests |
| Command sandbox | `tools.Sandbox` + `engine.SandboxFactory` | `sandboxFactory` in `app.go` | — |
| Agent in any language/framework | Runtime Protocol v1 (HTTP) | graph YAML (`runtime.kind: external`) | `sdk/python/tests` as reference |

## Add a model provider

One interface; `ChatRequest`/`ChatResponse` already carry tools, tool calls,
JSON mode and usage. Platform tool names contain dots (`filesystem.read`):
convert with `llm.WireToolName` / `llm.PlatformToolName` for vendors that
restrict function names.

```go
package acme // server/internal/llm/acme

import (
	"context"

	"github.com/woliveiras/weft/server/internal/llm"
)

func init() {
	llm.RegisterPlugin(llm.Manifest{
		ID: "acme", Label: "Acme", DefaultBaseURL: "https://api.acme.example", Key: llm.KeyRequired,
		Capabilities: llm.Capabilities{Tools: true, JSON: llm.JSONNative, Usage: true, ModelListing: true},
	}, func(cfg llm.ProviderConfig) (llm.Provider, error) {
		key, err := cfg.APIKey() // from apiKeyEnv / apiKeyFile
		if err != nil {
			return nil, err
		}
		return &provider{cfg: cfg, key: key}, nil
	})
}

type provider struct {
	cfg llm.ProviderConfig
	key string
}

func (p *provider) Name() string { return p.cfg.Name }

func (p *provider) Chat(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	// 1. map req.System, req.Messages (incl. ToolCalls / RoleTool results), req.Tools, req.JSON
	// 2. POST to the vendor (reuse the retrying JSON helper in llm/httpx.go)
	// 3. map back: Message{Role: assistant, Content, ToolCalls}, StopReason, Usage
	return &llm.ChatResponse{}, nil
}

func (p *provider) Models(ctx context.Context) ([]llm.ModelInfo, error) { return nil, nil }
```

Add `_ ".../llm/acme"` to `server/internal/llm/providers/providers.go`; an
administrator then enables the Acme plugin and adds a provider in the console.
The registry
adds metrics, tracing (`gen_ai.*` attributes) and the per-provider
concurrency limit for free. OpenAI-compatible backends need no code at all:
`openai.New(cfg, openai.Options{...})` is reused by `openrouter` and
`huggingface`.

### Provider conformance suite

Every built-in plugin passes `llm/conformance`, which drives the provider only
through `llm.Provider` against a scripted fake backend speaking its wire
protocol (`conformance.OpenAI`, `conformance.Anthropic`, `conformance.Ollama`).
It checks text answers and usage, a structured tool call and the tool result
sent back, JSON mode (parameter or instruction), model listing, a 401 that is
neither retried nor leaks the credential, a retried 503, cancellation, the
per-provider timeout, and that the credential travels only in its header.
`TestBuiltinPluginsConform` fails when a plugin has no wire mapping, so a new
plugin cannot ship without it; a new wire protocol adds a `Wire` value.

`conformance.RunLive` holds a real backend to what agents need (an answer
with usage, a structured tool call, valid JSON, the model listed,
cancellation). It runs against a real Ollama only when asked:

```bash
WEFT_TEST_OLLAMA_URL=http://host.docker.internal:11434 WEFT_TEST_OLLAMA_MODEL=llama3.2:latest \
  ./scripts/go.sh test ./internal/llm/conformance -run TestOllamaLive -v
```

### Out-of-process provider plugins

Installing a provider without rebuilding Weft is designed in
[Provider Protocol v1](provider-protocol.md) (stdio JSON-RPC, manifest with
checksum and declared egress, its own credential only, conformance before it
can be enabled). It is not implemented yet.

## Add a message broker

Implement `queue.Broker` (`Declare`, `DeleteGroup`, `Publish`, `Subscribe`,
`Stats`, `Ping`, `Close`), a pull `Subscription.Fetch`, and `Delivery`
(`Ack`, `Nack(delay)`, `Term(reason)`, `Extend`, `Attempt`). The model is
**subjects with consumer groups**: every declared group receives every
message; within a group each message is leased to one consumer, redelivered
when the lease (`AckWait`) expires, and dead-lettered after `MaxDeliver`.
All channel types (queue, topic, fanout, broadcast) are expressed with that
primitive by the engine — a broker never needs to know about them.

```go
package mq // server/internal/queue/mq

type Broker struct{ /* client */ }

func New(ctx context.Context, url string) (*Broker, error) { /* connect */ }

func (b *Broker) Name() string { return "mq" }
func (b *Broker) Declare(ctx context.Context, cfg queue.SubjectConfig) error { cfg = cfg.WithDefaults(); /* create stream + groups idempotently */ return nil }
func (b *Broker) Publish(ctx context.Context, subject string, m *domain.Message) error { /* durable append; queue.ErrNoGroups if undeclared */ return nil }
func (b *Broker) Subscribe(ctx context.Context, subject, group, consumer string) (queue.Subscription, error) { ... }
// DeleteGroup, Stats, Ping, Close ...
```

Register it in `queue/brokers/brokers.go`:

```go
case "mq":
	return mq.New(ctx, cfg.URL)
```

and prove it with the shared suite:

```go
func TestConformance(t *testing.T) {
	url := os.Getenv("WEFT_TEST_MQ_URL")
	if url == "" {
		t.Skip("set WEFT_TEST_MQ_URL")
	}
	conformance.Run(t, func(t *testing.T) queue.Broker { b, _ := mq.New(context.Background(), url); return b })
}
```

`conformance.RunWith` relaxes checks a broker genuinely cannot meet (document
them in the package comment).

## Add a storage backend

Implement `storage.Store` (graphs with versions and optimistic concurrency,
records, events with monotonic `Seq`, message history, dedupe via
`MarkProcessed`, executions, approvals with atomic `DecideApproval`,
operator messages with consume-once, replica status, KV, leases). A SQL
database is usually a new `sqlstore.Dialect` rather than a new store. Register
it in `storage/stores.Open` and run `storage/conformance.Run` — it includes
the race tests (single lease winner, single approval decision) that the
engine's correctness depends on.

## Add an agent runtime

A runtime turns one `TurnInput` into one `TurnResult`; the engine owns
everything else (claiming, acking, retries, routing, stop policies).

```go
type Runtime interface {
	Kind() domain.RuntimeKind
	Turn(ctx context.Context, in *runtime.TurnInput, env runtime.TurnEnv) (*runtime.TurnResult, error)
	Prepare(ctx context.Context, rc runtime.ReplicaContext) error // replica start
	Release(ctx context.Context, rc runtime.ReplicaContext) error // replica stop
}
```

`TurnEnv` is the only door out: `InvokeTool` (gateway, under the node's
grants), `Chat` (LLM gateway, node's model), `Emit`, `StateGet/StatePut`,
`Output`, `AskHuman`. Add a `domain.RuntimeKind` constant and an entry in the
`runtimes` map in `app.New`. In most cases you do **not** need a new Go
runtime: write an external agent instead (below).

## Add a framework adapter (Python)

`sdk/python/weft/adapters/` contains LangGraph, AutoGen and CrewAI
adapters. Each is a subclass of `weft.Agent` that builds the framework
object per turn and bridges three things: the model (a framework chat-model
wrapper over `ctx.llm`), tools (framework tools wrapping `ctx.tools.call`,
which enforces grants on the platform side), and outputs (`ctx.emit`).

```python
from weft import Agent, serve
from weft.adapters.langgraph import LangGraphAgent, weft_tools_for_langchain

def build(ctx):
    from langgraph.graph import StateGraph, END
    g = StateGraph(dict)
    g.add_node("analyse", lambda s: {**s, "verdict": "approved"})
    g.add_node("decide", lambda s: {**s, "emits": [{"type": "review.approved", "payload": {"ok": True}}],
                                     "summary": "approved"})
    g.add_edge("analyse", "decide"); g.add_edge("decide", END)
    g.set_entry_point("analyse")
    return g.compile()

serve([LangGraphAgent("reviewer", build)], port=8000)
```

A new framework follows the same shape; import it lazily so the SDK stays
dependency-free (`weft.adapters.require`).

## Write an agent (Python SDK)

```python
from weft import Agent, serve

class Summariser(Agent):
    name = "summariser"

    def turn(self, ctx):
        for m in ctx.inputs:
            text = ctx.llm.complete(f"Summarise:\n{m.payload}")
            ctx.tools.call("filesystem.write", path=f"summaries/{m.id}.md", content=text)
            ctx.emit("summary.ready", {"source": m.id, "summary": text})
        return f"summarised {len(ctx.inputs)} message(s)"

serve([Summariser()], port=8000)
```

```yaml
nodes:
  - id: summariser
    runtime: {kind: external, agent: summariser, endpoint: http://summariser:8000}
    tools: [{name: filesystem.write, paths: ["summaries/**"]}]
```

`weft.testing` provides an in-memory fake platform to unit-test agents
(scripted LLM replies, granted/denied tools, recorded emits).

## Write an agent in any other language

Implement Runtime Protocol v1 (`docs/dev/contracts.md` §2): `GET /v1/health`
and `POST /v1/turn`. During the turn, call back `input.callback.url` with
`Authorization: Bearer <input.callback.token>` for tools, the model, emits and
state. Example in TypeScript (Node 20+, no dependencies):

```ts
import { createServer } from "node:http";

const cb = async (input: any, path: string, body?: unknown) => {
  const r = await fetch(input.callback.url + path, {
    method: "POST",
    headers: { Authorization: `Bearer ${input.callback.token}`, "Content-Type": "application/json" },
    body: JSON.stringify(body ?? {}),
  });
  if (r.status === 403) throw new Error("permission denied");
  return r.json();
};

createServer(async (req, res) => {
  if (req.method === "GET" && req.url === "/v1/health") {
    res.end(JSON.stringify({ status: "ok", agents: ["translator"], framework: "custom" }));
    return;
  }
  let raw = ""; for await (const c of req) raw += c;
  const input = JSON.parse(raw);                      // TurnInput
  const emits = [];
  for (const m of input.inputs) {
    const out = await cb(input, "/llm/chat", { messages: [{ role: "user", content: `Translate to French: ${JSON.stringify(m.payload)}` }] });
    emits.push({ type: "translation.ready", payload: { source: m.id, text: out.message.content } });
  }
  res.setHeader("Content-Type", "application/json");
  res.end(JSON.stringify({ emits, summary: `translated ${input.inputs.length}` })); // TurnResult
}).listen(Number(process.env.PORT ?? 8000));
```

If `WEFT_RUNTIME_SECRET` is configured, reject `/v1/turn` requests whose
`X-Weft-Runtime-Secret` header does not match.

## Add a tool

```go
type Tool interface {
	Name() string            // invocation name, e.g. "jira.create_issue"
	Capability() string      // permission, e.g. "jira.write"
	Description() string     // shown to models
	Schema() json.RawMessage // JSON Schema of the arguments object
	Invoke(ctx context.Context, inv *tools.Invocation) (*tools.Result, error)
}
```

Register it on the registry built in `app.New`
(`a.Tools.Register(myTool)`). The gateway already authorises by capability
or name (globs supported), applies rate limits and approvals, audits and
meters every call. Inside `Invoke`, honour the grant's constraints with the
policy helpers: `tools.ResolvePath(inv.Workdir, p)` + `tools.CheckPath(inv.Grant, rel)`
for files, `tools.CheckCommand` for commands, `tools.CheckHost` for network
destinations, and run processes only through `inv.Sandbox`. Return failures
as `&tools.Result{IsError: true, Content: ...}` so the model can recover.

An MCP bridge is a tool provider that registers one `Tool` per MCP tool with
names `mcp.<server>.<tool>` and capability `mcp.<server>` — matching the grants
the graph document format.

## Add a sandbox

```go
type Sandbox interface {
	Kind() string
	Exec(ctx context.Context, req tools.ExecRequest) (*tools.ExecResult, error)
	Close() error
}
```

Return it from the `engine.SandboxFactory` in `app.go`, keyed on
`spec.Node.Isolation.Mode`. A Kubernetes sandbox would create one pod per
replica (workspace PVC mounted, restricted security context, optionally a
gVisor/Kata `runtimeClassName`) and implement `Exec` via the pod `exec`
subresource.
