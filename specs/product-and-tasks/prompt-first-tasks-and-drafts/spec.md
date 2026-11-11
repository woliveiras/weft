---
id: TASK-005
title: "Prompt-first Tasks and Drafts"
summary: "Save and interpret a task request, review suggestions and retain the draft through corrections."
status: implemented
scope: ["prompt first tasks and drafts"]
navigation: ["specs/README.md","specs/product-and-tasks/prompt-first-tasks-and-drafts/matrix.md"]
documentation: ["docs/task-workflows.md","docs/product-contracts.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Prompt-first Tasks and Drafts

Save and interpret a task request, review suggestions and retain the draft through corrections.

## Behavior and invariants

- A saved request, explicit title and reviewed workflow selection survive reload. A suggested title is bounded and preserves explicit edits. Viewers cannot create or change drafts.

## Acceptance criteria

### AC-001 — Durable editable request

A saved request, explicit title and reviewed workflow selection survive reload. A suggested title is bounded and preserves explicit edits. Viewers cannot create or change drafts.

### AC-002 — Reviewable interpretation

Interpreted settings and generated payload suggestions are reviewable and preserve explicit edits. Missing requirements and dependency failures retain the draft and expose a repair path.

### AC-003 — Task journey and first example

A user sees current stage and next action, durable outcomes and decision history after reload. The synthetic example requires an explicit save and ordinary reviewed admission.

## Explicit exclusions

- Consent rules and execution admission belong to TASK-006 and TASK-007. Repository imports belong to ENV-001.

## Open decisions

- The task workflow inventory retains historical planned language for prompt-first UX. The delivered scope is described by the later PROMPT-01/02/05/06/07 checkpoints; final installed usability remains open under PROD-04.

## History

- 2026-10-19. FLOW-03a established persisted drafts. Phase A added structured suggestions and a synthetic first task. PROMPT-01/02/05/06/07 introduced the prompt-first journey and qualified it with fixtures.

## Reconciliation

- [task-workflows.md](../../../docs/task-workflows.md), [product-contracts.md](../../../docs/product-contracts.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
