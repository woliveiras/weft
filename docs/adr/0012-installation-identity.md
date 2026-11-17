# 0012 — Separate installation identity from integration credentials

**Status:** Accepted

## Context

An installation has operator identities and separately managed credentials
for model, MCP and SCM connections. Task authorization also records a reviewed
scope. These controls have distinct issuers, owners and expiry conditions.

## Decision

Bootstrap and provision installation identities through installation
authentication. Use integration OAuth and credential entry only for the
reviewed connection. Store connection credentials on the server and resolve
them at the authorized gateway boundary. Bind task consent to reviewed task,
workflow, policy and actor revisions. It operates within
[ADR 0005](0005-default-deny-tool-gateway.md) grants.

## Consequences

- Revoking an integration credential and changing an installation identity
  are independent actions.
- Connecting a service supplies no task consent or tool grant. A reviewed task
  has no general access to its connection credentials.
- [Production sign-in](../../specs/production-readiness/prod-01/spec.md)
  and [task consent](../../specs/product-and-tasks/task-consent-and-gateway-authorization/spec.md)
  retain their own qualification and audit conditions.

## Alternatives

- **Treat integration OAuth as installation sign-in:** couples vendor scopes
  and lifecycle to operator roles.
- **Pass integration credentials to agents:** bypasses reviewed gateway scope
  and obscures authorization and audit.
