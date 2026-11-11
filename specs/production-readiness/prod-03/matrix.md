# PROD-03 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | An installation admits a production model. | Qualified inference | The model uses a paid service covered by approved data-protection terms. A free tier that trains on inputs cannot qualify. | independent (backlog and operating contracts, before new implementation) | Review terms and approved configuration; fixture and authorized provider conformance. | Pending |
| AC-002 | A qualified model processes requests with known usage. | Pricing | The catalogue holds verified prices for that model and the gateway uses them to record spend. | independent (backlog and operating contracts, before new implementation) | Unit cost calculation; engine/store/provider-fixture integration. | Pending |
| AC-003 | A project or user reaches its configured hard budget. | Budget exhaustion | The existing reservation and pause controls prevent further admission beyond the hard threshold and surface the reason. | independent (backlog and operating contracts, before new implementation) | Unit budget boundaries; engine/store concurrent admission and approved-resume integration. | Pending |
