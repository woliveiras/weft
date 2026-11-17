# 0008 — Deterministic mock provider as the default model

**Status:** Accepted

## Context
`docker compose up` must produce a fully functional platform with no
Internet access, no GPU and no API keys, and CI must exercise agent graphs
deterministically.

## Decision
Ship a `mock` provider (default `llm.default`) that answers deterministically:
verbatim scripted responses via request metadata, an `emit` tool call for
native agents, a small JSON object in JSON mode, otherwise an echo. Example
Python agents fall back to deterministic heuristics when model output lacks
the fields they need, so the software factory runs end to end offline
(including real files, commits and test runs). Real providers are one
environment variable away (`WEFT_DEFAULT_PROVIDER`).

## Consequences
- The whole machinery — queues, clusters, approvals, recovery, observability —
  is demonstrable and testable for free; the `echo` runtime complements it.
- Output quality in the default setup is intentionally trivial; documentation
  makes switching providers prominent.

## Alternatives
- **Bundle a small local model by default:** large download, slow on CPU,
  hardware-dependent.
- **Require a provider at install:** breaks the offline, one-command goal.

## Superseded in part (2026-10-30)

Providers became inference plugins that start disabled and are configured in
the console. The mock is no longer the default of every installation: it is a
development plugin offered only with `llm.devMock` / `WEFT_DEV_MOCK=true`.
The offline, one-command goal remains available for development through that
flag; a real installation enables a plugin (for example Ollama) instead.
