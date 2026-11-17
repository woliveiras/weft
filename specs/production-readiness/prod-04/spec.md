---
id: PROD-04
title: Final Release 1 acceptance
summary: "Qualify the complete installed workflow and developer experience before production."
status: planned
scope: [production readiness]
risk: production blocker
risk_domains: [security, isolation, user experience]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-04/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: ["remaining Release 1 work and authorized test resources"]
---

# Final Release 1 acceptance

Qualify the complete installed workflow and developer experience before production.

## Behavior and invariants

Automated fixture flows cover both delegation modes. The clean-install exercise and developer walkthrough were deferred until the remaining Release 1 work is ready. Live provider/vendor checks need independent authorization. The global onboarding panel was withdrawn.

## Acceptance criteria

- **AC-001 — Installed journey.** Record the linked capability criteria exercised in one documented clean installation, including both delegation modes and the actual boundaries used.
- **AC-002 — Secrets.** Qualify credential handling across the selected installed flow and verify that all applicable sinks in the credential boundary remain clear, including after an upstream failure.
- **AC-003 — Isolation.** Qualify the linked Docker, native and scoped-guidance boundaries together with distinct workspaces and verify reset preservation on the selected installation.
- **AC-004 — Developer validation.** Remaining Release 1 implementation is ready. A developer validates navigation, MCP connection, agent configuration and execution tracking. Completion records developer feedback and green relevant backend, SDK, browser and Docker checks with their coverage and limitations.

## Open decisions

- Acceptance environment, accounts and participants; timing of the deferred walkthrough and approvals for images, models and real services.

## History

- 2026-10-18 to 2026-10-19. The maintainer deferred clean-install and developer walkthrough acceptance while fixture qualification continued. On 2026-11-11 this became the fourth production blocker.

## Capability criteria

- [TASK-002 AC-002](../../product-and-tasks/console-navigation-and-configuration/spec.md#ac-002--discover-and-edit-agents).
- [TASK-003 AC-002](../../product-and-tasks/graph-editing-and-versioned-saves/spec.md#ac-002--canvas-interaction).
- [TASK-004 AC-001](../../product-and-tasks/execution-activity-and-run-diagnostics/spec.md#ac-001--execution-identity-and-outcome).
- [TASK-005 AC-003](../../product-and-tasks/prompt-first-tasks-and-drafts/spec.md#ac-003--task-journey-and-first-example).
- [TASK-006 AC-001](../../product-and-tasks/task-consent-and-gateway-authorization/spec.md#ac-001--reviewed-authorization-modes).
- [TASK-007 AC-002](../../product-and-tasks/task-execution-admission-and-recovery/spec.md#ac-002--environment-and-preparation-gates).
- [ENV-002 AC-001](../../workspaces-and-environments/read-only-workspace-and-git-review/spec.md#ac-001--read-only-inspection).
- [ENV-005 AC-001](../../workspaces-and-environments/dedicated-docker-sandbox-isolation/spec.md#ac-001--dedicated-daemon-and-workspace).
- [ENV-008 AC-001](../../workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md#ac-001--build-lifecycle-and-progress).
- [ENV-008 AC-003](../../workspaces-and-environments/environment-build-reuse-and-cleanup/spec.md#ac-003--safe-cleanup).
- [INT-001 AC-002](../../providers-and-integrations/built-in-inference-plugin-management/spec.md#ac-002--managed-configuration-and-credential-isolation).
- [INT-005 AC-001](../../providers-and-integrations/mcp-connections-and-tool-invocation/spec.md#ac-001--connection-lifecycle-and-transport).
- [INT-006 AC-002](../../providers-and-integrations/mcp-templates-and-browser-oauth/spec.md#ac-002--oauth-transaction-and-credential-lifecycle).
- [RUN-001 AC-002](../../runtime-and-delivery/skills-and-project-steering/spec.md#ac-002--scoped-loading-and-immutable-audit).
- [RUN-002 AC-001](../../runtime-and-delivery/dynamic-delegation-and-execution-trees/spec.md#ac-001--reviewed-delegation-and-grants).
- [OPS-001 AC-001](../../installation-and-operations/compose-installation-and-operations/spec.md#ac-001--separate-bootstrap-and-application-setup).
- [OPS-002 AC-004](../../installation-and-operations/standalone-installation-and-recovery/spec.md#ac-004--platform-qualification-record).

## Reconciliation

- Supporting references: [Deployment](../../../docs/deployment.md), [task workflow contracts](../../../docs/task-workflows.md), and [sandbox acceptance](../../../docs/sandbox-acceptance.md).
- [Historical acceptance evidence](./evidence.md).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
