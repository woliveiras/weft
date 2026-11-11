# PROD-05 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | The assessment reviews the selected production topology. | Scope | The assessment covers sandbox isolation, network egress, approval enforcement and untrusted guidance from repositories, issues and tool outputs. | independent ([2026-11-11 production checklist](../../historical-source-map.md#tasksmd); proposed production qualification) | Review threat model and authorized boundary validation results. | Pending |
| AC-002 | A boundary is unverified or a control fails. | Findings | The report records the result, risk and disposition. Completion requires the agreed remediation or risk acceptance criteria. | independent ([2026-11-11 production checklist](../../historical-source-map.md#tasksmd); proposed production qualification) | Review findings, retest evidence and disposition. | Pending |

Capability criterion owners: [ENV-005 AC-002](../../workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md#ac-002--daemon-and-service-boundary), [ENV-009 AC-002](../../workspaces-and-environments/environment-network-policy-and-egress/spec.md#ac-002--separate-egress-phases), [TASK-006 AC-001](../../product-and-tasks/task-consent-and-gateway-authorization/spec.md#ac-001--reviewed-authorization-modes), [RUN-001 AC-002](../../runtime-and-delivery/skills-and-project-steering/spec.md#ac-002--scoped-loading-and-immutable-audit).
