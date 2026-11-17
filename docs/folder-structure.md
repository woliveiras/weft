# Folder structure

```
weft/
├── README.md, LICENSE, NOTICE, CONTRIBUTING.md, Makefile
├── docker-compose.yml          whole platform + observability (+ profiles: ollama, hf, sandbox)
├── Dockerfile                  weftd + weft + web console (multi-stage, non-root)
├── Dockerfile.sandbox          default image for docker-isolated tool sandboxes
├── api/                        OpenAPI specification of the public API
├── schemas/                    JSON Schemas: message envelope, events, graph documents
├── scripts/go.sh               run the Go toolchain in a container (only Docker required)
│
├── server/                     Go module github.com/woliveiras/weft/server
│   ├── cmd/
│   │   ├── weftd/             control plane binary (API, engine, gateways, console)
│   │   └── weft/              CLI (graphs, lifecycle, inject, approvals, events, k8s render)
│   └── internal/
│       ├── domain/             vocabulary: Graph, Node, Channel, Message, Execution, Approval,
│       │                       Event; state machines (graph, replica, execution, message)
│       ├── graph/              normalisation, validation, routing (`when` via expr), schedules,
│       │                       channel types → consumer groups
│       ├── engine/             reconciler, leases, replica loops, turns, publishing, gates,
│       │                       status, ring comms, callback registry
│       ├── runtime/            Runtime interface, TurnInput/TurnResult/TurnEnv
│       │   ├── native/         built-in LLM tool-calling agent
│       │   ├── echo/           model-free forwarding runtime (dry runs, tests)
│       │   └── external/       Runtime Protocol v1 client + process/docker launchers
│       ├── queue/              Broker interface (subjects × consumer groups, leases)
│       │   ├── natsq/ redisq/ rabbitq/ kafkaq/ fsq/ memory/   adapters
│       │   ├── brokers/        Open(config) → Broker
│       │   └── conformance/    shared adapter test suite
│       ├── storage/            Store interface
│       │   ├── sqlstore/       SQLite + PostgreSQL (dialects, embedded migrations)
│       │   ├── fsstore/        filesystem store
│       │   ├── stores/         Open(config) → Store
│       │   └── conformance/    shared adapter test suite
│       ├── llm/                Provider interface, registry (metrics, tracing, concurrency), HTTP helpers
│       │   ├── openai/ anthropic/ ollama/ openrouter/ huggingface/ mock/
│       │   └── providers/      imports every provider
│       ├── tools/              tool registry, default-deny policy, gateway, sandboxes (process,
│       │                       docker), builtin tools (filesystem, shell, git, http, human)
│       ├── hitl/               approvals (blocking waits, decisions, expiry, hooks)
│       ├── events/             durable event bus (append, then wake tailing subscribers)
│       ├── workspace/          graph workspaces, git init/clone, per-replica worktrees
│       ├── api/                REST API, SSE stream, auth (tokens, OIDC), runtime callback API
│       ├── observability/      Prometheus metrics, OTel tracing/logging setup, trace propagation
│       ├── config/             configuration loading (file + env)
│       ├── app/                composition root (wires config to adapters)
│       └── deps/               dependency pinning (build tag, never compiled)
│
├── sdk/python/                 Python Agent SDK `weft` (stdlib only)
│   ├── weft/                types, context, agent base, server, callback client, testing harness
│   │   └── adapters/           LangGraph, AutoGen, CrewAI adapters (lazy imports)
│   └── tests/
├── agents/                     example agents (planner, researcher, developer, reviewer, qa)
│   ├── weft_agents/
│   ├── tests/
│   └── Dockerfile
├── web/                        React + TypeScript console (Vite, @xyflow/react)
│   └── src/{api,components,hooks,mock}
│
├── examples/
│   └── graphs/                 software-factory, hello-echo
│
├── deploy/
│   ├── config/weft.yaml       container default configuration
│   ├── observability/          otel-collector, prometheus (+ alerts), tempo, loki, grafana provisioning + dashboard
│   └── kubernetes/             kustomize manifests (+ optional ServiceMonitor/PrometheusRule)
│
└── docs/
    ├── architecture.md, security.md, …     design documentation
    ├── getting-started.md, configuration.md, deployment.md, production-hardening.md
    ├── extensibility.md, folder-structure.md
    ├── adr/                    architecture decision records
    ├── diagrams/               diagram sources
    └── dev/contracts.md        cross-component contracts (API, Runtime Protocol, turn semantics)
```

## Dependency rules

- `domain` imports nothing internal. Interfaces (`queue`, `storage`, `llm`,
  `tools`, `runtime`) import only `domain` (and `llm` for tool schemas).
- Adapters import their interface package, never the engine.
- `engine` depends on interfaces only; `app` is the only package importing
  concrete adapters.
- `api` depends on `engine` and interfaces; it never touches brokers'
  delivery semantics directly.
