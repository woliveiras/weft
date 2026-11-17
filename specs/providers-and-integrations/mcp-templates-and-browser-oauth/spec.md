---
id: INT-006
title: "MCP Templates and Browser OAuth"
summary: "Open versioned connection templates and complete reviewed browser authorization within the supported client setup."
status: implemented
scope: ["mcp templates and browser oauth"]
navigation: ["specs/README.md","specs/providers-and-integrations/mcp-templates-and-browser-oauth/matrix.md"]
documentation: ["docs/mcp.md","docs/mcp-templates.md","docs/mcp-integrations.md"]
dependencies: ["INT-005"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# MCP Templates and Browser OAuth

Open versioned connection templates and complete reviewed browser authorization within the supported client setup.

## Behavior and invariants

- A pinned browser link survives sign-in and opens an explicit preview. Invalid links create nothing. Upgrades show a version and configuration diff without silently expanding scope.

## Acceptance criteria

### AC-001 — Reviewed template links and upgrades

A pinned browser link survives sign-in and opens an explicit preview. Invalid links create nothing. Upgrades show a version and configuration diff without silently expanding scope.

### AC-002 — OAuth transaction and credential lifecycle

A reviewed browser flow uses session-bound state and PKCE. Durable pending transactions survive supported multi-instance use; refresh, reauthorization and revocation are isolated per connection and keep tokens server-side.

### AC-003 — Capability-gated client setup

DCR/CIMD follow reviewed issuer/resource metadata and supported policy. Unsupported or uncertain setup offers a documented manual route and avoids duplicate client creation.

## Explicit exclusions

- Actual vendor registration, corporate approval, consent and lifecycle qualification are INT-007 and PROD-14.

## Open decisions

- The older Release 1 introduction listed shared pending OAuth storage as missing. The 2026-10-18 checkpoint records delivered support; provider-specific live interoperability remains open.

## History

- 2026-10-17. Documented metadata, browser links and generic pre-registration informed the initial design.
- 2026-10-18. Shared transactions, reviewed template upgrades and hosted-CIMD fixture qualification were recorded.

## Reconciliation

- [mcp.md](../../../docs/mcp.md), [mcp-templates.md](../../../docs/mcp-templates.md), [mcp-integrations.md](../../../docs/mcp-integrations.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
