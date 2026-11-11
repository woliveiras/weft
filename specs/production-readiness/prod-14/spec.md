---
id: PROD-14
title: Production integration qualification
summary: "Qualify the real integrations selected for production using authorized test accounts."
status: planned
scope: [production readiness]
risk: production qualification
risk_domains: [security, external access]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-14/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: ["authorized real accounts and test resources"]
---

# Production integration qualification

Qualify the real integrations selected for production using authorized test accounts.

## Behavior and invariants

Local-first and fixture coverage come first. Provider selection or fixture success does not qualify live accounts. Bitbucket Data Center may require a license and Bedrock qualification depends on adoption.

## Acceptance criteria

- **AC-001 — Live qualification.** An integration in scope has approved test resources. The real service passes the required connection, identity and permitted operation checks, with renew/revoke or mutation cases as applicable to that integration.
- **AC-002 — Denial or missing authorization.** A credential lacks access or required test approval is pending. Denials respect the remote and Weft boundaries. The integration remains unqualified until required live evidence exists.

## Open decisions

- Final integration list and environments, test accounts, scopes and allowed mutations, and vendor or corporate approval.

## History

- 2026-10-30. Live accounts were moved to the external phase. The 2026-11-11 checklist retained real-account acceptance for selected integrations.

## Capability criteria

- [INT-002 AC-003](../../providers-and-integrations/model-provider-adapters-and-conformance/spec.md#ac-003--qualification-scope).
- [INT-007 AC-003](../../providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md#ac-003--per-vendor-acceptance-boundary).
- [RUN-005 AC-003](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md#ac-003--adapter-qualification).
- [RUN-006 AC-003](../../runtime-and-delivery/issue-references-and-polling-triggers/spec.md#ac-003--gateway-write-back).

## Reconciliation

- Supporting references: [MCP integration guide](../../../docs/mcp.md), [Atlassian and Bitbucket Data Center setup](../../../docs/atlassian-bitbucket-setup.md), and [provider conformance](../../../docs/extensibility.md#provider-conformance-suite).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
