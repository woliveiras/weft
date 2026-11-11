# PROD-13 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | The selected model runs the agreed suite for the chosen repetitions. | Measured result | Results identify model, workload and run conditions, record outcomes and usage, and show whether the agreed solve rate was met. | independent (backlog and operating contracts, before new implementation) | Repeat authorized native evaluation and verify scores from independent task checks. | Pending |
| AC-002 | A run fails or the score falls below the approved minimum. | Below bar or unavailable | Failures and incomplete trials stay visible. A fixture result or exploratory free-tier result does not qualify a production model. | independent (backlog and operating contracts, before new implementation) | Validate aggregation against fixtures and review repeated evaluation records. | Pending |
