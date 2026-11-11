---
id: INT-008
title: "Managed MCP Hosting"
summary: "Preserve the reviewed design for isolated, installation-managed MCP server generations."
status: planned
scope: ["managed mcp hosting"]
navigation: ["specs/README.md","specs/providers-and-integrations/managed-mcp-hosting/matrix.md"]
documentation: ["docs/mcp-hosting.md","docs/mcp.md","docs/sandbox-runtime.md"]
dependencies: ["INT-005","ENV-005","ENV-006"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Managed MCP Hosting

Preserve the reviewed design for isolated, installation-managed MCP server generations.

## Behavior and invariants

- A future administrator review binds profile version, image, resources, network and credential references. Durable admission precedes launch, with the deployment separate from task workspace and process scopes.

## Acceptance criteria

### AC-001 — Reviewed isolated deployment

A future administrator review binds profile version, image, resources, network and credential references. Durable admission precedes launch, with the deployment separate from task workspace and process scopes.

### AC-002 — Health and generation changes

A future authenticated readiness check precedes use. Update and retirement drain active generations and preserve explicit grants; restart or cleanup reconciles owned resources.

## Explicit exclusions

- Playwright, MLflow and Qdrant are candidate profiles. The design grants no authority to install or launch them.

## Open decisions

- Select a profile and authorize the real-daemon qualification before implementation or vendor deployment.

## History

- 2026-10-18. The [Evolution Plan](../../historical-source-map.md#planmd) records the managed hosting design only, with isolation, lifecycle, authenticated transport and candidate validation boundaries.

## Reconciliation

- [mcp-hosting.md](../../../docs/mcp-hosting.md), [mcp.md](../../../docs/mcp.md), [sandbox-runtime.md](../../../docs/sandbox-runtime.md).
- [Behavior and oracle matrix](./matrix.md).
