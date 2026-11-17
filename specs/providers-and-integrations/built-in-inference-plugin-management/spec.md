---
id: INT-001
title: "Built-in Inference Plugin Management"
summary: "Manage built-in plugin availability and provider instances through the installation console and API."
status: implemented
scope: ["built in inference plugin management"]
navigation: ["specs/README.md","specs/providers-and-integrations/built-in-inference-plugin-management/matrix.md"]
documentation: ["docs/configuration.md","docs/extensibility.md","docs/provider-langgraph-validation.md","docs/adr/0008-offline-mock-provider.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Built-in Inference Plugin Management

Manage built-in plugin availability and provider instances through the installation console and API.

## Behavior and invariants

- Installed, active and configured are distinct states. New installations start with built-in real plugins disabled, and the console uses manifests to render fields and capabilities.

## Acceptance criteria

### AC-001 — Manifest and activation

Installed, active and configured are distinct states. New installations start with built-in real plugins disabled, and the console uses manifests to render fields and capabilities.

### AC-002 — Managed configuration and credential isolation

Admin changes use the managed API. The console keeps secrets write-only and providers call upstream through the gateway. Test results and upstream failures are sanitized.

### AC-003 — Unavailable provider

Disabled or missing ready providers block inference-dependent admission and explain the required configuration. The development mock is exposed under its development configuration.

## Explicit exclusions

- Provider-specific wire conformance is INT-002. Historical removal of legacy provider settings was a POC decision; production compatibility belongs to PROD-02.

## Open decisions

- The initial local-first proposal called deactivation of an in-use type a recommendation pending confirmation. The later PLUG-02/04 record describes a reviewed workflow; use that scope and retain the earlier proposal as history.

## History

- 2026-10-18. Browser configuration and managed LangGraph gateway acceptance used synthetic credentials. Provider management was isolated in its own disposable browser backend after registry reload affected other fixture scenarios.
- 2026-10-30. The local-first plan recorded the three plugin states, UI/API-only configuration and development-only mock. PLUG-01–05 are reported complete by 2026-11-11.

## Reconciliation

- [configuration.md](../../../docs/configuration.md), [extensibility.md](../../../docs/extensibility.md), [provider-langgraph-validation.md](../../../docs/provider-langgraph-validation.md), [adr/0008-offline-mock-provider.md](../../../docs/adr/0008-offline-mock-provider.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
