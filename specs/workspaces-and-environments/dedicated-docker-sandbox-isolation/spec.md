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

In the supported Compose topology, mutual TLS rejects daemon clients without a
valid client certificate. The daemon firewall keeps sandbox traffic away from
private infrastructure. Its entrypoint exits if firewall installation fails.

### AC-003 — Optional dependency readiness

The console and readiness report daemon availability without globally blocking non-sandbox work. Docker work that requires the unavailable daemon remains blocked.

### AC-004 — Default container hardening

Default Docker execution drops all capabilities, sets no-new-privileges,
uses network `none`, and mounts only the replica workspace. Reviewed
environments can add declared cache mounts and explicitly selected networking.
Agent containers receive no Docker socket.

## Explicit exclusions

- Rootless Docker-in-Docker remains deferred. Native host execution is an explicit separate choice. Final cross-scope acceptance belongs to PROD-04.
- The Compose daemon is privileged. Graphs share one daemon, images and
  caches. Kubernetes mode runs tools in weftd without a per-replica boundary.
- External runtimes started via `runtime.image` cannot use the dedicated daemon
  because they need to call weftd back. Other topologies need separate qualification.

## Open decisions

- Clean-install coverage for the selected production topology remains with OPS-001 and PROD-04.

## History

- 2026-10-18 Release 1 checkpoint. The sandbox checklist records the privileged dedicated daemon, TLS, shared workspace and real network/isolation checks.
- 2026-11-11. Cached local rollout recreated the dedicated daemon with its existing volumes and verified readiness.

## Reconciliation

- [sandbox-runtime.md](../../../docs/sandbox-runtime.md), [sandbox-acceptance.md](../../../docs/sandbox-acceptance.md), [adr/0009-dedicated-sandbox-daemon.md](../../../docs/adr/0009-dedicated-sandbox-daemon.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
