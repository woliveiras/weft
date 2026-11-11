# PROD-13 behavior and oracle matrix

| Criterion | Scenario | Invariant | Observable oracle | Oracle provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- | --- |
| AC-001 | The selected model runs the agreed suite for the chosen repetitions. | Measured result | An approved model meets the chosen production threshold over the agreed workload and repetitions, using the linked RUN-004 verdict and reporting criteria. | independent (backlog and operating contracts, before new implementation) | Repeat authorized native evaluation and verify scores from independent task checks. | Pending |
| AC-002 | A run fails or the score falls below the approved minimum. | Below bar or unavailable | A below-threshold, incomplete or unavailable evaluation leaves the production model unqualified and records the failed qualification decision. | independent (backlog and operating contracts, before new implementation) | Validate aggregation against fixtures and review repeated evaluation records. | Pending |

Capability criterion owners: [RUN-004 AC-001](../../runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md#ac-001--task-verdict), [RUN-004 AC-002](../../runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md#ac-002--repeatable-benchmark-report).
