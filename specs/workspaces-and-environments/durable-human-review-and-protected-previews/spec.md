---
id: ENV-004
title: "Durable Human Review and Protected Previews"
summary: "Bind review to a durable candidate and expose protected Docker previews through the gateway."
status: implemented
scope: ["durable human review and protected previews"]
navigation: ["specs/README.md","specs/workspaces-and-environments/durable-human-review-and-protected-previews/matrix.md"]
documentation: ["docs/sandbox-runtime.md","docs/sandbox-acceptance.md","docs/runtime-protocol.md"]
dependencies: ["ENV-005","TASK-006"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Durable Human Review and Protected Previews

Bind review to a durable candidate and expose protected Docker previews through the gateway.

## Behavior and invariants

- A request persists candidate identity and yields the execution. Authorized decisions apply once to the reviewed candidate, resume or request changes as appropriate, and reject stale or expired decisions.

## Acceptance criteria

### AC-001 — Candidate-bound review gate

A request persists candidate identity and yields the execution. Authorized decisions apply once to the reviewed candidate, resume or request changes as appropriate, and reject stale or expired decisions.

### AC-002 — Private preview lifecycle

A reviewed Docker preview exposes HTTP and WebSocket access through the protected gateway. Credentials bind the session; revoke, restart, expiry and shutdown release owned resources and prevent access to stale generations.

### AC-003 — Review evidence and access

The interface shows readiness, logs, expiry and candidate evidence. Browser and explicit HTTP access retain application authentication without publishing a sandbox port.

## Explicit exclusions

- Native process supervision and automatic authenticated PR/CI review adapters are deferred. Manual external/local review and separate SCM delivery remain supported.

## Open decisions

- Interactive Postman and public DNS/TLS deployment acceptance remain unverified. The historical remote-topology exercise used private forwarding and HTTP/cURL.

## History

- 2026-10-18 Release 1 checkpoint. The implementation plan and task checklist record durable gates, protected Docker previews and real-daemon/browser coverage. The individual implementation date is unstated.

## Reconciliation

- [sandbox-runtime.md](../../../docs/sandbox-runtime.md), [sandbox-acceptance.md](../../../docs/sandbox-acceptance.md), [runtime-protocol.md](../../../docs/runtime-protocol.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
