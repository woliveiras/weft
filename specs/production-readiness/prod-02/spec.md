---
id: PROD-02
title: Versioned production upgrades
summary: "Qualify upgrades between production releases and close the POC compatibility exception."
status: planned
scope: [production readiness]
risk: production blocker
risk_domains: [data persistence, migrations]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-02/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: []
---

# Versioned production upgrades

Qualify upgrades between production releases and close the POC compatibility exception.

## Behavior and invariants

The existing embedded SQL migrations and PostgreSQL advisory lock are the baseline. The earlier provider-plugin POC exception allowed breakage without migration. This initiative ends that exception for supported production upgrades.

## Acceptance criteria

- **AC-001 — Versioned migrations.** A supported previous installation is upgraded. Versioned migrations apply in order, preserve persisted state under the supported contract, and rerunning startup does not repeat applied changes.
- **AC-002 — Concurrent startup.** Two controllers start against PostgreSQL during the upgrade. The advisory lock serializes migration, and mixed-version workers obey the feature's documented fencing rules.
- **AC-003 — Failure and recovery.** A migration fails or startup is interrupted. The failure is visible. Recovery follows the documented backup, compatibility and fencing procedure before affected workers resume.

## Open decisions

- Supported upgrade starting versions and POC data cutover; compatibility window across releases; rollback procedure for incompatible schema changes.

## History

- The 2026-10-30 POC provider cutover allowed breaking settings without migration. The 2026-11-11 production checklist requires supported release-to-release upgrades.

## Capability criteria

- [OPS-002 AC-002](../../installation-and-operations/standalone-installation-and-recovery/spec.md#ac-002--offline-backup-and-versioned-recovery).

## Reconciliation

- Supporting references: [Deployment upgrades](../../../docs/deployment.md#upgrades-and-schema-migrations) and [upgrade and rollback](../../../docs/operations.md#upgrade-and-rollback).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
