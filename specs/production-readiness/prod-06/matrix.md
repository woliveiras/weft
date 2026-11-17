# PROD-06 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | The production installation starts with the chosen managed key service. | Managed key | Weft can use encrypted credentials with access limited to authorized server components. The local key file is no longer required in production. | independent ([2026-11-11 production checklist](../../historical-source-map.md#tasksmd); proposed production qualification) | Unit key-service contract tests; authorized store and key-service integration. | Pending |
| AC-002 | The key is rotated or the key service becomes unavailable. | Rotation and recovery | Verify rotation and outage recovery for model and MCP credentials, OAuth access/refresh/client secrets, local MCP process environment secrets, and SCM tokens without exposing key material. | spec-derived (maintainer's 2026-11-11 rotation scope decision, before managed-key implementation) | Authorized rotation and outage drill using distinct synthetic credentials for each store, then verify the corresponding connection paths | Pending |

Capability criterion owners: [INT-001 AC-002](../../providers-and-integrations/built-in-inference-plugin-management/spec.md#ac-002--managed-configuration-and-credential-isolation), [ENV-001 AC-002](../../workspaces-and-environments/repository-snapshot-imports/spec.md#ac-002--credential-boundary).

Additional capability owners: [INT-005 AC-001](../../providers-and-integrations/mcp-connections-and-tool-invocation/spec.md#ac-001--connection-lifecycle-and-transport), [INT-006 AC-002](../../providers-and-integrations/mcp-templates-and-browser-oauth/spec.md#ac-002--oauth-transaction-and-credential-lifecycle), [RUN-005 AC-001](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md#ac-001--bounded-push).
