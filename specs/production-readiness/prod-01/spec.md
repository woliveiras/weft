---
id: PROD-01
title: Production sign-in
summary: "Replace the production human sign-in path with OIDC/SSO and role mapping."
status: draft
scope: [production readiness]
risk: production blocker
risk_domains: [security, authorization]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-01/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: []
---

# Production sign-in

Replace the production human sign-in path with OIDC/SSO and role mapping.

## Behavior and invariants

AUTH-OIDC was deferred for local-first delivery. It remains the first production blocker. The hardening guide describes scoped automation tokens and reserves an admin token for break-glass access.

## Acceptance criteria

- **AC-001 — Human sign-in.** A person signs in and out via the configured IdP. OIDC authorization-code flow with PKCE establishes a browser session, supports refresh and logout, and maps IdP groups to existing viewer, operator and admin roles.
- **AC-002 — Rejected login.** Issuer, audience or callback validation fails, or a person lacks a mapped role. The rejected request gains no privileged session. Existing role checks still govern each action.
- **AC-003 — Automation.** A scoped automation token calls the API. Existing API role enforcement limits the token to its automation scope. Production human sign-in uses the IdP.

## Open decisions

- OIDC provider and tenant; group claim and mapping; session lifetimes; break-glass procedure reconciling the hardening guide's admin token with automation-only static credentials.

## Reconciliation

- Governing inputs: `identity and access; release 1 AUTH-OIDC`. [Production hardening](../../../docs/production-hardening.md#identity-and-access) and [deployment](../../../docs/deployment.md). [Initial identity and credentials](../../../docs/operations.md#initial-identity-and-credentials).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
