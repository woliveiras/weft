# Failed task retry behavior/oracle matrix

Written before implementation from the approved recovery proposal on 2026-11-25.
All expectations are spec-derived from TASK-011. Test authors exposed to code
must disclose implementation awareness separately in evidence.

| ID | Criterion / scenario | Observable oracle | Verification |
| --- | --- | --- | --- |
| R01 | AC-001 failed manual snapshot | Exact input and provenance; fresh draft has no inherited lifecycle/consent | Unit snapshot; API/storage integration |
| R02 | AC-001 editing/resolving child | Provenance persists; stale pin not silently advanced | Manager unit; save/resolve integration |
| R03 | AC-002 replay/concurrency/restart | Same child ID; only one retained child; later child edits/status preserved | Real storage concurrent integration |
| R04 | AC-002 invalid/auth/stale/trigger | 400/403/409/404 as applicable; no new draft or runtime side effect | Boundary unit; role/API integration |
| R05 | AC-002 deleted/uncertain/failing store/audit | No resurrection or false success; acknowledged retry recovers committed child | Fault unit; durable storage integration |
| R06 | AC-003 old state and new admission | Source unchanged; no graph/workspace/auth/model/SCM call from retry; normal review required | API/storage integration; browser journey |
| R07 | AC-004 fresh attempt entry | Visible action/disclosure, pending disabled, error announced, original link after reload | Helper/component unit; browser/API integration |
| R08 | AC-004 replay with edited/started child | Navigate to actual returned attempt without recreating/resetting it | Component integration |
| R09 | AC-005 approval status | Exact expired status gives specific guidance; nonexpiry retains appropriate diagnostics | Presentation unit; tracking integration |
| R10 | AC-004/005 roles/keyboard/narrow layout | Reader lacks creation; semantic labels/status; keyboard route works, controls wrap | Component and browser verification |
| R11 | AC-003 retry identity at the graph DNS boundary | Existing retry task IDs and replay remain stable; review, environment preparation, start, task authorization and evidence ownership accept bounded execution IDs; mismatched owners fail closed | Identity unit; task/API/SCM integration; explicit-start browser regression |
