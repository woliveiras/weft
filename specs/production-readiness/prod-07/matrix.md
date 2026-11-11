# PROD-07 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | A new workflow tries a write-capable tool, push or pull request with the applicable grant. | New workflow | A new production workflow defaults to human approval for granted write tools, push and PR. Verify the default with the linked gateway and SCM criteria. | independent (backlog and operating contracts, before new implementation) | Unit defaults and policy tests; workflow creation and gateway integration. | Pending |
| AC-002 | A required decision is rejected or absent. | Missing or denied consent | Production acceptance includes a threat model and demonstrates the new default for rejection and absence of consent. | independent (backlog and operating contracts, before new implementation) | Unit decision tests; integration with isolated filesystem and SCM fixtures. | Pending |

Capability criterion owners: [TASK-006 AC-001](../../product-and-tasks/task-consent-and-gateway-authorization/spec.md#ac-001--reviewed-authorization-modes), [RUN-005 AC-001](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md#ac-001--bounded-push), [RUN-005 AC-002](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md#ac-002--idempotent-pull-request-and-hand-off).
