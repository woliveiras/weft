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

## Product and tasks

- [TASK-001 — Release 1 Product Contracts](./product-and-tasks/release-one-product-contracts/spec.md) — `implemented`. Define shared release boundaries, observable errors, and audit identity.
- [TASK-002 — Console Navigation and Configuration](./product-and-tasks/console-navigation-and-configuration/spec.md) — `implemented`. Organize daily work and configuration, preserving route compatibility and contextual guidance.
- [TASK-003 — Graph Editing and Versioned Saves](./product-and-tasks/graph-editing-and-versioned-saves/spec.md) — `implemented`. Edit complete graph definitions in the canvas and recover safely from validation and save conflicts.
- [TASK-004 — Execution Activity and Run Diagnostics](./product-and-tasks/execution-activity-and-run-diagnostics/spec.md) — `implemented`. Expose execution state, pending decisions and bounded health diagnostics through Activity and Runs.
- [TASK-005 — Prompt-first Tasks and Drafts](./product-and-tasks/prompt-first-tasks-and-drafts/spec.md) — `implemented`. Save and interpret a task request, review suggestions and retain the draft through corrections.
- [TASK-006 — Task Consent and Gateway Authorization](./product-and-tasks/task-consent-and-gateway-authorization/spec.md) — `implemented`. Bind task authorization to reviewed scope and preserve gateway grants and mandatory approvals.
- [TASK-007 — Task Execution Admission and Recovery](./product-and-tasks/task-execution-admission-and-recovery/spec.md) — `implemented`. Start a reviewed workflow copy once, preserve its scoped execution state and recover without inventing success.
- [TASK-008 — Registered Projects and Task Worktrees](./product-and-tasks/registered-projects-and-task-worktrees/spec.md) — `implemented`. Register repositories and create isolated task worktrees with reviewed cleanup.

## Workspaces and environments

- [ENV-001 — Repository Snapshot Imports](./workspaces-and-environments/repository-snapshot-imports/spec.md) — `implemented`. Import reviewed Git snapshots and retain safe retry, authentication and worktree boundaries.
- [ENV-002 — Read-only Workspace and Git Review](./workspaces-and-environments/read-only-workspace-and-git-review/spec.md) — `implemented`. Inspect authorized files and Git evidence and hand off a selected workspace for human review.
- [ENV-003 — Workspace Change Journal and Attribution](./workspaces-and-environments/workspace-change-journal-and-attribution/spec.md) — `in-progress`. Record bounded change evidence, preserve honest attribution and recover the Linux workspace journal.
- [ENV-004 — Durable Human Review and Protected Previews](./workspaces-and-environments/durable-human-review-and-protected-previews/spec.md) — `implemented`. Bind review to a durable candidate and expose protected Docker previews through the gateway.
- [ENV-005 — Dedicated Docker Sandbox Isolation](./workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md) — `implemented`. Run Docker-isolated agents on the dedicated daemon and keep sandbox resources and access separate from the host.
- [ENV-006 — Sandbox Capacity and Idle Resume](./workspaces-and-environments/sandbox-capacity-and-idle-resume/spec.md) — `implemented`. Bound preparation and execution resources and resume idle containers through admission.
- [ENV-007 — Environment Discovery and Reviewed Definitions](./workspaces-and-environments/environment-discovery-and-reviewed-definitions/spec.md) — `implemented`. Discover repository environment candidates and require review before adopting a definition or an agent proposal.
- [ENV-008 — Environment Build, Reuse and Cleanup](./workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md) — `implemented`. Build and reuse compatible environments while retaining isolated workspaces and protecting referenced generations.
- [ENV-009 — Environment Network Policy and Egress](./workspaces-and-environments/environment-network-policy-and-egress/spec.md) — `implemented`. Enforce installation policy across environment preparation and execution and expose bounded proxy denial history.

## Production readiness

The first four items are production blockers in the order listed. The remaining
items can progress in parallel, subject to the dependencies below.

- [PROD-01 — Production sign-in](./production-readiness/prod-01/spec.md) — Open, blocker 1. Completes AUTH-OIDC, deferred for local-first delivery.
- [PROD-02 — Versioned production upgrades](./production-readiness/prod-02/spec.md) — Open, blocker 2. Qualify the release-to-release upgrade path.
- [PROD-03 — Production model providers and budgets](./production-readiness/prod-03/spec.md) — Open, blocker 3. Requires approved paid service and data terms, verified prices, and project/user budget settings.
- [PROD-04 — Final Release 1 acceptance](./production-readiness/prod-04/spec.md) — Open, blocker 4. After remaining Release 1 work and with authorized test resources.
- [PROD-05 — Production security assessment](./production-readiness/prod-05/spec.md) — Open. Final assessment covers the selected topology and security boundaries.
- [PROD-06 — Managed secrets and key rotation](./production-readiness/prod-06/spec.md) — Open. Vault/KMS choice and authorized rotation qualification pending.
- [PROD-07 — Production approval defaults](./production-readiness/prod-07/spec.md) — Open. Review new-workflow defaults and threat model.
- [PROD-08 — Production topology and workspace storage](./production-readiness/prod-08/spec.md) — Open. Choose deployment shape before the dedicated workspace filesystem migration.
- [PROD-09 — Timed backup and restore](./production-readiness/prod-09/spec.md) — Partial. Journal recovery exists. Time a full restore for the selected installation.
- [PROD-10 — Production SLOs and alert ownership](./production-readiness/prod-10/spec.md) — Partial. Journal alerts exist. Production SLOs, routing and ownership are open.
- [PROD-11 — Required GitHub CI checks](./production-readiness/prod-11/spec.md) — Open. Reconfirm the repository-import and MCP browser failures and qualify the full CI workflow.
- [PROD-12 — Production supply chain](./production-readiness/prod-12/spec.md) — Open. Build on CI artifact generation and agree on admission policy.
- [PROD-13 — Coding model acceptance](./production-readiness/prod-13/spec.md) — Open. Choose the production model with PROD-03 and agree on a repeated evaluation and solve-rate bar.
- [PROD-14 — Production integration qualification](./production-readiness/prod-14/spec.md) — Open for the external phase. Requires selected integrations and authorized accounts.
- [PROD-15 — Organizational approval](./production-readiness/prod-15/spec.md) — Open for company use. Requires approval of source-code transfer, data terms and acceptable use.

## Other Release 1 work

Where a standalone specification is pending, the link points to the current
contract or operating guide.

- [Workspace change provenance](../docs/operations.md#workspace-journal) — Partial. Exact execution attribution, unsupported runtime/PID boundaries and collector gaps remain open.
- [Delegation scope and cost](../docs/delegation.md) — Partial. Preserve explicit grants and empty child permissions by default. Broader runtime/profile support and enforceable monetary ceilings across delegation remain open.
- [Clean Compose installation](./production-readiness/prod-04/spec.md) — Open. Validate reachability and browser-only setup using the documentation after approval for required image downloads.
- [Native installation qualification (LOCAL-07)](./production-readiness/prod-04/spec.md) — Partial. Real local Ollama passed. VS Code/user review, real sandbox, external accounts and other platforms remain.
- [Vendor MCP and OAuth qualification](./production-readiness/prod-14/spec.md) — Partial. Finish vendor-specific auth, catalogue, scopes and live end-to-end checks, including DCR/CIMD where supported, in the external phase with authorized accounts.
- [Bedrock adapter (PROV-02)](../docs/extensibility.md#add-a-model-provider) — Open for the external phase. Live Vertex AI qualification is also pending.
- [Bitbucket Data Center delivery (SCM-05)](../docs/atlassian-bitbucket-setup.md) — Open for the external phase. Requires an evaluation license and approval for any image download.
- [Issue webhooks](../docs/task-workflows.md) — Deferred. Polling and fixtures precede authenticated webhooks.
- [External provider plugins (PROV-06)](../docs/extensibility.md#out-of-process-provider-plugins) — Deferred by maintainer on 2026-11-11 due to third-party code risk. Design is delivered. Revisit the dependency on PROV-02 only if implementation is authorized.
- [Rootless sandbox](../docs/sandbox-runtime.md) — Deferred by maintainer.
- [Native preview supervision and automatic PR/CI review adapters](../docs/sandbox-runtime.md#human-review-and-application-previews) — Deferred. Manual review and gateway-mediated SCM delivery are available.

## Agent guidance

- [Repository agent guidance](./repository-agent-guidance/spec.md) — Draft awaiting maintainer review of the source policy, precedence, loading and audit decisions.

## UX and Release 2

- [Developer usability walkthrough](./production-readiness/prod-04/spec.md) — Deferred by maintainer to final Release 1 acceptance. Repeat navigation, agent configuration, MCP connection and run tracking without facilitator help. Confirm the earlier graph-screen feedback. UX-01–UX-05 are delivered and the global onboarding panel was withdrawn.
- [Reusable components](./reusable-components/spec.md) — Release 2 is deferred until Release 1 is complete and stable and the maintainer authorizes it.
