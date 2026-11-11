---
id: PROD-14
title: Production integration qualification
summary: "Qualify the real integrations selected for production using authorized test accounts."
status: draft
scope: [production readiness]
risk: production qualification
risk_domains: [security, external access]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-14/matrix.md]
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

## Reconciliation

- Governing inputs: `original TASKS.md vendor checklist; local-first external phase`. [Production hardening](../../../docs/production-hardening.md#identity-and-access) and [deployment](../../../docs/deployment.md). [Local-first external phase](../../../wip/OLLAMA-PLAN.md#fica-para-a-fase-de-apis-e-contas-externas).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
