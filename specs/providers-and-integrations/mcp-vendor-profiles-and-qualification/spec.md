---
id: INT-007
title: "MCP Vendor Profiles and Qualification"
summary: "Preserve provider-specific connection and resource boundaries and qualify each selected profile separately."
status: in-progress
scope: ["mcp vendor profiles and qualification"]
navigation: ["specs/README.md","specs/providers-and-integrations/mcp-vendor-profiles-and-qualification/matrix.md"]
documentation: ["docs/mcp-integrations.md","docs/mcp.md","docs/mcp-templates.md"]
dependencies: ["INT-005","INT-006"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# MCP Vendor Profiles and Qualification

Preserve provider-specific connection and resource boundaries and qualify each selected profile separately.

## Behavior and invariants

- Profiles distinguish service endpoints, credential modes and scopes. Atlassian flat operation discovery and reviewed site/product filters preserve exact authorization. GitLab instance and GitHub compatibility rules apply to their documented hosts. For the selected Atlassian service with canonical MCP endpoint and matching advertised Atlassian issuer/authorization/token paths, a server-side Weft DCR client requests an advertised confidential method (`client_secret_basic` preferred, then `client_secret_post`) when available; public `none` remains available when confidential methods are not advertised. The setup preview identifies the selected method. Returned method must exactly match the request; confidential credentials are required, bounded and encrypted. Callback, scope, expiry and PKCE validation remain enforced; CIMD and existing/manual registrations are unchanged.

## Acceptance criteria

### AC-001 — Vendor configuration and access profiles

Profiles distinguish service endpoints, credential modes and scopes. Atlassian flat operation discovery and reviewed site/product filters preserve exact authorization. GitLab instance and GitHub compatibility rules apply to their documented hosts. For the selected Atlassian service with canonical MCP endpoint and matching advertised Atlassian issuer/authorization/token paths, a server-side Weft DCR client requests an advertised confidential method (`client_secret_basic` preferred, then `client_secret_post`) when available; public `none` remains available when confidential methods are not advertised. The setup preview identifies the selected method. Returned method must exactly match the request; confidential credentials are required, bounded and encrypted. Callback, scope, expiry and PKCE validation remain enforced; CIMD and existing/manual registrations are unchanged.

### AC-002 — Typed connection fields

Applicable dbt, LangSmith, Sentry and Databricks profiles keep sensitive configuration server-side, bound metadata and distinguish the reviewed service or workspace.

### AC-003 — Per-vendor acceptance boundary

Record whether a result covers metadata, public discovery, public tool read, authenticated read, renewal/revocation or approved mutation. Catalogues and scope evidence are tied to the selected profile and environment.

## Explicit exclusions

- Production integrated acceptance is PROD-14. Public discovery and fixture coverage do not qualify paid or authenticated calls.

## Open decisions

- [INT-009](../portable-issue-and-source-control/spec.md) owns the planned
  cross-provider task/review contract and separate Jira/Bitbucket Cloud/Data
  Center adapter scope. An MCP vendor label or discovery result does not qualify
  native SCM delivery, Data Center support or portable automatic merge.
- GitHub private/organization/SSO access, expiry, revocation and approved writes remain open. Atlassian DCR had an uncertain outcome; live DCR/CIMD, site/IP policy and separate Jira, Confluence and Bitbucket Cloud acceptance remain pending. GitLab qualification depends on the selected instance and advertised capabilities.
- Complete Context7 query-docs/authenticated and private-library qualification, first-wave W&B checks, and typed-header/scope qualification for applicable vendors. Warehouse integration still needs a hard cost and data boundary; BigQuery stateless discovery and Snowflake documentation are limited evidence.

## History

- 2026-10-17. Installed GitHub manual OAuth and read-only calls were recorded.
- 2026-10-18. Flat Atlassian discovery and operation boundaries, typed profiles and public opt-in discovery/read checkpoints expanded the vendor evidence.

- 2026-11-23. A live HTTP 201 failed only client_secret validation for a requested public client. Maintainer authorized correcting interoperability without relaxing response validation. Prefer advertised confidential DCR for the server-side Atlassian client; the rejected secret value was not retained or inspected. This is a profile-method selection decision, not proof of the provider value or completed live qualification.

## Reconciliation

- [mcp-integrations.md](../../../docs/mcp-integrations.md), [mcp.md](../../../docs/mcp.md), [mcp-templates.md](../../../docs/mcp-templates.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
