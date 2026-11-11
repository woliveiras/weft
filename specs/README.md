# Weft specification index

Release 1 is in progress. Keep local and fixture-based delivery first and run
automated regression checks with each delivery. The delivery order is Phase A
(task-oriented UX), Phase B (standalone), then Phase C (issue to pull request).
The main Phase A–C paths are delivered. Finish remaining Release 1 work before
the consolidated clean-install exercise and developer walkthrough. Real-account
validation needs separate authorization.

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
