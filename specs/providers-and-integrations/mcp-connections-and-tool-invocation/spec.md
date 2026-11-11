---
id: INT-005
title: "MCP Connections and Tool Invocation"
summary: "Register MCP endpoints and enforce exact, scoped tool authorization through the gateway."
status: implemented
scope: ["mcp connections and tool invocation"]
navigation: ["specs/README.md","specs/providers-and-integrations/mcp-connections-and-tool-invocation/matrix.md"]
documentation: ["docs/mcp.md","docs/tool-permissions.md","docs/standalone.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# MCP Connections and Tool Invocation

Register MCP endpoints and enforce exact, scoped tool authorization through the gateway.

## Behavior and invariants

- Admins review revisioned HTTP or explicitly configured native stdio connections. Unsupported transport, unavailable endpoints and stale catalogues report actionable failures. A disabled connection can be reviewed for removal without reusing its identity.

## Acceptance criteria

### AC-001 — Connection lifecycle and transport

Admins review revisioned HTTP or explicitly configured native stdio connections. Unsupported transport, unavailable endpoints and stale catalogues report actionable failures. A disabled connection can be reviewed for removal without reusing its identity.

### AC-002 — Exact grants and argument scope

Every tool call requires an exact current grant and applicable approval. Connection restrictions validate approved argument values and combinations before credential access or dispatch. Stale policy or catalogue changes require revalidation.

### AC-003 — Quotas and safe history

Connection quotas are shared across callers. Bounded history records approval, result and error with redacted output; authentication, scope and quota failures are distinguishable without replaying mutations.

## Explicit exclusions

- Vendor live assurance is INT-007 and PROD-14. Managed server launching is INT-008.

## Open decisions

- The local-first record reports a failure at [the MCP authentication-failure scenario](../../../web/e2e/mcp.spec.ts#L18). Reconfirm under PROD-11 and retain the intended safe failure behavior.

## History

- 2026-10-17 to 2026-10-19. The delivery increments added exact gateway grants, bounded lifecycle and history, explicit stateless transport, argument rules and reviewed native stdio.
- 2026-11-11. Local-first verification notes retain the main-branch browser failure.

## Reconciliation

- [mcp.md](../../../docs/mcp.md), [tool-permissions.md](../../../docs/tool-permissions.md), [standalone.md](../../../docs/standalone.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
