---
id: PROD-06
title: Managed secrets and key rotation
summary: "Use managed Vault or KMS in place of the local installation key file and qualify rotation."
status: draft
scope: [production readiness]
risk: production qualification
risk_domains: [security, secrets, data persistence]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-06/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: []
---

# Managed secrets and key rotation

Use managed Vault or KMS in place of the local installation key file and qualify rotation.

## Behavior and invariants

The current local key file is the starting point. The model gateway remains the holder of model credentials. Rotation must account for the current runbook's key-rotation limits and recovery of stored encrypted data.

## Acceptance criteria

- **AC-001 — Managed key.** The production installation starts with the chosen managed key service. Weft can use encrypted credentials with access limited to authorized server components. The local key file is no longer required in production.
- **AC-002 — Rotation and recovery.** The key is rotated or the key service becomes unavailable. The documented procedure is tested against existing encrypted data and states how to recover. Failures are visible without exposing key material.

## Open decisions

- Vault versus KMS, key and identity ownership, rotation cadence, availability and recovery design for existing encrypted data.

## Reconciliation

- Supporting references: [Production secrets](../../../docs/production-hardening.md#secrets) and [encryption-key rotation](../../../docs/operations.md#encryption-key-rotation).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
