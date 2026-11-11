---
id: PROD-08
title: Production topology and workspace storage
summary: "Choose and qualify the production topology and dedicated workspace filesystem."
status: draft
scope: [production readiness]
risk: production qualification
risk_domains: [isolation, data persistence]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-08/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: ["production topology selection"]
---

# Production topology and workspace storage

Choose and qualify the production topology and dedicated workspace filesystem.

## Behavior and invariants

A Docker named volume on a shared Linux filesystem does not give fanotify a dedicated filesystem mark. The production choice must preserve the existing mount paths, workspace ownership and Git metadata. The topology decision and data move require separate authorization.

## Acceptance criteria

- **AC-001 — Topology qualification.** The selected topology runs a representative workload. The approved deployment uses managed or replicated PostgreSQL and NATS as selected and satisfies its documented availability and isolation boundaries.
- **AC-002 — Workspace migration.** Existing workspaces move to the dedicated fanotify-capable Linux filesystem. Weftd and the dedicated daemon see the distinct filesystem, with ownership and Git metadata preserved. Journal and daemon layers and PostgreSQL remain on other filesystems.
- **AC-003 — Journal coverage.** Unrelated filesystem writes occur alongside transient workspace operations. The drill verifies transient operations and PID evidence where supported, and compares unresolved paths and overflows before and after migration.

## Open decisions

- Single node versus Kubernetes HA; PostgreSQL and NATS ownership and replication; workspace filesystem/host, maintenance window and migration plan.

## Reconciliation

- Governing inputs: `deployment topologies; hardening dedicated Linux workspace filesystem`. [Production hardening](../../../docs/production-hardening.md#data-and-durability) and [deployment](../../../docs/deployment.md). [Workspace journal](../../../docs/operations.md#workspace-journal).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
