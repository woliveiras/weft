---
id: ENV-007
title: "Environment Discovery and Reviewed Definitions"
summary: "Discover repository environment candidates and require review before adopting a definition or an agent proposal."
status: implemented
scope: ["environment discovery and reviewed definitions"]
navigation: ["specs/README.md","specs/workspaces-and-environments/environment-discovery-and-reviewed-definitions/matrix.md"]
documentation: ["docs/environments.md","docs/product-contracts.md","docs/sandbox-acceptance.md"]
dependencies: ["ENV-005","ENV-009"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Environment Discovery and Reviewed Definitions

Discover repository environment candidates and require review before adopting a definition or an agent proposal.

## Behavior and invariants

- An explicit override remains effective. Otherwise discovery evaluates Weft YAML, supported devcontainer, Dockerfile and Compose in order. An invalid higher-priority source reports failure and ambiguous candidates require selection.

## Acceptance criteria

### AC-001 — Deterministic discovery

An explicit override remains effective. Otherwise discovery evaluates Weft YAML, supported devcontainer, Dockerfile and Compose in order. An invalid higher-priority source reports failure and ambiguous candidates require selection.

### AC-002 — Reviewed source and context

The accepted definition preserves source and fingerprints. Build contexts stay bounded and confined; supported stage and Compose service choices are explicit. Changed fingerprints invalidate previous review.

### AC-003 — Agent proposal gate

If no repository source is available, a granted eligible agent may submit an own-graph proposal. Administrative review and successful preparation precede continued command execution; stale, rejected or expired approvals do not start the build.

## Explicit exclusions

- Compose override merging, unsupported Compose fields and broader Dockerfile/devcontainer features remain excluded. In-memory proposal calls are not restored after restart.

## Open decisions

- Record separately any request to expand the supported format subset or registry policy.

## History

- 2026-10-18 Release 1 checkpoint. The plan and sandbox acceptance record preserve reviewed discovery sources, bounded contexts and proposal-driven preparation.

## Reconciliation

- [environments.md](../../../docs/environments.md), [product-contracts.md](../../../docs/product-contracts.md), [sandbox-acceptance.md](../../../docs/sandbox-acceptance.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
