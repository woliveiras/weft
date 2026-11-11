---
id: PROD-07
title: Production approval defaults
summary: "Protect new workflows with human approval on write tools, push and pull requests."
status: draft
scope: [production readiness]
risk: production qualification
risk_domains: [security, authorization]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-07/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: []
---

# Production approval defaults

Protect new workflows with human approval on write tools, push and pull requests.

## Behavior and invariants

The scope is conservative defaults for new workflows. Existing grants, operator decisions and gateway controls still apply, including default-deny tool grants.

## Acceptance criteria

- **AC-001 — New workflow.** A new workflow tries a write-capable tool, push or pull request with the applicable grant. The gateway requires a human decision before the side effect. Grant enforcement still applies.
- **AC-002 — Missing or denied consent.** A required decision is rejected or absent. The protected operation has no side effect. The threat model explains enforcement and trust boundaries.

## Open decisions

- Definition of write-capable tools, applicable workflow creation paths, timeout/denial behavior and treatment of existing workflows.

## Reconciliation

- Governing inputs: `hardening grant and approval checklist`. [Production hardening](../../../docs/production-hardening.md#llm-safety) and [deployment](../../../docs/deployment.md).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
