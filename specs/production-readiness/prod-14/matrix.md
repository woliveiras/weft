# PROD-14 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | An integration in scope has approved test resources. | Live qualification | The real service passes the required connection, identity and permitted operation checks, with renew/revoke or mutation cases as applicable to that integration. | independent (backlog and operating contracts, before new implementation) | Authorized live qualification and targeted gateway integration. | Pending |
| AC-002 | A credential lacks access or required test approval is pending. | Denial or missing authorization | Denials respect the remote and Weft boundaries. The integration remains unqualified until required live evidence exists. | independent (backlog and operating contracts, before new implementation) | Fixture denial tests and authorized real-account scope/permission denial. | Pending |
