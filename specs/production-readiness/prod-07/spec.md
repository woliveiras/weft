---
id: PROD-07
title: Production approval defaults
summary: "Protect new workflows with human approval on write tools, push and pull requests."
status: planned
scope: [production readiness]
risk: production qualification
risk_domains: [security, authorization]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-07/matrix.md]
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

- **AC-001 — New workflow.** A new production workflow defaults to human approval for granted write tools, push and PR. Verify the default with the linked gateway and SCM criteria.
- **AC-002 — Missing or denied consent.** Production acceptance includes a threat model and demonstrates the new default for rejection and absence of consent.

## Open decisions

- Definition of write-capable tools, applicable workflow creation paths, timeout/denial behavior and treatment of existing workflows.

## History

- 2026-11-11. The production checklist called for conservative defaults for new workflows.

## Capability criteria

- [TASK-006 AC-001](../../product-and-tasks/task-consent-and-gateway-authorization/spec.md#ac-001--reviewed-authorization-modes).
- [RUN-005 AC-001](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md#ac-001--bounded-push).
- [RUN-005 AC-002](../../runtime-and-delivery/gateway-mediated-source-control-delivery/spec.md#ac-002--idempotent-pull-request-and-hand-off).

## Reconciliation

- Supporting references: [Agent isolation](../../../docs/production-hardening.md#isolation-of-agent-work) and [LLM safety](../../../docs/production-hardening.md#llm-safety).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
