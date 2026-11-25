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

The request editor exposes workflow, pinned version, repository selection and execution environment without opening a disclosure. Related fields are grouped with consistent spacing; execution actions follow configuration. Optional repository import is available beside repository selection. Project choices distinguish registered repositories with identical names. Loading and failed project discovery are explicit and preserve existing selections. Manual correction does not require model interpretation or grant consent. The workflow version is a dropdown from entry; an unselected version defaults to the current saved version, while existing positive pins are retained across workflow publication. Continuing an existing draft preserves its manual bindings: complete changes are saved before review, incomplete changes stay editable with field guidance. Save or review failure retains edits and the latest successfully saved revision; repeating interpretation requires the explicit Resolve request action.

### AC-003 — Task journey and first example

A user sees current stage and next action, durable outcomes and decision history after reload. When the server permits environment preparation, the task actions explain the local preparation, approval and separate start steps and offer a focused path to the build review. The build definition, consequences, confirmation and preparation action stay together; pending preparation is announced. Known graph-oriented environment-not-ready notices remain in technical diagnostics instead of directing the user away from the task. Other blockers remain visible and all backend admission decisions remain authoritative. The Tasks list opens saved tasks and offers the standard New task flow to authorized users. Previously saved examples remain available through the ordinary task journey and require reviewed admission before starting.

The Tasks table shows the recorded task ID, status, workflow version, issue, and dates where available. Search by title, ID or issue and filters by status and workflow apply to the current page. The displayed match count and pagination identify the scope of the results.

### AC-004 — Delete an unstarted draft

Operators and administrators can delete a saved draft after explicit confirmation naming that draft and supplying its current revision. Any preparation already initiated prevents deletion, even if no agent executed. Only a task still in `draft` with no execution copy or environment preparation can be deleted. Other lifecycle states remain retained, including completed, cancelled, failed and partially prepared tasks. Deletion must race safely with edits, interpretation and execution admission: one current-revision mutation wins; a deleted ID cannot be read, listed, resumed or recreated by stale requests. No repository, import, workspace, execution or approval is deleted. A minimal deletion marker and audit intent may be retained to prevent replay. Cancellation, refused eligibility checks and failures before the deletion write preserve the draft and explain recovery. If a response is lost after dispatch, the interface reports an uncertain outcome and directs the user to Tasks or a refresh instead of claiming the draft was retained. Viewers cannot delete drafts.

## Explicit exclusions

- Consent rules and execution admission belong to TASK-006 and TASK-007. Repository imports belong to ENV-001.

## Open decisions

- The task workflow inventory retains historical planned language for prompt-first UX. The delivered scope is described by the later PROMPT-01/02/05/06/07 checkpoints; final installed usability remains open under PROD-04.

## History

- 2026-11-24. The maintainer requested visible task configuration and repository selection, consistent form spacing, and deletion limited to unstarted drafts.

- 2026-10-19. FLOW-03a established persisted drafts. Phase A added structured suggestions and a synthetic first task. PROMPT-01/02/05/06/07 introduced the prompt-first journey and qualified it with fixtures.
- 2026-11-18. The maintainer retired the synthetic task shortcut from Tasks and kept the ordinary task journey for any saved examples.

## Reconciliation

- [task-workflows.md](../../../docs/task-workflows.md), [product-contracts.md](../../../docs/product-contracts.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
