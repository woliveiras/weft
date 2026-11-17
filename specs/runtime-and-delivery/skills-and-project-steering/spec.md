---
id: RUN-001
title: "Skills and Project Steering"
summary: "Compose explicitly selected managed skills and bounded project steering and preserve the execution snapshot."
status: implemented
scope: ["skills and project steering"]
navigation: ["specs/README.md","specs/runtime-and-delivery/skills-and-project-steering/matrix.md"]
documentation: ["docs/instructions.md","docs/product-contracts.md","docs/runtime-protocol.md"]
dependencies: ["TASK-006"]
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Skills and Project Steering

Compose explicitly selected managed skills and bounded project steering and preserve the execution snapshot.

## Behavior and invariants

- Revisioned managed Markdown and explicitly opted-in workspace steering compose with the agent prompt and task messages in documented precedence. Node selections can narrow managed skills; a graph without opt-in retains existing prompt behavior.

## Acceptance criteria

### AC-001 — Reviewed instruction composition

Revisioned managed Markdown and explicitly opted-in workspace steering compose with the agent prompt and task messages in documented precedence. Node selections can narrow managed skills; a graph without opt-in retains existing prompt behavior.

### AC-002 — Scoped loading and immutable audit

Workspace reads stay in the selected workspace, exclude unsupported entries and cannot grant capabilities. The execution snapshot preserves applied revisions/hashes and omission reasons after configuration changes.

## Explicit exclusions

- Repository-wide discovery, AGENTS.md and progressive skill loading belong to REPO-GUIDANCE. Skill scripts and grant changes are outside current steering.

## Open decisions

- REPO-GUIDANCE requires a maintainer decision on precedence and source policy before expanding this baseline.

## History

- 2026-10-18. The plan records implemented managed Markdown, selected project steering and immutable manifests. A concurrent two-graph acceptance test checked isolated sources and file access.

## Reconciliation

- [instructions.md](../../../docs/instructions.md), [product-contracts.md](../../../docs/product-contracts.md), [runtime-protocol.md](../../../docs/runtime-protocol.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
