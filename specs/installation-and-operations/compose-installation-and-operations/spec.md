---
id: OPS-001
title: "Compose Installation and Operations"
summary: "Operate the local Compose installation through documented bootstrap, health and maintenance boundaries."
status: in-progress
scope: ["compose installation and operations"]
navigation: ["specs/README.md","specs/installation-and-operations/compose-installation-and-operations/matrix.md"]
documentation: ["docs/deployment.md","docs/operations.md","docs/getting-started.md","docs/configuration.md"]
dependencies: ["ENV-005"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Compose Installation and Operations

Operate the local Compose installation through documented bootstrap, health and maintenance boundaries.

## Behavior and invariants

- Bootstrap provisions the initial installation identity and private data configuration without overwriting an existing profile. After bootstrap, routine model, connection and workflow operation uses the console.

## Acceptance criteria

### AC-001 — Separate bootstrap and application setup

Bootstrap provisions the initial installation identity and private data configuration without overwriting an existing profile. After bootstrap, routine model, connection and workflow operation uses the console.

### AC-002 — Health and dependency visibility

Health distinguishes liveness and required store/queue readiness from optional dependency state. Public readiness reports bounded states; a transient failure and recovery remain visible without raw upstream errors.

### AC-003 — Preserve operator intent

A stale observation from a worker cannot overwrite a newer run/stop command or graph version. Persistent configuration and recoverable state survive documented service restarts.

## Explicit exclusions

- Production topology, upgrade, restore and alert drills are PROD-02 and PROD-08–10. Authentication changes require their own approval.

## Open decisions

- Verify documented clean-install reachability for providers, MCP, store and runtime using the intended exposure policy. Image acquisition and final human walkthrough remain gated.

## History

- 2026-10-18. The release checkpoint documented Compose volume/network/health/restart behavior, bounded dependency reporting and the recovery runbook.
- 2026-10-19. The delivery plan retained a clean-install exercise after remaining Release 1 work.

## Reconciliation

- [deployment.md](../../../docs/deployment.md), [operations.md](../../../docs/operations.md), [getting-started.md](../../../docs/getting-started.md), [configuration.md](../../../docs/configuration.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
