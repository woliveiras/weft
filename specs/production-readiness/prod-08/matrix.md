# PROD-08 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | The selected topology runs a representative workload. | Topology qualification | The approved deployment uses managed or replicated PostgreSQL and NATS as selected and satisfies its documented availability and isolation boundaries. | independent (backlog and operating contracts, before new implementation) | Configuration checks and staged load, restart and failover checks appropriate to the topology. | Pending |
| AC-002 | Existing workspaces move to the dedicated fanotify-capable Linux filesystem. | Workspace migration | Weftd and the dedicated daemon see the distinct filesystem, with ownership and Git metadata preserved. Journal and daemon layers and PostgreSQL remain on other filesystems. | independent (backlog and operating contracts, before new implementation) | Mount verification and disposable migration with real Linux/daemon integration. | Pending |
| AC-003 | Unrelated filesystem writes occur alongside transient workspace operations. | Journal coverage | A migration drill evaluates the linked ENV-003 kernel coverage on the selected topology and compares unresolved paths and overflows before and after migration. | independent (backlog and operating contracts, before new implementation) | Controlled workload on the selected topology with journal and metric inspection. | Pending |

Capability criterion owners: [ENV-003 AC-003](../../workspaces-and-environments/workspace-change-journal-and-attribution/spec.md#ac-003--kernel-observations-and-gaps).
