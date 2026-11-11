---
id: PROD-12
title: Production supply chain
summary: "Make production artifacts traceable through pins, SBOMs, scans and signatures."
status: draft
scope: [production readiness]
risk: production qualification
risk_domains: [security, delivery]
review_policy: Maintainer review before implementation or live qualification
navigation: [wip/TASKS.md, specs/production-readiness/prod-12/matrix.md]
documentation: Review affected operating guides during delivery
authority:
  granted: [specification drafting]
  withheld: [implementation, push, release, deploy, production changes, destructive actions, secret changes]
dependencies: []
---

# Production supply chain

Make production artifacts traceable through pins, SBOMs, scans and signatures.

## Behavior and invariants

The hardening guide recommends scanning for critical vulnerabilities and deploying by digest. Final enforcement and exceptions require an agreed production policy.

## Acceptance criteria

- **AC-001 — Artifact qualification.** CI builds a production artifact. It uses pinned inputs, produces an SBOM, scans dependencies and images, and signs the image identified by digest.
- **AC-002 — Admission failure.** An image fails signature verification or meets the agreed blocking scan policy. It does not qualify for deployment. The failure is visible and any exception follows the approved policy.

## Open decisions

- Signing identities, key custody, scanner and exception policy, admission mechanism and registry/mirroring requirements.

## Reconciliation

- Governing inputs: `hardening supply chain; deployment artifact handling`. [Production hardening](../../../docs/production-hardening.md#supply-chain) and [deployment](../../../docs/deployment.md).
- [Behavior and oracle matrix](./matrix.md).
- Record test review before implementation review. Capture fresh evidence when the agreed checks run.
