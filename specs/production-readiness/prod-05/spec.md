---
id: PROD-05
title: Production security assessment
summary: "Complete a security review or authorized penetration test of the production boundaries."
status: draft
scope: [production readiness]
risk: production qualification
risk_domains: [security, isolation, authorization]
review_policy: Maintainer review before implementation or live qualification
navigation: [specs/README.md, specs/production-readiness/prod-05/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: []
---

# Production security assessment

Complete a security review or authorized penetration test of the production boundaries.

## Behavior and invariants

Assess and record the actual boundary for each runtime. Repository instructions, issues and tool results are untrusted. Grants and approvals remain enforced at the gateway.

## Acceptance criteria

- **AC-001 — Scope.** The assessment reviews the selected production topology. The assessment covers sandbox isolation, network egress, approval enforcement and untrusted guidance from repositories, issues and tool outputs.
- **AC-002 — Findings.** A boundary is unverified or a control fails. The report records the result, risk and disposition. Completion requires the agreed remediation or risk acceptance criteria.

## Open decisions

- Review versus penetration test, reviewer and permitted environment, finding severity thresholds and risk acceptance authority.

## Reconciliation

- Supporting references: [Network controls](../../../docs/production-hardening.md#network), [agent isolation](../../../docs/production-hardening.md#isolation-of-agent-work), and [LLM safety](../../../docs/production-hardening.md#llm-safety).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
