# Documentation

Start with [Getting started](getting-started.md) for Docker Compose or the
[native bundle](standalone.md) for installation without Docker. Developers
should use [Contributing](../CONTRIBUTING.md) for toolchains and checks.

## Find a guide

| Task | Documentation |
| --- | --- |
| Navigate the console and inspect runs | [Console navigation](interface-navigation.md) |
| Create tasks, register projects and follow issue-triggered work | [Task workflows](task-workflows.md), [task authorization](task-authorization.md) |
| Configure the installation | [Configuration](configuration.md) |
| Choose a topology and operate it | [Deployment](deployment.md), [operations and recovery](operations.md), [Kubernetes manifests](../deploy/kubernetes/README.md) |
| Understand execution security | [Security model](security.md), [isolation modes](execution-model.md#8-isolation-modes), [production hardening](production-hardening.md) |
| Run commands and review applications in containers | [Sandbox runtime](sandbox-runtime.md), [sandbox acceptance evidence](sandbox-acceptance.md) |
| Prepare a project's toolchain and network policy | [Environments](environments.md), [mise example](../examples/projects/mise-node/README.md) |
| Grant tools and guide agents | [Tool permissions](tool-permissions.md), [skills and steering](instructions.md), [delegation](delegation.md) |
| Configure models and budgets | [Model setup](getting-started.md#3-use-a-real-model), [cost control](cost-control.md), [LangGraph qualification](provider-langgraph-validation.md) |
| Connect MCP services | [Connections and permissions](mcp.md), [integration contracts](mcp-integrations.md), [template candidates](mcp-templates.md), [Atlassian and Bitbucket setup](atlassian-bitbucket-setup.md) |
| Understand native coding and evaluations | [Native runtime](native-coding-runtime.md), [evaluation specification and evidence](../specs/runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md) |
| Extend Weft | [Extension guide](extensibility.md), [Python SDK](../sdk/python/README.md), [example agents](../agents/README.md), [web console](../web/README.md) |
| Work on visual identity | [Brand assets](brand/README.md), [brand guidelines](brand/brand-guidelines.md) |

## Architecture and contracts

| Topic | Owning reference |
| --- | --- |
| Component responsibilities and trade-offs | [Architecture](architecture.md), [repository map](folder-structure.md) |
| System and interaction diagrams | [C4 views](diagrams/c4.md), [sequences](diagrams/sequences.md) |
| Entities, execution and state | [Domain model](domain-model.md), [execution model](execution-model.md), [state machines](state-machines.md) |
| Queues and persistence | [Queue architecture](queue-architecture.md), [storage schema](database-schema.md) |
| Public HTTP API | [OpenAPI](../api/openapi.yaml), [HTTP usage and conventions](api.md) |
| Agent runtime callbacks | [Runtime Protocol v1](runtime-protocol.md), [turn schema](../schemas/turn.schema.json) |
| Graph and message formats | [Graph schema](../schemas/graph.schema.json), [message schema](../schemas/message.schema.json) |
| Events and tracing | [Event catalogue](events.md), [event schema](../schemas/event.schema.json) |
| Cross-component design conventions | [Product contracts](product-contracts.md) |

## Status, decisions and evidence

The [specification index](../specs/README.md) owns capability status, acceptance
criteria and pending qualification. Each specification links to its verification
matrix and recorded evidence. [ADRs](adr/README.md) explain architectural decisions;
the [historical source map](../specs/historical-source-map.md) locates earlier plans.
Historical test results apply to the recorded revision and environment.

[Managed MCP hosting](mcp-hosting.md) is a planned design, and
[out-of-process provider plugins](provider-protocol.md) are deferred. Their
design documents do not describe available installation features.

## Maintaining these documents

- Keep the root README as an introduction and link to this index for navigation.
- Update a topic's owning guide, then link to it from complementary documents.
  Keep a short local explanation where readers need context or a safety boundary.
- Link to source declarations instead of copying Go interfaces and function
  signatures. Keep runnable examples only when they teach a distinct workflow.
- Keep acceptance intent and historical evidence in `specs/`; link shared test
  runs to one evidence record instead of duplicating their result tables.
- Distinguish implemented behavior from proposals. Check status against the
  specification index and behavior against the relevant code and contracts.
- When moving or removing content, update incoming links and check file paths
  and section anchors, including links from specifications.
