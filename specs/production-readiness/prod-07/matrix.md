# PROD-07 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | A new workflow tries a write-capable tool, push or pull request with the applicable grant. | New workflow | The gateway requires a human decision before the side effect. Grant enforcement still applies. | independent (backlog and operating contracts, before new implementation) | Unit defaults and policy tests; workflow creation and gateway integration. | Pending |
| AC-002 | A required decision is rejected or absent. | Missing or denied consent | The protected operation has no side effect. The threat model explains enforcement and trust boundaries. | independent (backlog and operating contracts, before new implementation) | Unit decision tests; integration with isolated filesystem and SCM fixtures. | Pending |
