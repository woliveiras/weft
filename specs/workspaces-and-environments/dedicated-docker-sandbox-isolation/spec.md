---
id: ENV-005
title: "Dedicated Docker Sandbox Isolation"
summary: "Run Docker-isolated agents on the dedicated daemon and keep sandbox resources and access separate from the host."
status: implemented
scope: ["dedicated docker sandbox isolation"]
navigation: ["specs/README.md","specs/workspaces-and-environments/dedicated-docker-sandbox-isolation/matrix.md"]
documentation: ["docs/sandbox-runtime.md","docs/sandbox-acceptance.md","docs/adr/0009-dedicated-sandbox-daemon.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Dedicated Docker Sandbox Isolation

Run Docker-isolated agents on the dedicated daemon and keep sandbox resources and access separate from the host.

## Behavior and invariants

- Docker isolation uses the configured dedicated daemon and workspace mount. Agent containers have no Docker access. Reset affects resources of the dedicated daemon as documented.

## Acceptance criteria

### AC-001 — Dedicated daemon and workspace

Docker isolation uses the configured dedicated daemon and workspace mount. Agent containers have no Docker access. Reset affects resources of the dedicated daemon as documented.

### AC-002 — Daemon and service boundary

Mutual TLS restricts daemon clients. Network policy keeps sandbox traffic away from private infrastructure and setup fails closed if the firewall cannot be installed.

### AC-003 — Optional dependency readiness

The console and readiness report daemon availability without globally blocking non-sandbox work. Docker work that requires the unavailable daemon remains blocked.

## Explicit exclusions

- Rootless Docker-in-Docker remains deferred. Native host execution is an explicit separate choice. Final cross-scope acceptance belongs to PROD-04.

## Open decisions

- Clean-install coverage for the selected production topology remains with OPS-001 and PROD-04.

## History

- 2026-10-18 Release 1 checkpoint. The sandbox checklist records the privileged dedicated daemon, TLS, shared workspace and real network/isolation checks.
- 2026-11-11. Cached local rollout recreated the dedicated daemon with its existing volumes and verified readiness.

## Reconciliation

- [sandbox-runtime.md](../../../docs/sandbox-runtime.md), [sandbox-acceptance.md](../../../docs/sandbox-acceptance.md), [adr/0009-dedicated-sandbox-daemon.md](../../../docs/adr/0009-dedicated-sandbox-daemon.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
