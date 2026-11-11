---
id: PROD-09
title: Timed backup and restore
summary: "Perform and time a restore drill for the production installation."
status: draft
scope: [production readiness]
risk: production qualification
risk_domains: [data persistence]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-09/matrix.md]
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
- **AC-002 — Incomplete or incompatible restore.** A needed segment is missing or a restored cursor is behind the prune anchor. Recovery exposes the gap or mismatch and follows the runbook without presenting the incomplete replay as successful.

## Open decisions

- Backup cadence, retention, storage location, recovery point/time objectives, and authorized restore environment.

## Reconciliation

- Governing inputs: `deployment backup and restore; operations inventory, drill and workspace journal`. [Production hardening](../../../docs/production-hardening.md#data-and-durability) and [deployment](../../../docs/deployment.md). [Backup inventory and restore drill](../../../docs/operations.md#backup-inventory).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
