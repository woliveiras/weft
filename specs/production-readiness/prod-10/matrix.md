# PROD-10 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | A representative failure or SLO breach occurs in the production configuration. | Operational monitoring | The existing metrics and applicable rules make it visible and route the actionable alert to the assigned owner with runbook guidance. | independent (backlog and operating contracts, before new implementation) | Rule tests and authorized routing drill against the deployed monitoring shape. | Pending |
| AC-002 | The condition clears or the monitoring path fails. | Recovery | The drill records recovery and any blind spots. Journal capacity and collector alerts retain their existing coverage. | independent (backlog and operating contracts, before new implementation) | Rule firing/clearing tests and operational integration drill. | Pending |
