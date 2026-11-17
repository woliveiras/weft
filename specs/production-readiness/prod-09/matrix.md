# PROD-09 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | A backup is restored into an isolated environment. | Complete restore | The inventory covers the selected store, broker, workspace, configuration and required key recovery, plus the complete journal chain required by its cursor and anchor. The drill records elapsed time and recoverable state. | independent ([2026-11-11 production checklist](../../historical-source-map.md#tasksmd); proposed production qualification) | Timed documented restore drill with journal replay and reconciliation. | Pending |
| AC-002 | A needed segment is missing or a restored cursor is behind the prune anchor. | Incomplete or incompatible restore | A timed, incomplete production restore exercises the linked journal failure boundaries and records the runbook outcome and recoverable state. | independent ([2026-11-11 production checklist](../../historical-source-map.md#tasksmd); proposed production qualification) | Unit cursor and chain validation; real storage integration and failed-restore drill. | Pending |

Capability criterion owners: [ENV-003 AC-004](../../workspaces-and-environments/workspace-change-journal-and-attribution/spec.md#ac-004--durable-rotation-and-replay), [ENV-003 AC-005](../../workspaces-and-environments/workspace-change-journal-and-attribution/spec.md#ac-005--cursor-gated-retention).
