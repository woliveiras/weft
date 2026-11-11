# Weft specification index

Release 1 is in progress. Keep local and fixture-based delivery first and run
automated regression checks with each delivery. The delivery order is Phase A
(task-oriented UX), Phase B (standalone), then Phase C (issue to pull request).
The main Phase A–C paths are delivered. Finish remaining Release 1 work before
the consolidated clean-install exercise and developer walkthrough. Real-account
validation needs separate authorization.

## Specification layout and identifiers

New specifications live at `specs/<area>/<slug>/spec.md`, with a sibling
`matrix.md` and an `evidence.md` when durable evidence is available. Use a
lowercase kebab-case slug that describes the capability. The 33 historical
capability specifications use the following area prefixes and three-digit
sequences. IDs and slugs remain stable after assignment.

| Area directory | ID range | Capabilities |
| --- | --- | --- |
| `product-and-tasks` | `TASK-001`–`TASK-008` | Release contracts, console navigation, graph editing, Activity, prompt tasks, consent, admission, and registered projects |
| `workspaces-and-environments` | `ENV-001`–`ENV-009` | Repository imports, workspace review, journal, human review, Docker isolation, capacity, definitions, build/reuse, and network policy |
| `providers-and-integrations` | `INT-001`–`INT-008` | Built-in inference plugins, adapters, external plugins, budgets, MCP invocation, MCP OAuth, vendor profiles, and hosting |
| `runtime-and-delivery` | `RUN-001`–`RUN-006` | Steering, delegation, native coding, evaluation, SCM, and issue triggers |
| `installation-and-operations` | `OPS-001`–`OPS-002` | Compose and standalone installations |

Keep existing specification IDs and paths. Use `implemented`, `in-progress`,
`planned`, `deferred` or `superseded` to describe capability status in the
index. Pending maintainer review is a separate condition.

Each acceptance criterion has one owning specification. A qualified reference
such as `ENV-003 AC-004` links to that owner's criterion. Production specs
reference capability criteria and own only the extra production acceptance
conditions. Historical task IDs remain source references.

Oracle provenance describes the source of each expectation. A retrospective
test or operating guide can establish an implementation-aware oracle.
Use spec-derived only when a recorded decision predates the behavior it
governs. Where a historical capability has only implementation-aware or
diagnostic evidence, independent qualification remains open.

The [historical source map](./historical-source-map.md) records the migration of the planning and evidence files. See the [architecture decisions](../docs/adr/README.md) for cross-cutting policy.

## Product and tasks

### Implemented

- [TASK-001 — Release 1 Product Contracts](./product-and-tasks/release-one-product-contracts/spec.md) — `implemented`. Define shared release boundaries, observable errors, and audit identity.
- [TASK-002 — Console Navigation and Configuration](./product-and-tasks/console-navigation-and-configuration/spec.md) — `implemented`. Organize daily work and configuration, preserving route compatibility and contextual guidance.
- [TASK-003 — Graph Editing and Versioned Saves](./product-and-tasks/graph-editing-and-versioned-saves/spec.md) — `implemented`. Edit complete graph definitions in the canvas and recover safely from validation and save conflicts.
- [TASK-004 — Execution Activity and Run Diagnostics](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md) — `implemented`. Expose execution state, pending decisions and bounded health diagnostics through Activity and Runs.
- [TASK-005 — Prompt-first Tasks and Drafts](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md) — `implemented`. Save and interpret a task request, review suggestions and retain the draft through corrections.
- [TASK-006 — Task Consent and Gateway Authorization](./product-and-tasks/task-consent-and-gateway-authorization/spec.md) — `implemented`. Bind task authorization to reviewed scope and preserve gateway grants and mandatory approvals.
- [TASK-007 — Task Execution Admission and Recovery](./product-and-tasks/task-execution-admission-and-recovery/spec.md) — `implemented`. Start a reviewed workflow copy once, preserve its scoped execution state and recover without inventing success.
- [TASK-008 — Registered Projects and Task Worktrees](./product-and-tasks/registered-projects-and-task-worktrees/spec.md) — `implemented`. Register repositories and create isolated task worktrees with reviewed cleanup.

## Workspaces and environments

### Implemented

- [ENV-001 — Repository Snapshot Imports](./workspaces-and-environments/repository-snapshot-imports/spec.md) — `implemented`. Import reviewed Git snapshots and retain safe retry, authentication and worktree boundaries.
- [ENV-002 — Read-only Workspace and Git Review](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md) — `implemented`. Inspect authorized files and Git evidence and hand off a selected workspace for human review.
- [ENV-004 — Durable Human Review and Protected Previews](./workspaces-and-environments/durable-human-review-and-protected-previews/spec.md) — `implemented`. Bind review to a durable candidate and expose protected Docker previews through the gateway.
- [ENV-005 — Dedicated Docker Sandbox Isolation](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md) — `implemented`. Run Docker-isolated agents on the dedicated daemon and keep sandbox resources and access separate from the host.
- [ENV-006 — Sandbox Capacity and Idle Resume](./workspaces-and-environments/sandbox-capacity-and-idle-resume/spec.md) — `implemented`. Bound preparation and execution resources and resume idle containers through admission.
- [ENV-007 — Environment Discovery and Reviewed Definitions](./workspaces-and-environments/environment-discovery-and-reviewed-definitions/spec.md) — `implemented`. Discover repository environment candidates and require review before adopting a definition or an agent proposal.
- [ENV-008 — Environment Build, Reuse and Cleanup](./workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md) — `implemented`. Build and reuse compatible environments while retaining isolated workspaces and protecting referenced generations.
- [ENV-009 — Environment Network Policy and Egress](./workspaces-and-environments/environment-network-policy-and-egress/spec.md) — `implemented`. Enforce installation policy across environment preparation and execution and expose bounded proxy denial history.

### In progress

- [ENV-003 — Workspace Change Journal and Attribution](./workspaces-and-environments/workspace-change-journal-and-attribution/spec.md) — `in-progress`. Record bounded change evidence, preserve honest attribution and recover the Linux workspace journal.

## Providers and integrations

### Implemented

- [INT-001 — Built-in Inference Plugin Management](./providers-and-integrations/built-in-inference-plugin-management/spec.md) — `implemented`. Manage built-in plugin availability and provider instances through the installation console and API.
- [INT-004 — Monetary Usage and Budget Controls](./providers-and-integrations/monetary-usage-and-budget-controls/spec.md) — `implemented`. Record model spend and enforce applicable budgets with revisioned pricing and conservative reservations.
- [INT-005 — MCP Connections and Tool Invocation](./providers-and-integrations/mcp-connections-and-tool-invocation/spec.md) — `implemented`. Register MCP endpoints and enforce exact, scoped tool authorization through the gateway.
- [INT-006 — MCP Templates and Browser OAuth](./providers-and-integrations/mcp-templates-and-browser-oauth/spec.md) — `implemented`. Open versioned connection templates and complete reviewed browser authorization within the supported client setup.

### In progress

- [INT-002 — Model Provider Adapters and Conformance](./providers-and-integrations/model-provider-adapters-and-conformance/spec.md) — `in-progress`. Apply a shared provider contract and distinguish adapter delivery from live qualification.
- [INT-007 — MCP Vendor Profiles and Qualification](./providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md) — `in-progress`. Preserve provider-specific connection and resource boundaries and qualify each selected profile separately.

### Planned

- [INT-008 — Managed MCP Hosting](./providers-and-integrations/managed-mcp-hosting/spec.md) — `planned`. Preserve the reviewed design for isolated, installation-managed MCP server generations.

### Deferred

- [INT-003 — Out-of-process Provider Plugins](./providers-and-integrations/out-of-process-provider-plugins/spec.md) — `deferred`. Preserve the Provider Protocol v1 design while external plugin execution remains deferred.

## Runtime and delivery

### Implemented

- [RUN-001 — Skills and Project Steering](./runtime-and-delivery/skills-and-project-steering/spec.md) — `implemented`. Compose explicitly selected managed skills and bounded project steering and preserve the execution snapshot.
- [RUN-003 — Native Coding Runtime](./runtime-and-delivery/native-coding-runtime/spec.md) — `implemented`. Keep coding turns, tool execution and verification within Weft's native runtime and gateways.
- [RUN-004 — Coding Evaluation and Model Benchmarks](./runtime-and-delivery/coding-evaluation-and-model-benchmarks/spec.md) — `implemented`. Measure coding workflows with independent task checks and retain the conditions and limits of each benchmark.
- [RUN-006 — Issue References and Polling Triggers](./runtime-and-delivery/issue-references-and-polling-triggers/spec.md) — `implemented`. Read reviewed issue references and start eligible labeled work through consent-bound polling.

### In progress

- [RUN-002 — Dynamic Delegation and Execution Trees](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) — `in-progress`. Run bounded native children under inherited grants and preserve execution-tree control and token accounting.
- [RUN-005 — Gateway-mediated Source Control Delivery](./runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md) — `in-progress`. Push and open pull requests through the reviewed SCM gateway with task-scoped credentials and evidence.

## Installation and operations

### In progress

- [OPS-001 — Compose Installation and Operations](./installation-and-operations/compose-installation-and-operations/spec.md) — `in-progress`. Operate the local Compose installation through documented bootstrap, health and maintenance boundaries.
- [OPS-002 — Standalone Installation and Recovery](./installation-and-operations/standalone-installation-and-recovery/spec.md) — `in-progress`. Package the native profile, preserve offline data and qualify installation lifecycle on each supported platform.

## Production readiness

The first four items are production blockers in the order listed. The remaining
items can progress in parallel, subject to the dependencies below.

### Blockers in delivery order

- [PROD-01 — Production sign-in](./production-readiness/prod-01/spec.md) — `planned`, blocker 1. Completes AUTH-OIDC, deferred for local-first delivery.
- [PROD-02 — Versioned production upgrades](./production-readiness/prod-02/spec.md) — `planned`, blocker 2. Qualify the release-to-release upgrade path.
- [PROD-03 — Production model providers and budgets](./production-readiness/prod-03/spec.md) — `planned`, blocker 3. Requires approved paid service and data terms, verified prices, and project/user budget settings.
- [PROD-04 — Final Release 1 acceptance](./production-readiness/prod-04/spec.md) — `planned`, blocker 4. After remaining Release 1 work and with authorized test resources.
### Planned qualification

- [PROD-05 — Production security assessment](./production-readiness/prod-05/spec.md) — `planned`. Final assessment covers the selected topology and security boundaries.
- [PROD-06 — Managed secrets and key rotation](./production-readiness/prod-06/spec.md) — `planned`. Vault/KMS choice and authorized rotation qualification pending.
- [PROD-07 — Production approval defaults](./production-readiness/prod-07/spec.md) — `planned`. Review new-workflow defaults and threat model.
- [PROD-08 — Production topology and workspace storage](./production-readiness/prod-08/spec.md) — `planned`. Choose deployment shape before the dedicated workspace filesystem migration.
- [PROD-11 — Required GitHub CI checks](./production-readiness/prod-11/spec.md) — `planned`. Reconfirm the repository-import and MCP browser failures and qualify the full CI workflow.
- [PROD-12 — Production supply chain](./production-readiness/prod-12/spec.md) — `planned`. Build on CI artifact generation and agree on admission policy.
- [PROD-13 — Coding model acceptance](./production-readiness/prod-13/spec.md) — `planned`. Choose the production model with PROD-03 and agree on a repeated evaluation and solve-rate bar.
- [PROD-14 — Production integration qualification](./production-readiness/prod-14/spec.md) — `planned` for the external phase. Requires selected integrations and authorized accounts.
- [PROD-15 — Organizational approval](./production-readiness/prod-15/spec.md) — `planned` for company use. Requires approval of source-code transfer, data terms and acceptable use.

### In progress

- [PROD-09 — Timed backup and restore](./production-readiness/prod-09/spec.md) — `in-progress`. Journal recovery exists. Time a full restore for the selected installation.
- [PROD-10 — Production SLOs and alert ownership](./production-readiness/prod-10/spec.md) — `in-progress`. Journal alerts exist. Production SLOs, routing and ownership are open.

### Dependencies

- PROD-04 follows the remaining Release 1 work, the preceding blockers and
  authorization for the installation and external targets under test.
- PROD-05, PROD-09 and PROD-10 qualify the topology selected under PROD-08.
  PROD-08 uses ENV-003 kernel evidence when qualifying a dedicated workspace
  filesystem. PROD-09 and PROD-10 reuse ENV-003 replay, pruning and metrics.
- PROD-12 depends on CI artifact generation under PROD-11.
- PROD-13 depends on the model and data terms selected under PROD-03 and on
  the evaluation criteria in RUN-004. PROD-14 depends on the selected
  adapters and approved accounts. PROD-15 applies to organizational use.
- Task admission depends on reviewed consent, project/workspace ownership
  and the selected environment. Gateway SCM delivery precedes issue-trigger
  completion write-back. Capability-specific dependencies are in spec metadata.

## Maintainer decisions and pending qualification

- [Final Release 1 acceptance](./production-readiness/prod-04/spec.md) — The
  maintainer deferred the clean-install exercise and unaided developer
  walkthrough until remaining Release 1 work is ready. The walkthrough covers
  navigation, agent configuration, MCP connection and execution tracking.
- [Native qualification](./installation-and-operations/standalone-installation-and-recovery/spec.md) —
  Actual VS Code/user review, optional real native sandbox and additional
  operating systems remain open.
- [Vendor qualification](./production-readiness/prod-14/spec.md) — Selected
  MCP DCR/CIMD and vendor scopes, Vertex AI, Bedrock, GitHub/Jira and Bitbucket
  Data Center require their own qualification and authorized resources.
- [Workspace journal](./workspaces-and-environments/workspace-change-journal-and-attribution/spec.md) —
  Arbitrary execution attribution and unsupported runtime/PID boundaries remain open.
- [Delegation](./runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md) —
  Broader runtime/profile coverage, crash qualification and a tree-wide
  monetary ceiling remain open.
- [Docker isolation](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md) —
  Rootless remains deferred by the maintainer.
- [Human review](./workspaces-and-environments/durable-human-review-and-protected-previews/spec.md) —
  Native preview supervision and automatic PR/CI review adapters remain deferred.
- [Issue triggers](./runtime-and-delivery/issue-references-and-polling-triggers/spec.md) —
  Authenticated webhooks remain deferred.
- [External provider plugins](./providers-and-integrations/out-of-process-provider-plugins/spec.md) —
  The maintainer deferred PROV-06 on 2026-11-11 due to third-party code risk.
- [Console navigation](./product-and-tasks/console-navigation-and-configuration/spec.md#history) —
  The maintainer withdrew global onboarding on 2026-10-18.

## Agent guidance

- [REPO-GUIDANCE — Load agent guidance from repository workspaces](./repository-agent-guidance/spec.md) —
  `planned`. Draft awaiting maintainer review of source policy, precedence,
  loading and audit decisions.

## Release 2

- [RELEASE-2 — Reusable graph components](./reusable-components/spec.md) —
  `deferred` until Release 1 is complete and stable and the maintainer authorizes it.
