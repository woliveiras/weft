# Weft

<img src="docs/brand/weft-lockup.svg" alt="Weft" height="56">

**An open-source, vendor-neutral Agent Factory.** Autonomous AI agents run as a
graph of continuously running loops, exchange work through durable queues, and
operate under enforced tool permissions — on a laptop, in a datacenter, or on
any cloud, with no dependency on a specific cloud provider, model vendor or
agent framework.

Weft provides a production-grade agent orchestration platform with
pluggable brokers, storage and model providers; a framework-agnostic runtime
protocol; at-least-once delivery with deduplication; multi-instance high
availability; OpenTelemetry tracing across agents; and human-in-the-loop gates.

```bash
docker compose up        # the whole platform, offline, on one machine
```

---

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

## Execution modes and security

Weft can run directly on your machine without Docker, in Docker Compose,
or on a remote server. **Where the platform runs and how agent commands are
isolated are separate choices.** Opening the console on your laptop does not
make commands run on that laptop: tools execute where the `weftd` server and
its configured execution backend run.

| Platform deployment | Agent command mode | Where commands execute | Security boundary |
|---|---|---|---|
| Bare metal / native process on your machine | `process` | Child processes of your local `weftd` | Same OS rights as the user running Weft; no container isolation between commands and the platform. |
| Docker Compose on your machine | `process` | Inside the `weftd` container | The container separates execution from the host, but commands share the platform's container and can access files available to its user, including mounted data. |
| Native or Compose, with a dedicated sandbox daemon configured | `docker` | A separate sandbox container per agent replica | Commands receive the replica workspace and configured caches, with resource limits and no network by default. They do not run inside `weftd`. |
| Remote server or VM | `process` or `docker` | On the remote execution infrastructure | The selected mode has the same boundaries as above. A remote host separates work from your laptop, but does not itself isolate agents from the server's data and services. |

### Native execution without a container sandbox

Run `weftd` directly on the host and select this setting on the agent node:

```yaml
isolation:
  mode: process
```

The [native bundle and private profile](docs/standalone.md#initial-native-bundle)
provide explicit initialization and foreground start without Docker. A new native
profile includes Software Factory as its first stopped workflow. Configure and test
a model in the UI, make it the default, register a repository, then choose Software
Factory in New task; creating a graph is not required. Review permissions and the
human handoff gate before real work. The existing
[manual without-Docker setup](docs/getting-started.md#5-run-without-docker) uses
SQLite and filesystem queues, so it does not require the Compose services.
Install the project's tools on that host: for a React project, Node and the
chosen package manager must be available to Weft. Commands start in the
configured workspace, inherit the platform's `PATH`, and receive a reduced
environment rather than your complete interactive shell environment.

The implementation calls this a `ProcessSandbox`, but it is **not an OS
security sandbox**. It bounds execution time and captured output and validates
the starting directory. It does not prevent an allowed program, package install
script or test from accessing other files or networks available to that OS user.
Tool grants and approval gates still apply to tool calls; they do not confine
everything a program can do after `shell.exec` starts it. Use this mode for work
you trust under a suitably restricted OS account.

### Docker execution and remote hosting

Running the platform in Compose does not automatically put every agent command
in its own sandbox. Select `isolation.mode: docker` for that boundary. The
standard Compose setup provides a dedicated Docker-in-Docker daemon; agents
do not receive the host Docker socket or access to the daemon API. Sandbox
containers are unprivileged, but the dedicated daemon itself uses
`privileged: true`. On macOS it runs inside the Docker Desktop or Colima Linux
VM. Container isolation therefore reduces exposure but is not a guarantee
against host compromise. See [sandbox runtime](docs/sandbox-runtime.md).

Network access is a separate setting: sandbox execution defaults to `none`;
approved `egress` mode allows selected HTTP/HTTPS destinations, while `bridge`
allows broader access within the daemon's firewall rules. Dependency updates
need the relevant package registries permitted by the environment's network
policy. See [project environments](docs/environments.md).

For remote installations, prefer a host or VM dedicated to Weft, keep
unrelated workloads and production credentials out of the execution environment,
and use Docker isolation for untrusted commands. Protect the console and API
with authentication, TLS and restricted network access; keep internal services
private. Remote hosting does not make an internet-exposed installation safe by
default. Follow the [security model](docs/security.md) and
[production hardening guide](docs/production-hardening.md).

### Reviewing a React application in the browser

An agent can update dependencies and run tests, lint and builds in either mode.
With native execution on your machine, you can start the development server in
a separate terminal in the same workspace and open its local URL. On a remote
host, `localhost` in your browser refers to your own computer; access needs an
explicitly configured tunnel or protected proxy. A Docker sandbox's listening
port is not automatically exposed to your browser either.

For Docker nodes, `review.request` freezes the candidate, starts an optional
HTTP application and suspends the agent until a human decides in **Approvals →
Application reviews**. The human can open a React application in the browser or
validate an API with cURL/Postman, then approve, request changes or reject it.
The agent cannot publish completion while that gate is pending.

The preview gateway **ships with Weft and runs locally by default**. It uses
protected per-review URLs such as `http://<review-id>.localhost:8082`, with no
cloud account, public DNS or external tunnel service. Docker application ports
remain private. Remote installations can use the same gateway through an
administrator-configured private tunnel or HTTPS endpoint.

Review can also record an external CI environment, local branch validation or
explicit acceptance without executing the application, according to the node's
review policy. Approval does not merge a PR or deploy. Native application
supervision and automatic GitHub/Bitbucket/GitLab CI adapters remain future work.
See [setup, grants, HTTP clients and recovery](docs/sandbox-runtime.md#human-review-and-application-previews).

## macOS setup with Colima

If you already have Docker with Compose working, skip to the
[Quickstart](#quickstart-60-seconds). Otherwise, Colima can run the Docker
engine in a Linux VM on your Mac, without Docker Desktop.

With [Homebrew](https://brew.sh/) installed, install Colima and the Docker
client and plugins:

```bash
brew install colima docker docker-compose docker-buildx

mkdir -p ~/.docker/cli-plugins
ln -s "$(brew --prefix)/opt/docker-compose/bin/docker-compose" \
  ~/.docker/cli-plugins/docker-compose
ln -s "$(brew --prefix)/opt/docker-buildx/bin/docker-buildx" \
  ~/.docker/cli-plugins/docker-buildx
```

If a plugin link already exists, check it before replacing it.

On Apple Silicon with macOS 13 or newer, start the VM with Apple's
virtualization framework and VirtioFS file sharing:

```bash
colima start --runtime docker --vm-type vz --mount-type virtiofs \
  --cpus 4 --memory 8 --disk 100
```

The 4 CPUs, 8 GiB of RAM and 100 GiB virtual disk are suggested starting
settings for Weft, not minimum requirements. Adjust them to your machine;
local models need additional resources. On other supported Macs, use
`colima start --runtime docker --cpus 4 --memory 8 --disk 100`.

Verify the engine, plugins and a test container:

```bash
colima status
docker info
docker compose version
docker buildx version
docker run --rm hello-world
```

Initial setup and image downloads need internet access. Use `colima stop`
to stop the VM and `colima start` to restart it with its saved settings.
Then continue below to create Weft's `.env` and start the platform.
See the official [installation](https://colima.run/docs/installation/) and
[configuration](https://colima.run/docs/configuration/) guides for details.

## Quickstart (60 seconds)

After signing in, open **Getting started** in the console for optional, resumable
guidance and a no-model echo example. See the [browser guide](docs/getting-started.md#browser-guide).

Requirements: Docker with Compose v2.

```bash
git clone <this repository> weft && cd weft
for name in WEFT_ADMIN_TOKEN WEFT_RUNTIME_SECRET POSTGRES_PASSWORD GRAFANA_ADMIN_PASSWORD; do
  printf '%s=%s\n' "$name" "$(openssl rand -hex 32)"
done > .env
chmod 600 .env
docker compose up -d --build
```

1. Open the console at <http://localhost:8080> and sign in with the token
   generated for `WEFT_ADMIN_TOKEN` in `.env`. Grafana is at
   <http://localhost:3000>; its admin password is generated into the same file.
2. Two example graphs start automatically: `hello-echo` (zero-cost echo agents)
   and `software-factory` (planner → researcher → developer cluster → reviewer →
   QA → human release gate → integrator).
3. Inject a feature request:

   ```bash
   set -a; . ./.env; set +a
   curl -s -X POST localhost:8080/api/v1/graphs/software-factory/channels/requests/messages \
     -H "Authorization: Bearer $WEFT_ADMIN_TOKEN" -H 'Content-Type: application/json' \
     -d '{"type":"feature.request","payload":{"title":"slugify","description":"Add a slugify(text) helper. It lowercases text and joins words with dashes."}}'
   ```

   or with the CLI shipped in the image:

   ```bash
   set -a; . ./.env; set +a
   docker compose exec -e WEFT_TOKEN="$WEFT_ADMIN_TOKEN" weftd \
     weft inject software-factory requests feature.request '{"title":"slugify","description":"Add slugify(text)."}'
   ```

4. Watch the work move on the canvas, then approve the release in the
   **Approvals** view (or `weft approvals` / `weft approve <id>`).

Model providers are **inference plugins**: every plugin (Ollama, OpenAI,
Anthropic, ...) starts disabled, and an administrator enables one and adds a
provider in **Integrations → Model providers** — for example the Ollama plugin
pointing at `docker compose --profile ollama up -d` or an Ollama installed on
your machine (see
[Getting started](docs/getting-started.md#3-use-a-real-model)). For
development with no model and no Internet, `WEFT_DEV_MOCK=true` adds the
deterministic **mock** provider.

## Architecture

```mermaid
flowchart LR
  subgraph Clients
    UI[Web console<br/>React + TS]
    CLI[weft CLI]
    API_C[Any HTTP client]
  end
  subgraph CP[weftd — control plane, N instances]
    API[REST API + SSE]
    ENG[Engine<br/>reconciler · replica loops · router]
    TG[Tool gateway<br/>default-deny grants]
    LG[LLM gateway<br/>provider registry]
    HITL[Approvals / HITL]
    RT[Runtimes<br/>native · echo · external]
  end
  subgraph Agents[External agent runtimes]
    PY[Python SDK agents<br/>LangGraph · AutoGen · CrewAI · custom]
    ANY[Any language<br/>Runtime Protocol v1]
  end
  Q[(Broker<br/>NATS · Redis · RabbitMQ · Kafka · fs)]
  DB[(Store<br/>PostgreSQL · SQLite · fs)]
  WS[(Workspaces<br/>git + worktrees)]
  M[Model providers<br/>Ollama · vLLM · OpenAI · Anthropic · OpenRouter · TGI]
  OBS[OTel Collector → Prometheus · Tempo · Loki → Grafana]

  UI & CLI & API_C --> API
  API --> ENG
  ENG <--> Q
  ENG <--> DB
  ENG --> RT
  RT -- POST /v1/turn --> PY & ANY
  PY & ANY -- callback: tools · llm · emit · state --> API
  RT --> TG & LG
  TG --> WS
  LG --> M
  ENG --> HITL
  CP -. metrics · traces · logs .-> OBS
```

The engine is a **reconciler**: desired state lives in storage, every instance
takes leases on the replicas it runs, and agent runtime requests reach tools
and models through the tool and LLM gateways under their node's grants.
Commands launched by those tools have the access of their execution mode;
`process` execution does not isolate them from the platform user's files or
OS permissions. See [execution modes and security](#execution-modes-and-security).

## Repository layout

| Path | Contents |
|---|---|
| `server/` | Go module: `cmd/weftd` (control plane), `cmd/weft` (CLI), `internal/*` (domain, engine, graph, queue, storage, llm, tools, runtime, api, hitl, events, observability) |
| `web/` | React + TypeScript console |
| `sdk/python/` | Python Agent SDK (`weft`), framework adapters |
| `agents/` | Example agents: planner, researcher, developer, reviewer, QA |
| `examples/graphs/` | Example graphs |
| `deploy/` | Container config, observability stack, Kubernetes manifests |
| `docs/` | Architecture, operations and reference documentation |

Full tree: [docs/folder-structure.md](docs/folder-structure.md).

## Documentation

- [Getting started](docs/getting-started.md) — install, model providers, first graph, running without Docker
- [Contributing](CONTRIBUTING.md#development-setup) — development environment with asdf or mise, tool versions and checks
- [Architecture](docs/architecture.md) — C4 views, domain model, state machines, sequences, queue and execution model
- [Security model](docs/security.md) — threat model, tool permission model, isolation
- [Sandbox runtime](docs/sandbox-runtime.md) — agent commands in containers on a Docker daemon dedicated to Weft
- [Project environments](docs/environments.md) — the toolchain image agents run in, built from the console
- [Skills and project steering](docs/instructions.md) — opt-in guidance, administration, precedence and execution manifests
- [Configuration reference](docs/configuration.md)
- [Deployment](docs/deployment.md) — single node, HA, Kubernetes, backup, upgrades, air-gapped
- [Production hardening](docs/production-hardening.md)
- [Connecting Atlassian Cloud and Bitbucket Data Center](docs/atlassian-bitbucket-setup.md) — short setup path; Bitbucket Data Center part is proposed
- [Cost control](docs/cost-control.md) — proposed: prices, cost records and monetary budgets
- [Native coding runtime](docs/native-coding-runtime.md) — planned: context management, edit and navigation tools, verification loop and evaluation for the built-in agent
- [Extensibility](docs/extensibility.md) — providers, brokers, stores, runtimes, tools, sandboxes, agents in any language
- [API and protocol contracts](docs/dev/contracts.md), OpenAPI in `api/`
- [Architecture decision records](docs/adr/)
- [Python SDK](sdk/python/README.md), [example agents](agents/README.md), [web console](web/README.md)

## Status and limitations

Weft is young. What works is tested (adapter conformance suites against real
NATS, Redis, RabbitMQ, Kafka and PostgreSQL; engine end-to-end tests including
failover; an end-to-end compose run). Known gaps:

- **No MCP bridge yet.** MCP tools can be integrated through the tool registry.
- **Kubernetes tool sandbox is an extension point.** On Kubernetes, tools run in
  the (restricted) control-plane container; per-replica tool pods would be a
  `SandboxFactory` implementation. Docker isolation needs the Docker socket.
- **No streaming LLM output.** Agent narration appears per model round, not per token.
- **Process sandbox is not a security boundary** against a granted, unrestricted
  shell; use Docker isolation or a sandboxed container runtime for untrusted work.
- Lease expiry uses instance clocks: keep instances NTP-synchronised.
- The filesystem broker and store are meant for development and small single-node installs.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE). Weft is an
an independent open-source project.
