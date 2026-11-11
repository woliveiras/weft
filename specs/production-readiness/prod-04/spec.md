---
id: PROD-04
title: Final Release 1 acceptance
summary: "Qualify the complete installed workflow and developer experience before production."
status: draft
scope: [production readiness]
risk: production blocker
risk_domains: [security, isolation, user experience]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-04/matrix.md]
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

- **AC-001 — Installed journey.** A developer follows the documented clean install and first-use path. The journey covers login, model, MCP, grants, visual editor, environment, task, execution, files/diff and required approval. Both delegation approval modes are exercised.
- **AC-002 — Secrets.** Credentials are saved and used, including a failed provider call. Stored secrets are absent from browser responses and storage, runtime input, prompts and logs.
- **AC-003 — Isolation.** Two graphs run with distinct guidance and files, including Docker sandbox and explicitly selected native runs. Cross-scope access is denied. Docker runs stay on the dedicated daemon and reset preserves unrelated and persistent state. Native evidence identifies its host boundary.
- **AC-004 — Developer validation.** Remaining Release 1 implementation is ready. A developer validates navigation, MCP connection, agent configuration and execution tracking. Completion records developer feedback and green relevant backend, SDK, browser and Docker checks with their coverage and limitations.

## Open decisions

- Acceptance environment, accounts and participants; timing of the deferred walkthrough and approvals for images, models and real services.

## Reconciliation

- Governing inputs: `original TASKS.md section 11; product plan; local-first phase 5`. [Production hardening](../../../docs/production-hardening.md#isolation-of-agent-work) and [deployment](../../../docs/deployment.md). [Release 1 product plan](../../../wip/PLAN.md#release-1--build-and-operate-graphs-through-the-interface) and [sandbox acceptance](../../../docs/sandbox-acceptance.md).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
