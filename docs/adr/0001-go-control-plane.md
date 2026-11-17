# 0001 — Go for the control plane

**Status:** Accepted

## Context
The control plane runs long-lived concurrent loops (hundreds of replicas),
holds broker subscriptions, streams events, and must ship as a small, static,
cross-platform artifact that runs on Linux, macOS and Windows and in
minimal containers. The ecosystem for NATS, Kafka, PostgreSQL, OpenTelemetry
and Prometheus must be first-class.

## Decision
Write the control plane (`weftd`) and CLI (`weft`) in Go with
`CGO_ENABLED=0` (pure-Go SQLite via `modernc.org/sqlite`). Agents are not
written in Go by necessity: the Runtime Protocol (ADR 0004) lets them be
written in any language; Python is the first-class SDK.

## Consequences
- One static binary per platform; distroless/alpine images; no runtime to install.
- Goroutine-per-replica keeps the engine simple and cheap.
- AI framework ecosystems (LangGraph, CrewAI, AutoGen) are Python; they
  integrate out of process. The native Go agent covers the framework-free case.

## Alternatives
- **Python control plane:** closest to AI frameworks, but GIL/async
  complexity for many loops, heavier packaging, weaker static deployment.
- **TypeScript:** good UI sharing, weaker for the
  infrastructure clients and static distribution.
- **Rust:** excellent, but slower iteration and a smaller contributor pool.
