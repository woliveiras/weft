# PROD-01 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | A person signs in and out via the configured IdP. | Human sign-in | OIDC authorization-code flow with PKCE establishes a browser session, supports refresh and logout, and maps IdP groups to existing viewer, operator and admin roles. | independent (backlog and operating contracts, before new implementation) | Protocol and role-mapping unit tests; browser and local IdP integration. | Pending |
| AC-002 | Issuer, audience or callback validation fails, or a person lacks a mapped role. | Rejected login | The rejected request gains no privileged session. Existing role checks still govern each action. | independent (backlog and operating contracts, before new implementation) | Unit validation; local IdP/browser denial and expired-session tests. | Pending |
| AC-003 | A scoped automation token calls the API. | Automation | Existing API role enforcement limits the token to its automation scope. Production human sign-in uses the IdP. | independent (backlog and operating contracts, before new implementation) | Unit role checks; real API and local IdP integration. | Pending |

Capability criterion owners: [OPS-001 AC-001](../../installation-and-operations/compose-installation-and-operations/spec.md#ac-001--separate-bootstrap-and-application-setup).
