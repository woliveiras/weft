# INT-008 behavior and oracle matrix

Verification paths identify existing checks unless marked pending. Historical runs are scoped in the evidence record.

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| [AC-001](./spec.md#ac-001--reviewed-isolated-deployment) | Rejected profile, duplicate request or failed startup | Reviewed isolated deployment | A future administrator review binds profile version, image, resources, network and credential references. Durable admission precedes launch, with the deployment separate from task workspace and process scopes. | spec-derived (Managed MCP Hosting, Boundary and Admission and lifecycle) | Pending dedicated-daemon launcher and storage integration | Design only |
| [AC-002](./spec.md#ac-002--health-and-generation-changes) | Unhealthy server, upgrade or interrupted cleanup | Health and generation changes | A future authenticated readiness check precedes use. Update and retirement drain active generations and preserve explicit grants; restart or cleanup reconciles owned resources. | spec-derived (Managed MCP Hosting, Admission and lifecycle and Transport and credentials) | Pending lifecycle, authorization and recovery verification | Design only |
