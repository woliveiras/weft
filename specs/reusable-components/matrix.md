# Release 2 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | Save and share an agent or subgraph | Installation permissions apply | Authorized component operations round-trip versioned definitions; denied operations preserve the existing state | independent ([Release 2 plan, before implementation](../historical-source-map.md#planmd)) | Unit permission checks and real storage/API integration | Deferred |
| AC-002 | Insert valid, invalid and incompatible definitions | Insertion validates before graph mutation | Valid parameters and defaults resolve; invalid or incompatible input surfaces an error and preserves the draft | independent ([Release 2 plan, before implementation](../historical-source-map.md#planmd)) | Unit validation and visual editor integration | Deferred |
| AC-003 | Review and apply an update | Local configuration survives | Current and available versions and a diff appear before update; local settings survive the round trip | independent ([Release 2 plan, before implementation](../historical-source-map.md#planmd)) | Browser journey over real graph and component storage | Deferred |
