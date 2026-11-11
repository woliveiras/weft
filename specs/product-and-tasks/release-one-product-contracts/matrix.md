# TASK-001 behavior and oracle matrix

Verification paths identify existing checks unless marked pending. Historical runs are scoped in the evidence record.

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| [AC-001](./spec.md#ac-001--scoped-operation) | Unauthorized, invalid or stale request | Scoped operation | A request receives an outcome for its authorized scope. Validation, unavailability, approval waits and version conflicts surface as distinguishable states. | spec-derived (Product Contracts §§2, 5) | [server/internal/api/api_test.go](../../../server/internal/api/api_test.go), [server/internal/api/graph_edit_test.go](../../../server/internal/api/graph_edit_test.go) | Pending fresh verification |
| [AC-002](./spec.md#ac-002--audit-identity) | A scoped action emits an event | Audit identity | An event retains the applicable graph, agent, replica, task or execution identity so the console can correlate it with the recorded operation. | spec-derived (Product Contracts §5 and Events) | [server/internal/api/activity_test.go](../../../server/internal/api/activity_test.go) | Pending fresh verification |
| [AC-003](./spec.md#ac-003--supported-product-surface) | A client inspects the supported surface | Supported product surface | Users edit and operate Weft graphs directly. Legacy converter routes and UI entry points are outside the delivered product. | spec-derived (Weft Evolution Plan, Remove Out-of-Scope Product Functionality) | [server/internal/api/api_test.go](../../../server/internal/api/api_test.go), Pending explicit route inventory comparison | Historical cleanup checklist; targeted removal replay pending |
