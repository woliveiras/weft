# 0004 — Agents via an HTTP runtime protocol with a callback gateway

**Status:** Accepted

## Context
Teams use LangGraph, AutoGen, CrewAI or their own code, in Python and other
languages. Embedding frameworks in the control plane would couple releases,
languages and security to them. Agents also must not hold model keys or
unrestricted tool access.

## Decision
A Runtime executes exactly one turn: the engine owns the loop, claiming,
acking, routing and stop policies. External agents implement Runtime Protocol
v1: `POST /v1/turn` (TurnInput → TurnResult), `GET /v1/health`. During a turn
they reach the platform only through `callback.url` with a token scoped to
that execution (tools, LLM, emit, state, output, ask-human). The Python SDK
and framework adapters implement the protocol; any HTTP server can.

## Consequences
- Framework-agnostic and language-agnostic; agents are stateless per turn and
  scale horizontally behind a Service.
- Keys and grants stay in the platform; a compromised agent can do only what
  its node is granted, only during a turn.
- One HTTP round trip per turn plus callbacks; negligible next to model latency.
- Callbacks must reach the instance running the turn (`server.internalUrl` per instance).

## Alternatives
- **In-process plugins (Go plugins, embedded Python):** fragile, couples versions.
- **gRPC:** stronger typing, but harder to implement from minimal environments;
  can be added as a second binding of the same messages.
- **Agents talk to the broker directly:** duplicates delivery logic in every
  SDK and weakens guarantees.
