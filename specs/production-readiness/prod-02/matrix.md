# PROD-02 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | A supported previous installation is upgraded. | Versioned migrations | Versioned migrations apply in order, preserve persisted state under the supported contract, and rerunning startup does not repeat applied changes. | independent (backlog and operating contracts, before new implementation) | Unit migration state tests; populated release-to-release store integration. | Pending |
| AC-002 | Two controllers start against PostgreSQL during the upgrade. | Concurrent startup | The advisory lock serializes migration, and mixed-version workers obey the feature's documented fencing rules. | independent (backlog and operating contracts, before new implementation) | Unit fence checks; multi-controller PostgreSQL integration. | Pending |
| AC-003 | A migration fails or startup is interrupted. | Failure and recovery | The failure is visible. Recovery follows the documented backup, compatibility and fencing procedure before affected workers resume. | independent (backlog and operating contracts, before new implementation) | Unit failure handling; disposable interrupted-upgrade and recovery drill. | Pending |
