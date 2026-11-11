# PROD-09 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | A backup is restored into an isolated environment. | Complete restore | The inventory covers the selected store, broker, workspace, configuration and required key recovery, plus the complete journal chain required by its cursor and anchor. The drill records elapsed time and recoverable state. | independent (backlog and operating contracts, before new implementation) | Timed documented restore drill with journal replay and reconciliation. | Pending |
| AC-002 | A needed segment is missing or a restored cursor is behind the prune anchor. | Incomplete or incompatible restore | Recovery exposes the gap or mismatch and follows the runbook without presenting the incomplete replay as successful. | independent (backlog and operating contracts, before new implementation) | Unit cursor and chain validation; real storage integration and failed-restore drill. | Pending |
