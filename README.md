# Weft

<img src="docs/brand/weft-lockup.svg" alt="Weft" height="56">

**An open-source, vendor-neutral Agent Factory.** Autonomous AI agents run as a
graph of continuously running loops, exchange work through durable queues, and
operate under enforced tool permissions — on a laptop, in a datacenter, or on
any cloud, with no dependency on a specific cloud provider, model vendor or
agent framework.

Weft combines pluggable brokers, storage and model providers with a
framework-agnostic runtime protocol, durable message delivery, observability,
and human approval gates. Release 1 is in progress; the
[specification index](specs/README.md) tracks delivered capabilities and
production blockers.

## Features

| Capability | How Weft provides it |
|---|---|
| **Graph of agents** | Nodes are agents, edges are durable channels. Cycles (review → rework loops), parallel branches and **dynamic branching** (`when:` expressions, type filters, per-message `target`) are first-class. Graph documents are versioned YAML/JSON and can be edited while running. |
| **Visual graph editor** | Add and remove agents, connect channels, edit channel routing, arrange nodes, validate, save and discard changes from the canvas. YAML remains available for advanced graph settings. |
| **Agent loops** | Every node runs `once`, `continuous`, `scheduled` (cron / `@every`) or `event`-triggered (wait for work). Stop policies: `never`, `auto` (two-turn "no further work" handshake), `iterations`, `duration`, `idle`. |
| **Message passing** | One envelope `{id, type, source, target, payload, metadata}` with correlation/causation ids, hop counter and W3C trace context in metadata. |
| **Queue semantics** | `queue` (point-to-point, competing consumers), `topic` (pub-sub by name, wildcard subscriptions), `fanout` (copy per target node), `broadcast` (copy per replica). |
| **Clusters** | N replicas per node, fixed or **autoscaled** on backlog, isolated or **ring** communications. Replicas compete over the same queue. |
| **Tool permissions** | Default-deny capability grants (`filesystem.read`, `shell.exec`, `git.write`, `http.fetch`, …) narrowed by paths, commands, hosts and rate limits; optional per-tool human approval. Enforced in a gateway the agent cannot bypass. |
| **Isolation** | Local process execution (same OS permissions as the platform user), Docker sandbox per replica (no network by default), Kubernetes deployment; per-replica git worktrees. See [execution modes and security](#execution-modes-and-security). |
| **Persistence & recovery** | Graphs, versions, executions, messages, approvals, state, events and leases are stored in SQLite / PostgreSQL / filesystem. Unacked messages are redelivered; a crashed instance's replicas are taken over when its leases expire. |
| **Pluggable everything** | Brokers: NATS JetStream (default), Redis Streams, RabbitMQ, Kafka, filesystem, memory. Storage: SQLite, PostgreSQL, filesystem. Models: Ollama, vLLM, OpenAI-compatible, Anthropic-compatible, OpenRouter, HuggingFace TGI, offline mock. Runtimes: native Go agent, echo, external (LangGraph, AutoGen, CrewAI, custom — any language). |
| **Observability** | Prometheus metrics, OpenTelemetry traces that follow a message across agents and brokers, OTLP logs, Grafana dashboard and alerts, full message history with causal chains, live event stream, graph visualisation. |
| **Human in the loop** | Approval gates on channels, turns, outputs and tools; agents can ask a human a question; pause/resume of graphs and nodes; operator chat with running agents; message injection. |
| **Workspace inspection** | Browse graph workspaces and read agent-produced files in the console; user-side workspace editing and uploads are not exposed. |

## Quickstart

Choose an installation path:

- [Docker Compose](docs/getting-started.md#1-start-the-platform): bootstrap
  credentials, start the services and open the console. On macOS, see
  [Colima setup](docs/getting-started.md#macos-setup-with-colima) if needed.
- [Native bundle](docs/standalone.md#initial-native-bundle): build, initialize
  a private profile and run without Docker.

Example workflows are initially stopped. Follow the
[model setup](docs/getting-started.md#3-use-a-real-model) before running real
work, or explicitly enable the development mock for synthetic runs.
See [console navigation](docs/interface-navigation.md) for Tasks, Projects,
Workflows, Models and Approvals.

## Execution modes and security

Platform deployment and agent isolation are separate choices. Commands execute
where `weftd` and its execution backend run. `process` uses the platform user's
OS permissions; it is not a security sandbox. Compose alone does not isolate
commands from the control-plane container. `docker` runs commands in separate
containers on the dedicated sandbox daemon.

See [isolation modes](docs/execution-model.md#8-isolation-modes),
[sandbox setup and limitations](docs/sandbox-runtime.md), and the
[security model](docs/security.md). Browser validation uses the
[human review and preview gateway](docs/sandbox-runtime.md#human-review-and-application-previews).

## Architecture

The engine reconciles desired state from storage, leases replicas, and routes
messages through durable queues. Runtimes reach tools and models through the
control plane's gateways. See the [architecture overview](docs/architecture.md)
and [C4 diagrams](docs/diagrams/c4.md) for component boundaries and deployment views.

## Repository layout

See the [repository map](docs/folder-structure.md) for the Go server, React
console, Python SDK, example agents, deployment files and specifications.

## Documentation

- [Documentation index](docs/README.md) — guides and references grouped by task
- [Contributing](CONTRIBUTING.md) — toolchains, development and validation
- [Specification index](specs/README.md) — acceptance criteria, status and evidence
- [Architecture decisions](docs/adr/README.md) — decisions and their rationale

## Status and limitations

See the [architecture limitations](docs/architecture.md#7-known-limitations-and-roadmap)
for runtime boundaries and the [production readiness specifications](specs/README.md#production-readiness)
for outstanding qualification. Historical test results describe their recorded
revision and environment; they do not establish readiness for every deployment.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE). Weft is an
independent open-source project.
