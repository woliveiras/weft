# PROD-14 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | An integration in scope has approved test resources. | Live qualification | The real service passes the required connection, identity and permitted operation checks, with renew/revoke or mutation cases as applicable to that integration. | independent (backlog and operating contracts, before new implementation) | Authorized live qualification and targeted gateway integration. | Pending |
| AC-002 | A credential lacks access or required test approval is pending. | Denial or missing authorization | Denials respect the remote and Weft boundaries. The integration remains unqualified until required live evidence exists. | independent (backlog and operating contracts, before new implementation) | Fixture denial tests and authorized real-account scope/permission denial. | Pending |

Capability criterion owners: [INT-002 AC-003](../../providers-and-integrations/model-provider-adapters-and-conformance/spec.md#ac-003--qualification-scope), [INT-007 AC-003](../../providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md#ac-003--per-vendor-acceptance-boundary), [RUN-005 AC-003](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md#ac-003--adapter-qualification), [RUN-006 AC-003](../../runtime-and-delivery/issue-references-and-polling-triggers/spec.md#ac-003--gateway-write-back).
