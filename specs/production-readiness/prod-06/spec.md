---
id: PROD-06
title: Managed secrets and key rotation
summary: "Use managed Vault or KMS in place of the local installation key file and qualify rotation."
status: planned
scope: [production readiness]
risk: production qualification
risk_domains: [security, secrets, data persistence]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-06/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: [INT-001, INT-005, INT-006, RUN-005]
---

# Managed secrets and key rotation

Use managed Vault or KMS in place of the local installation key file and qualify rotation.

## Behavior and invariants

The current local key file is the starting point. The model gateway remains the holder of model credentials. Rotation must account for the current runbook's key-rotation limits and recovery of stored encrypted data.

## Acceptance criteria

- **AC-001 — Managed key.** The production installation starts with the chosen managed key service. Weft can use encrypted credentials with access limited to authorized server components. The local key file is no longer required in production.
- **AC-002 — Rotation and recovery.** Qualify rotation and managed-key outage recovery for model credentials, MCP connection tokens and secret headers, OAuth access and refresh tokens and client secrets, local MCP process environment secrets, and SCM tokens. Verify each connection remains usable or follows its documented recovery path. Failures are visible without exposing key material.

## Open decisions

- Vault versus KMS, key and identity ownership, rotation cadence, availability and recovery design for existing encrypted data.

## History

- 2026-11-11. The production checklist proposed managed key material and tested rotation.

## Capability criteria

- [INT-001 AC-002](../../providers-and-integrations/built-in-inference-plugin-management/spec.md#ac-002--managed-configuration-and-credential-isolation).
- [ENV-001 AC-002](../../workspaces-and-environments/repository-snapshot-imports/spec.md#ac-002--credential-boundary).
- [INT-005 AC-001](../../providers-and-integrations/mcp-connections-and-tool-invocation/spec.md#ac-001--connection-lifecycle-and-transport).
- [INT-006 AC-002](../../providers-and-integrations/mcp-templates-and-browser-oauth/spec.md#ac-002--oauth-transaction-and-credential-lifecycle).
- [RUN-005 AC-001](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md#ac-001--bounded-push).

## Reconciliation

- Supporting references: [Production secrets](../../../docs/production-hardening.md#secrets) and [encryption-key rotation](../../../docs/operations.md#encryption-key-rotation).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
