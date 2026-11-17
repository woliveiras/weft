---
id: PROD-09
title: Timed backup and restore
summary: "Perform and time a restore drill for the production installation."
status: in-progress
scope: [production readiness]
risk: production qualification
risk_domains: [data persistence]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-09/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: ["selected topology for final production restore qualification"]
---

# Timed backup and restore

Perform and time a restore drill for the production installation.

## Behavior and invariants

Local workspace journal recovery and cursor-gated pruning are already covered. The open deliverable is a timed restore of the selected installation, including its journal and cursor.

## Acceptance criteria

- **AC-001 — Complete restore.** A backup is restored into an isolated environment. The inventory covers the selected store, broker, workspace, configuration and required key recovery, plus the complete journal chain required by its cursor and anchor. The drill records elapsed time and recoverable state.
- **AC-002 — Incomplete or incompatible restore.** A timed, incomplete production restore exercises the linked journal failure boundaries and records the runbook outcome and recoverable state.

## Open decisions

- Backup cadence, retention, storage location, recovery point/time objectives, and authorized restore environment.

## History

- 2026-11-11. Local archive recovery and verified pruning were recorded; a timed full-installation restore remained open.

## Capability criteria

- [ENV-003 AC-004](../../workspaces-and-environments/workspace-change-journal-and-attribution/spec.md#ac-004--durable-rotation-and-replay).
- [ENV-003 AC-005](../../workspaces-and-environments/workspace-change-journal-and-attribution/spec.md#ac-005--cursor-gated-retention).

## Reconciliation

- Supporting references: [Backup and restore](../../../docs/deployment.md#backup-and-restore), [backup inventory](../../../docs/operations.md#backup-inventory), and [workspace journal](../../../docs/operations.md#workspace-journal).
- [Local journal rollout and checks](../../workspaces-and-environments/workspace-change-journal-and-attribution/evidence.md).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
