# Behavior and oracle matrix

All verification below is planned. `spec-derived` refers to the maintainer's
2026-11-20 portability and custom-workflow decisions as recorded in this spec;
detailed recovery/merge policies are proposed criteria pending specification
review, not evidence of an approved implementation.

| Criterion | Scenario | Invariant and observable oracle | Provenance | Planned verification | Evidence |
| --- | --- | --- | --- | --- | --- |
| AC-001 | Jira + GitHub/Bitbucket; GitHub Issues + Bitbucket | Correct independent connections and issue links survive execution snapshot; no unsupported closing claim | spec-derived | Configuration unit tests; task/SCM/tracker integration | Pending |
| AC-001 | Same issue key/repository on two instances; missing/wrong-scope connection | No identity collision, cross-instance write or dispatch on invalid scope | spec-derived | Identity/validation units; isolated two-instance integration | Pending |
| AC-002 | Four product/edition profiles; missing adapter or operation | Edition-specific requirements and actual availability; no Cloud/DC credential fallback | spec-derived | Adapter conformance units; API/console/YAML integration; TLS/denial fixtures | Pending |
| AC-003 | `Review` versus `In Review`, localization, duplicate names, one column with multiple statuses | Explicit scoped IDs and current-issue discovery; no name guessing | spec-derived | Mapping units; Jira workflow discovery/transition integration | Pending |
| AC-003 | Missing mapping; different issue-type workflow; renamed status with stable ID | No transition when unmapped; validate actual workflow; rename preserves mapping | spec-derived | Mapping units; multiple-workflow integration | Pending |
| AC-004 | Deleted transition, required fields, permission loss, already at target | Visible sync failure or idempotent no-op; task/PR evidence retained; no duplicate work | spec-derived; proposed recovery policy | Recovery units; persistence/remote transition integration | Pending |
| AC-004 | Timeout after remote transition; workflow/config change before retry | Reconcile current outcome and scope before replay; no false success | spec-derived; proposed recovery policy | Retry units; fault-injected tracker integration | Pending |
| AC-005 | Findings/no findings; summary publication failure; denied/unsupported tool | Summary precedes merge; failures do not become a clean verdict or implicit grant | spec-derived; proposed portable review contract | Review decision units; gateway/SCM integration; browser failure states | Pending |
| AC-005 | New head, failing/unknown checks, remote rules, async/uncertain merge | Merge only exact reviewed revision under required rules; unavailable concurrency guarantee disables auto-merge | spec-derived; proposed merge contract | Revision/conformance units; concurrent Git/PR integration | Pending |
| AC-006 | Duplicate PR-opened event; same PR number on different instances | One review per scoped revision; no duplicate summary/merge; correct tracker synchronization | spec-derived | Event identity units; event/review/SCM/tracker persistence integration | Pending |
| AC-007 | Existing GitHub workflow/task, explicit migration, failed save/apply | Existing behavior retained; draft recoverable; grants/history/secrets unchanged | spec-derived | Compatibility units; console/API/YAML integration and migration preview | Pending |
| AC-008 | 401/403/404/429/5xx, pagination, unavailable service | Errors retain context, enforce bounds, prevent cross-scope calls and permit safe recovery | spec-derived; external provider contracts to verify per edition | Adapter units; HTTP/Git integration | Pending |
| AC-008 | Fixture versus authorized real-account rehearsal | Separately record product/edition/combination and exact stage verified | spec-derived; PROD-14 | Rehearsal per edition and selected pair under authorized resources | Pending |
