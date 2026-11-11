# Historical source coverage

This map routes the three historical planning files as they stood at
`7cb67a3` on 2026-11-11. Line ranges are inclusive and refer to that snapshot.
Every source line belongs to a range, including blank lines, section
introductions and nested headings. The destinations are the owning specs,
their linked evidence, or the ADRs.

Repeated checklist text, link-only pointers and duplicate run narratives are
consolidated into the same destinations. Routine restart syntax, installation
record IDs, repeated before/after counts and setup incidental details are
omitted where the operating guide or the retained evidence states the
durable result. Historical branch metadata is explicitly discarded below.

## TASKS.md

| Source lines | Section or evidence block | Destination |
| --- | --- | --- |
| 1–6 | Weft Implementation Tasks | [TASK-001](./product-and-tasks/release-one-product-contracts/spec.md), [PROD-04](./production-readiness/prod-04/spec.md) |
| 7–36 | Delivery Order and Final Acceptance (updated 2026-10-19) | [PROD-04](./production-readiness/prod-04/spec.md), [ADR 10](../docs/adr/0010-local-first-delivery.md), [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 37–38 | Release 1 — Build and Operate Graphs Through the Interface | [TASK-001](./product-and-tasks/release-one-product-contracts/spec.md) |
| 39–49 | 0. Prepare Product Contracts and Acceptance Criteria | [TASK-001](./product-and-tasks/release-one-product-contracts/spec.md) |
| 50–59 | 1. Remove Out-of-Scope Product Functionality | [TASK-001](./product-and-tasks/release-one-product-contracts/spec.md) |
| 60–86 | 2. Turn the Canvas into a Graph Editor | [TASK-003](./product-and-tasks/graph-editing-and-versioned-saves/spec.md) |
| 87–90 | 3. Provide Containerized Project Environments | [ENV-005](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md), [ENV-006](./workspaces-and-environments/sandbox-capacity-and-idle-resume/spec.md), [ENV-007](./workspaces-and-environments/environment-discovery-and-reviewed-definitions/spec.md), [ENV-008](./workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md), [ENV-009](./workspaces-and-environments/environment-network-policy-and-egress/spec.md) |
| 91–100 | Isolated Container Runtime | [ENV-005](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md), [ADR 9](../docs/adr/0009-dedicated-sandbox-daemon.md) |
| 101–110 | Environment Model and Lifecycle | [ENV-008](./workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md) |
| 111–130 | Definition Sources | [ENV-007](./workspaces-and-environments/environment-discovery-and-reviewed-definitions/spec.md) |
| 131–145 | Reuse Across Tasks and Resource Budgets | [ENV-006](./workspaces-and-environments/sandbox-capacity-and-idle-resume/spec.md), [ENV-008](./workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md) |
| 146–156 | Network Policy | [ENV-009](./workspaces-and-environments/environment-network-policy-and-egress/spec.md) |
| 157–164 | Interface | [ENV-007](./workspaces-and-environments/environment-discovery-and-reviewed-definitions/spec.md), [ENV-008](./workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md) |
| 165–181 | Tests and Documentation | [ENV-005](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md), [ENV-006](./workspaces-and-environments/sandbox-capacity-and-idle-resume/spec.md), [ENV-007](./workspaces-and-environments/environment-discovery-and-reviewed-definitions/spec.md), [ENV-008](./workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md), [ENV-009](./workspaces-and-environments/environment-network-policy-and-egress/spec.md) |
| 182–183 | 4. Create the File and Interaction Panel | [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md) |
| 184–196 | Workspace API and Security | [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md), [ENV-003](./workspaces-and-environments/workspace-change-journal-and-attribution/spec.md) |
| 197–213 | Operational Interface | [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 214–253 | 4a. Durable Human Review and Application Previews | [ENV-004](./workspaces-and-environments/durable-human-review-and-protected-previews/spec.md) |
| 254–280 | 5. Configure Model Providers Through the Interface | [INT-001](./providers-and-integrations/built-in-inference-plugin-management/spec.md), [INT-002](./providers-and-integrations/model-provider-adapters-and-conformance/spec.md) |
| 281–306 | 6. Integrate User-Configurable MCPs | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 307–317 | Connectivity and Integrations | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 318–328 | Credentials, Authorization, and Auditing | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md) |
| 329–339 | Interface | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 340–366 | Vendor Delivery Checklist | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 367–383 | Automatic OAuth Client Setup — DCR and CIMD | [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 384–401 | Browser-Link Connection and OAuth Lifecycle | [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 402–411 | Additional Template Backlog | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 412–437 | 7. Add Skills and Steering | [RUN-001](./runtime-and-delivery/skills-and-project-steering/spec.md) |
| 438–665 | 8. Allow Dynamic Delegation to Subagents | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 666–677 | Verified Foundation | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 678–692 | Model, Policies, and Execution | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md), [INT-004](./providers-and-integrations/monetary-usage-and-budget-controls/spec.md) |
| 693–703 | Interface and Tests | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 704–716 | 9. Make the Docker Installation Operable Through the Interface | [OPS-001](./installation-and-operations/compose-installation-and-operations/spec.md), [PROD-04](./production-readiness/prod-04/spec.md), [ADR 12](../docs/adr/0012-installation-identity.md) |
| 717–749 | 10. First-use guidance — global onboarding withdrawn | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md) |
| 750–789 | 10a. Phase A — Task-oriented Navigation and Execution Flow (implemented 2026-10-19) | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md), [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md), [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md), [TASK-008](./product-and-tasks/registered-projects-and-task-worktrees/spec.md), [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md), [RUN-006](./runtime-and-delivery/issue-references-and-polling-triggers/spec.md) |
| 790–808 | 10a.1 Prompt-first tasks and authorization modes — implemented with local qualification (2026-10-19) | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md), [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md), [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md), [ENV-001](./workspaces-and-environments/repository-snapshot-imports/spec.md), [PROD-04](./production-readiness/prod-04/spec.md) |
| 809–837 | 10b. Phase B — Standalone Weft on the User's Machine (after Phase A) | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md), [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md), [TASK-008](./product-and-tasks/registered-projects-and-task-worktrees/spec.md), [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md), [ADR 11](../docs/adr/0011-execution-boundaries.md) |
| 838–860 | 10c. Phase C — From Issue to Pull Request Under the Gateway (after Phase B) | [RUN-005](./runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md), [RUN-006](./runtime-and-delivery/issue-references-and-polling-triggers/spec.md), [PROD-14](./production-readiness/prod-14/spec.md), [ADR 10](../docs/adr/0010-local-first-delivery.md) |
| 861–874 | Inference provider plugins (added 2026-10-30, before PROV-04) | [INT-001](./providers-and-integrations/built-in-inference-plugin-management/spec.md) |
| 875–886 | Model providers | [INT-002](./providers-and-integrations/model-provider-adapters-and-conformance/spec.md), [INT-003](./providers-and-integrations/out-of-process-provider-plugins/spec.md) |
| 887–899 | Push and pull requests through the gateway | [RUN-005](./runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md) |
| 900–914 | Issue triggers by label (Jira and GitHub) | [RUN-006](./runtime-and-delivery/issue-references-and-polling-triggers/spec.md) |
| 915–925 | Monetary cost control (design) | [INT-004](./providers-and-integrations/monetary-usage-and-budget-controls/spec.md) |
| 926–947 | Native coding runtime (design) | [RUN-003](./runtime-and-delivery/native-coding-runtime/spec.md), [RUN-004](./runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md), [PROD-13](./production-readiness/prod-13/spec.md) |
| 948–956 | Activity view (added 2026-11-11) | [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md) |
| 957–962 | Deferred | [INT-003](./providers-and-integrations/out-of-process-provider-plugins/spec.md), [PROD-01](./production-readiness/prod-01/spec.md) |
| 963–978 | 11. Integrated Validation of Release 1 | [PROD-04](./production-readiness/prod-04/spec.md), [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md), [ENV-005](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md), [RUN-001](./runtime-and-delivery/skills-and-project-steering/spec.md) |
| 979–1000 | Production readiness (added 2026-11-11) | [PROD-01](./production-readiness/prod-01/spec.md), [PROD-02](./production-readiness/prod-02/spec.md), [PROD-03](./production-readiness/prod-03/spec.md), [PROD-04](./production-readiness/prod-04/spec.md), [PROD-05](./production-readiness/prod-05/spec.md), [PROD-06](./production-readiness/prod-06/spec.md), [PROD-07](./production-readiness/prod-07/spec.md), [PROD-08](./production-readiness/prod-08/spec.md), [PROD-09](./production-readiness/prod-09/spec.md), [PROD-10](./production-readiness/prod-10/spec.md), [PROD-11](./production-readiness/prod-11/spec.md), [PROD-12](./production-readiness/prod-12/spec.md), [PROD-13](./production-readiness/prod-13/spec.md), [PROD-14](./production-readiness/prod-14/spec.md), [PROD-15](./production-readiness/prod-15/spec.md) |
| 1001–1029 | Production topology, journal recovery and monitoring | [PROD-08](./production-readiness/prod-08/spec.md), [PROD-09](./production-readiness/prod-09/spec.md), [PROD-10](./production-readiness/prod-10/spec.md), [ENV-003](./workspaces-and-environments/workspace-change-journal-and-attribution/spec.md) |
| 1030–1035 | Quality and delivery | [PROD-11](./production-readiness/prod-11/spec.md), [PROD-12](./production-readiness/prod-12/spec.md), [PROD-13](./production-readiness/prod-13/spec.md) |
| 1036–1040 | Integrations and organization | [PROD-14](./production-readiness/prod-14/spec.md), [PROD-15](./production-readiness/prod-15/spec.md) |
| 1041–1057 | Release 2 — Reusable Components | [RELEASE-2](./reusable-components/spec.md) |
| 1058–1069 | UX Reorganization — Approved Direction | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [TASK-003](./product-and-tasks/graph-editing-and-versioned-saves/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [PROD-04](./production-readiness/prod-04/spec.md) |
| 1070–1089 | Installed navigation and visual baselines | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md) |
| 1090–1099 | Historical developer usability round and initial feedback | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [PROD-04](./production-readiness/prod-04/spec.md) |
| 1100–1180 | UX-01–UX-05 developer observations and fixes | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 1181–1194 | UX-01–UX-05 suite and intermittent sandbox check | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [PROD-11](./production-readiness/prod-11/spec.md) |
| 1195–1225 | UX-01–UX-05 installed activation | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 1226–1262 | Historical onboarding implementation and withdrawal | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md) |
| 1263–1281 | Historical onboarding activation | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md) |
| 1282–1289 | Release 1 validation correction: paused input admission | [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 1290–1296 | Canvas regression follow-up | [TASK-003](./product-and-tasks/graph-editing-and-versioned-saves/spec.md) |

## PLAN.md

| Source lines | Section or evidence block | Destination |
| --- | --- | --- |
| 1–4 | Weft Evolution Plan | [TASK-001](./product-and-tasks/release-one-product-contracts/spec.md), [ADR 10](../docs/adr/0010-local-first-delivery.md) |
| 5–34 | Delivery Order and Final Acceptance (updated 2026-10-19) | [PROD-04](./production-readiness/prod-04/spec.md), [ADR 10](../docs/adr/0010-local-first-delivery.md) |
| 35–48 | Task-oriented UX and Standalone Delivery Plan (2026-10-19) | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md), [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md), [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md) |
| 49–91 | Phase A — Daily work first | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md), [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 92–121 | Phase B — Independent standalone installation | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md), [TASK-008](./product-and-tasks/registered-projects-and-task-worktrees/spec.md), [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [ADR 11](../docs/adr/0011-execution-boundaries.md) |
| 122–132 | Evidence and acceptance | [PROD-04](./production-readiness/prod-04/spec.md), [PROD-11](./production-readiness/prod-11/spec.md), [ADR 10](../docs/adr/0010-local-first-delivery.md) |
| 133–142 | Product Principles | [TASK-001](./product-and-tasks/release-one-product-contracts/spec.md), [ADR 10](../docs/adr/0010-local-first-delivery.md) |
| 143–144 | Release 1 — Build and Operate Graphs Through the Interface | [TASK-001](./product-and-tasks/release-one-product-contracts/spec.md) |
| 145–154 | 1. Remove Out-of-Scope Product Functionality | [TASK-001](./product-and-tasks/release-one-product-contracts/spec.md) |
| 155–167 | 2. Complete Model Configuration | [INT-001](./providers-and-integrations/built-in-inference-plugin-management/spec.md), [INT-002](./providers-and-integrations/model-provider-adapters-and-conformance/spec.md) |
| 168–182 | 3. Turn the Canvas into a Graph Editor | [TASK-003](./product-and-tasks/graph-editing-and-versioned-saves/spec.md) |
| 183–208 | 4. Provide Containerized Project Environments | [ENV-005](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md), [ENV-006](./workspaces-and-environments/sandbox-capacity-and-idle-resume/spec.md), [ENV-007](./workspaces-and-environments/environment-discovery-and-reviewed-definitions/spec.md), [ENV-008](./workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md), [ENV-009](./workspaces-and-environments/environment-network-policy-and-egress/spec.md) |
| 209–226 | 5. Create the File and Interaction Panel | [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md), [ENV-003](./workspaces-and-environments/workspace-change-journal-and-attribution/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 227–253 | 5a. Human Review with Managed or External Previews | [ENV-004](./workspaces-and-environments/durable-human-review-and-protected-previews/spec.md) |
| 254–283 | Review choices and evidence | [ENV-004](./workspaces-and-environments/durable-human-review-and-protected-previews/spec.md) |
| 284–315 | Managed application sessions | [ENV-004](./workspaces-and-environments/durable-human-review-and-protected-previews/spec.md) |
| 316–359 | Access from browsers and HTTP clients | [ENV-004](./workspaces-and-environments/durable-human-review-and-protected-previews/spec.md) |
| 360–393 | Gate outcomes and delivery | [ENV-004](./workspaces-and-environments/durable-human-review-and-protected-previews/spec.md), [RUN-005](./runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md) |
| 394–632 | 6. Add MCP Configuration | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 633–645 | 7. Add Skills and Steering | [RUN-001](./runtime-and-delivery/skills-and-project-steering/spec.md) |
| 646–894 | 8. Allow Dynamic Delegation to Subagents | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 895–906 | 9. Prepare Local Docker Operation | [OPS-001](./installation-and-operations/compose-installation-and-operations/spec.md) |
| 907–939 | 10. First-use guidance — global onboarding withdrawn | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md) |
| 940–951 | Release 2 — Reusable Components | [RELEASE-2](./reusable-components/spec.md) |
| 952–992 | Suggested Implementation Sequence | [ADR 10](../docs/adr/0010-local-first-delivery.md), [ENV-006](./workspaces-and-environments/sandbox-capacity-and-idle-resume/spec.md), [ENV-007](./workspaces-and-environments/environment-discovery-and-reviewed-definitions/spec.md), [ENV-009](./workspaces-and-environments/environment-network-policy-and-egress/spec.md) |
| 993–1048 | UX Reorganization — Incremental Delivery | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [TASK-003](./product-and-tasks/graph-editing-and-versioned-saves/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 1049–1060 | Installation validation of the navigation baseline | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md) |
| 1061–1077 | Visual consolidation | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md) |
| 1078–1089 | Visual consolidation installation validation | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md) |
| 1090–1208 | Developer usability validation — historical round (2026-10-17; continuation deferred to final acceptance on 2026-10-18) | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [PROD-04](./production-readiness/prod-04/spec.md) |
| 1209–1338 | UX-01–UX-05 implementation evidence (2026-10-17) | [TASK-002](./product-and-tasks/console-navigation-and-configuration/spec.md), [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [PROD-11](./production-readiness/prod-11/spec.md) |
| 1339–1346 | Release 1 operations runbook (2026-10-18) | [OPS-001](./installation-and-operations/compose-installation-and-operations/spec.md), [PROD-02](./production-readiness/prod-02/spec.md), [PROD-06](./production-readiness/prod-06/spec.md), [PROD-09](./production-readiness/prod-09/spec.md) |
| 1347–1355 | Compose installation contract (2026-10-18) | [OPS-001](./installation-and-operations/compose-installation-and-operations/spec.md), [ADR 9](../docs/adr/0009-dedicated-sandbox-daemon.md) |
| 1356–1363 | Managed MCP hosting design (2026-10-18) | [INT-008](./providers-and-integrations/managed-mcp-hosting/spec.md) |
| 1364–1375 | Native child grant inheritance (2026-10-18) | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 1376–1383 | Dependency health contract (2026-10-18) | [OPS-001](./installation-and-operations/compose-installation-and-operations/spec.md), [ENV-005](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md) |
| 1384–1392 | Delegation entity/state contract (2026-10-18) | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 1393–1401 | Initial installation identity (2026-10-18) | [OPS-001](./installation-and-operations/compose-installation-and-operations/spec.md), [PROD-01](./production-readiness/prod-01/spec.md), [ADR 12](../docs/adr/0012-installation-identity.md) |
| 1402–1414 | Bounded native recursive delegation (2026-10-18) | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 1415–1422 | Pause admission race correction (2026-10-18) | [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 1423–1432 | Shared pending MCP OAuth transactions (2026-10-18) | [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md) |
| 1433–1443 | Parameterized MCP templates and reviewed upgrades (2026-10-18) | [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md) |
| 1444–1453 | Atlassian v2 flat catalogue foundation (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1454–1469 | Atlassian operation authorization (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1470–1480 | Graph state reconciliation preserves operator commands (2026-10-18) | [OPS-001](./installation-and-operations/compose-installation-and-operations/spec.md) |
| 1481–1492 | Workspace inspection response isolation (2026-10-18) | [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md) |
| 1493–1504 | Atlassian Basic credentials (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1505–1517 | Typed MCP headers for dbt and standalone LangSmith (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1518–1528 | Sentry MCP configuration profile (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1529–1542 | Databricks workspace/service configuration (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1543–1557 | External parents requesting native delegation (2026-10-18) | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 1558–1570 | Canvas connection measurement recovery (2026-10-18) | [TASK-003](./product-and-tasks/graph-editing-and-versioned-saves/spec.md) |
| 1571–1581 | Public MCP live qualification (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1582–1594 | Warehouse MCP qualification boundary (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1595–1605 | Concurrent graph scope acceptance (2026-10-18) | [RUN-001](./runtime-and-delivery/skills-and-project-steering/spec.md), [PROD-04](./production-readiness/prod-04/spec.md) |
| 1606–1613 | Delegation contract reconciliation (2026-10-18) | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 1614–1620 | Context7 public discovery evidence (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1621–1639 | Release 1 verification checkpoint (2026-10-18) | [PROD-04](./production-readiness/prod-04/spec.md), [PROD-11](./production-readiness/prod-11/spec.md), [RUN-001](./runtime-and-delivery/skills-and-project-steering/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1640–1653 | Explicit stateless MCP transport (2026-10-18) | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 1654–1672 | Atlassian product/site read policy (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1673–1695 | Consolidated Release 1 browser journey (2026-10-18) | [PROD-04](./production-readiness/prod-04/spec.md) |
| 1696–1705 | Context7 public lookup acceptance (2026-10-18) | [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1706–1715 | MCP contract documentation reconciliation (2026-10-18) | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md), [INT-006](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1716–1730 | Provider browser configuration and credential acceptance (2026-10-18) | [INT-001](./providers-and-integrations/built-in-inference-plugin-management/spec.md) |
| 1731–1746 | Release 1 continuation verification checkpoint (2026-10-18) | [PROD-04](./production-readiness/prod-04/spec.md), [PROD-11](./production-readiness/prod-11/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) |
| 1747–1770 | Release 1 exact MCP argument restrictions (2026-10-18) | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 1771–1788 | Provider management E2E isolation (2026-10-18) | [INT-001](./providers-and-integrations/built-in-inference-plugin-management/spec.md), [PROD-11](./production-readiness/prod-11/spec.md) |
| 1789–1799 | FLOW-03a — Durable task drafts (2026-10-19) | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md) |
| 1800–1814 | Task execution admission evidence — 2026-10-19 | [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 1815–1827 | Project/worktree foundation evidence — 2026-10-19 | [TASK-008](./product-and-tasks/registered-projects-and-task-worktrees/spec.md) |
| 1828–1837 | Structured task ingress evidence — 2026-10-19 | [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 1838–1847 | Standalone dependency audit — 2026-10-19 | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md) |
| 1848–1864 | Initial native test baseline — 2026-10-19 | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md) |
| 1865–1875 | Native offline recovery delivery (2026-10-19) | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md) |
| 1876–1884 | Native editor handoff | [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md) |
| 1885–1893 | Synthetic first-use path | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md) |
| 1894–1902 | Task-local review evidence | [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md) |
| 1903–1915 | First native workflow — user decision, 2026-10-19 | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md) |
| 1916–1925 | Short first-task path | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md) |
| 1926–1933 | Native token recovery evidence | [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md), [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md) |
| 1934–1946 | Local MCP transport foundation | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 1947–1955 | Callback completion boundary | [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 1956–1968 | Reviewed native MCP registration | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 1969–1978 | Native format-1 upgrade qualification | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md), [PROD-02](./production-readiness/prod-02/spec.md) |
| 1979–1989 | Contextual task decisions | [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md) |
| 1990–1998 | Inline task execution evidence | [TASK-004](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md), [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 1999–2007 | Task requirement repair before admission | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md) |
| 2008–2018 | Task workspace review scopes | [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md) |
| 2019–2026 | Preparation failure visibility | [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 2027–2036 | Reviewed native task worktree cleanup | [TASK-008](./product-and-tasks/registered-projects-and-task-worktrees/spec.md) |
| 2037–2047 | Explicit task environment selection | [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md), [ADR 11](../docs/adr/0011-execution-boundaries.md) |
| 2048–2056 | Remembered project environment preference | [TASK-008](./product-and-tasks/registered-projects-and-task-worktrees/spec.md) |
| 2057–2069 | Separate task environment build and execution admission | [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md), [ENV-008](./workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md), [ADR 11](../docs/adr/0011-execution-boundaries.md) |
| 2070–2080 | FLOW-04 — Issue references through existing MCP grants (2026-10-19) | [RUN-006](./runtime-and-delivery/issue-references-and-polling-triggers/spec.md) |
| 2081–2089 | FLOW-07e — Explicit delivery evidence states (2026-10-19) | [ENV-002](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md), [RUN-005](./runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md) |
| 2090–2107 | LOCAL-07a — Native production-binary qualification (2026-10-19) | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md), [PROD-04](./production-readiness/prod-04/spec.md) |
| 2108–2126 | FLOW-08 — Final automated flow regression (2026-10-19) | [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md), [PROD-04](./production-readiness/prod-04/spec.md), [PROD-11](./production-readiness/prod-11/spec.md) |
| 2127–2147 | Native terminal feedback and browser-stream shutdown (2026-10-19) | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md) |
| 2148–2171 | Paired MCP resource arguments (2026-10-19) | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 2172–2194 | MCP remote access diagnostics (2026-10-19) | [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
| 2195–2241 | Prompt-first New task and authorization modes — agreed, not implemented (2026-10-19) | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md), [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md) |
| 2242–2272 | PROMPT-01 — Prompt-first editor (2026-10-19) | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md) |
| 2273–2308 | PROMPT-02 — Durable request interpretation (2026-10-19) | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md) |
| 2309–2360 | PROMPT-03 — Public repository snapshots (2026-10-19) | [ENV-001](./workspaces-and-environments/repository-snapshot-imports/spec.md) |
| 2361–2411 | PROMPT-04 — Explicit private Git authentication (2026-10-19) | [ENV-001](./workspaces-and-environments/repository-snapshot-imports/spec.md) |
| 2412–2458 | PROMPT-05 — Conditional prompt admission (2026-10-19) | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md), [TASK-007](./product-and-tasks/task-execution-admission-and-recovery/spec.md) |
| 2459–2463 | AUTH-01 — Task authorization contract | [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md) |
| 2464–2478 | AUTH-02 — Gateway and task consent | [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md) |
| 2479–2492 | AUTH-03 — Suggestions and live reduction | [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md), [TASK-008](./product-and-tasks/registered-projects-and-task-worktrees/spec.md) |
| 2493–2504 | PROMPT-06 — Task journey and decision history | [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md), [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md) |
| 2505–2546 | PROMPT-07 — Integrated qualification | [PROD-04](./production-readiness/prod-04/spec.md), [TASK-005](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md), [TASK-006](./product-and-tasks/task-consent-and-gateway-authorization/spec.md), [ENV-001](./workspaces-and-environments/repository-snapshot-imports/spec.md) |

## OLLAMA-PLAN.md

| Source lines | Section or evidence block | Destination |
| --- | --- | --- |
| 1–15 | Objective and starting point | [ADR 10](../docs/adr/0010-local-first-delivery.md), [INT-002](./providers-and-integrations/model-provider-adapters-and-conformance/spec.md) |
| 16–24 | Phase 0 — Provider baseline and conformance | [INT-002](./providers-and-integrations/model-provider-adapters-and-conformance/spec.md), [INT-003](./providers-and-integrations/out-of-process-provider-plugins/spec.md) |
| 25–30 | Phase 1 — Monetary controls | [INT-004](./providers-and-integrations/monetary-usage-and-budget-controls/spec.md), [RUN-002](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) |
| 31–37 | Phase 2 — Gateway push and pull requests | [RUN-005](./runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md) |
| 38–42 | Phase 3 — Issue triggers | [RUN-006](./runtime-and-delivery/issue-references-and-polling-triggers/spec.md) |
| 43–53 | Phase 4 — Native coding runtime | [RUN-003](./runtime-and-delivery/native-coding-runtime/spec.md), [RUN-004](./runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md) |
| 54–63 | Phase 5 — Remaining local Release 1 work | [ENV-003](./workspaces-and-environments/workspace-change-journal-and-attribution/spec.md), [OPS-001](./installation-and-operations/compose-installation-and-operations/spec.md), [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md), [PROD-04](./production-readiness/prod-04/spec.md) |
| 64–75 | External accounts, APIs and deferred work | [INT-002](./providers-and-integrations/model-provider-adapters-and-conformance/spec.md), [INT-007](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md), [RUN-005](./runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md), [RUN-006](./runtime-and-delivery/issue-references-and-polling-triggers/spec.md), [ENV-005](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md), [PROD-01](./production-readiness/prod-01/spec.md), [PROD-14](./production-readiness/prod-14/spec.md), [RELEASE-2](./reusable-components/spec.md) |
| 76–84 | Risks and decisions | [INT-003](./providers-and-integrations/out-of-process-provider-plugins/spec.md), [RUN-004](./runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md), [ADR 10](../docs/adr/0010-local-first-delivery.md) |
| 85–116 | Provider plugins and POC compatibility | [INT-001](./providers-and-integrations/built-in-inference-plugin-management/spec.md), [INT-003](./providers-and-integrations/out-of-process-provider-plugins/spec.md), [PROD-02](./production-readiness/prod-02/spec.md) |
| 117–120 | Progress heading and former branch metadata | Discarded — superseded branch instructions; Git retains the available history |
| 121–124 | Built-in plugin and provider conformance progress | [INT-001](./providers-and-integrations/built-in-inference-plugin-management/spec.md), [INT-002](./providers-and-integrations/model-provider-adapters-and-conformance/spec.md), [INT-003](./providers-and-integrations/out-of-process-provider-plugins/spec.md) |
| 125–129 | Cost, SCM and issue trigger progress | [INT-004](./providers-and-integrations/monetary-usage-and-budget-controls/spec.md), [RUN-005](./runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md), [RUN-006](./runtime-and-delivery/issue-references-and-polling-triggers/spec.md) |
| 130–149 | Native runtime and local model measurements | [RUN-003](./runtime-and-delivery/native-coding-runtime/spec.md), [RUN-004](./runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md) |
| 150–157 | Hosted baseline and harder-case decision | [RUN-004](./runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md), [PROD-13](./production-readiness/prod-13/spec.md) |
| 158–160 | Native Ollama and clean install | [OPS-002](./installation-and-operations/standalone-installation-and-recovery/spec.md), [INT-004](./providers-and-integrations/monetary-usage-and-budget-controls/spec.md), [OPS-001](./installation-and-operations/compose-installation-and-operations/spec.md), [PROD-04](./production-readiness/prod-04/spec.md) |
| 161–179 | Journal recovery and local rollout | [ENV-003](./workspaces-and-environments/workspace-change-journal-and-attribution/spec.md), [ENV-005](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md), [PROD-08](./production-readiness/prod-08/spec.md), [PROD-09](./production-readiness/prod-09/spec.md), [PROD-10](./production-readiness/prod-10/spec.md) |
| 180–186 | Historical check environment and known failures | [PROD-11](./production-readiness/prod-11/spec.md), [ENV-001](./workspaces-and-environments/repository-snapshot-imports/spec.md), [INT-005](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) |
