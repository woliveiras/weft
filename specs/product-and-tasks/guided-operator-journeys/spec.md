---
id: TASK-009
title: Guided Operator Journeys
summary: Reduce repeated task, workflow and agent configuration while retaining explicit review and authority.
status: implemented
scope: [task entry, workflow task defaults, guided workflow creation, shared agent editing, contextual setup]
risk: high
risk_domains: [consent, configuration persistence, unpublished changes]
reversibility: additive optional configuration and reversible console changes
change_surfaces: [console, graph format, graph validation, documentation]
contracts: [Graph.spec.task]
review_policy: specification, tests and independent implementation review
dependencies: [TASK-002, TASK-003, TASK-005, TASK-006, TASK-007, TASK-008, RELEASE-2]
documentation: [docs/features/README.md]
authority:
  granted: [maintainer approval on 2026-11-25 to specify and implement the UX audit in committed stages]
  withheld: [production deployment, remote push, implicit consent, credential changes]
---

# Guided Operator Journeys

An operator should describe work and review concrete execution choices without
reassembling the installation's architecture on every task. An administrator
configures reusable behavior once, then operators reuse versioned suggestions.
This refactoring follows the 2026-11-25 code/documentation UX audit and the
maintainer's repeated reports of unclear actions and lost manual corrections.
Those reports are supplied evidence; no unaided usability study or timing
baseline is claimed.

## Invariants and compatibility

- TASK-006 owns consent and TASK-007 owns execution admission. Suggestions,
  navigation, validation, saving, publishing and inspection never imply consent.
- Positive task/workflow agent version pins remain explicit snapshots. Publishing
  a shared agent does not update consumers. Existing graphs without `spec.task`
  retain inference from unambiguous topology and remain round-trippable.
- Roles, CAS, gateway grants, budgets, required approvals, isolation and project
  ownership remain authoritative. No secret is stored in navigation context.
- Existing task-submit API semantics remain compatible. The console coordinates
  saving and prerequisite inspection behind Run task; preparation and execution
  remain separate explicit actions against a reviewed saved revision.
- No automatic migration of existing inline agents or historical executions.
  Origin snapshots remain reusable and distinguishable from linked definitions.
- Contextual setup is optional and belongs to the current task. The withdrawn
  global onboarding panel is not reinstated.

## Acceptance criteria

### AC-001 — Context follows task entry

Create task from Projects carries that project's ID. New task from a workflow
carries its ID. The new editor resolves only loaded, available context, pins the
current saved workflow version, and suggests compatible defaults. Context never
overwrites a saved task or explicit subsequent choices. Unknown context exposes
an actionable unavailable state instead of silently selecting another resource.
A visible summary names workflow/version, project or workflow repository/base,
effective isolation/environment and delivery. Missing configuration stays visible.

### AC-002 — A clear task launch

Run task is the single primary action for new tasks, saved drafts and retry
drafts. It saves outstanding edits and inspects that exact acknowledged revision
without calling the model. The operator need not separately save and refresh
requirements. Save draft remains a non-executing alternative. Optional Resolve
request remains explicit and explains its binding changes.

An execution requires a preceding visible review of the saved workflow version,
input, repository/base, environment, effective grants, access mode and budget.
Run task may start directly only when those displayed facts still match the
fresh server review. Otherwise it presents a compact inline launch review and
requires another explicit Run task click. Saving, navigation and background
inspection alone never authorize execution. New tasks retain Ask; Workspace or
Full access requires existing explicit scope-bound consent. A retry does not
inherit consent from its source. Mandatory approvals and server admission apply.

Missing configuration opens its editable section and receives field guidance.
Save, inspection or start failure retains inputs and the last acknowledged
revision. Repeated clicks cannot dispatch concurrent launch requests. Loading,
unchanged success, blockers and failures are announced. Environment preparation
shows its reviewed definition before a separate explicit build action. Build
completion refreshes prerequisites without starting agents; the ready environment
offers Run task without a separate inspection button or redundant consent form.

### AC-003 — Reusable task defaults

A graph may declare `spec.task` with optional `nodeId`, `resultNodeId`,
`channelId`, `messageType`, `projectId` and `scmConnection`. These are suggestions
for new task configuration, not execution instructions or grants. Authored local
bindings must identify enabled nodes. A selected channel must be an unconditional
queue ingress with exactly one receiver matching the selected entry; its message
type is required and must match allowed channel types. A message type without a
channel is invalid. IDs/types are bounded to 128 bytes. Project/SCM references are
names only; task admission validates external resources and their permissions.
No default includes consent, secrets, a moving version alias or arbitrary payload.

The workflow settings editor exposes and explains these suggestions. Explicit
defaults win over topology inference, which remains the legacy fallback. Selecting
a workflow deliberately replaces its routing suggestions; late loading only fills
missing initial choices. Explicit task/project entry context wins over workflow
project preferences. Saving a task captures a concrete version and selected base.

### AC-004 — Readiness and contextual setup

The task explains repository, workflow/input, model, environment, delivery, access
and budget readiness together, distinguishing configured facts, review-required,
loading, unavailable and blocked states. Goals (run repository work, deliver a PR,
work from an issue, review a PR) explain their prerequisites near the task, with
links to the relevant existing configuration. Required configuration is not hidden
behind optional help. Simple corrections stay in the editor; advanced routing may
collapse only while complete, with a readable summary and an Edit action.

The ordinary editor keeps request, workflow and repository visible. Optional
identity, environment overrides, routing, payload, issue source and delivery
settings use an Advanced settings disclosure with a factual summary. Required
missing or invalid advanced settings open automatically. Technical environment
definitions and raw grants remain available for inspection rather than filling
the ordinary launch path with duplicate forms.

Configuration repair links retain the saved task destination and offer return
navigation. Unsaved edits must be saved before leaving for repair, or remain in
the current page; a failed save prevents navigation. Returning never authorizes or
starts work. Operators lacking administration see who must repair a dependency.
SCM discovery distinguishes loading, real empty lists, saved unavailable selections,
failures and retry; discovery failure must not become an empty successful list.

### AC-005 — Guided workflow creation

New workflow offers a visual path with identity, task/event/continuous purpose,
catalogue or new agents, reviewable proposed connections and a final review before
saving. Task-purpose instances use once/event run modes; incompatible instance
modes are explained and block task-purpose creation until corrected. New agent creation
does not silently grant tools. The review identifies agent versions, channels and
execution modes; users can correct/remove proposals. Invalid references, duplicate
IDs, absent required agents and validation/API failures retain the draft. Save
creates a definition and opens configuration; it does not run it. Advanced YAML/
JSON remains available without silently losing the visual draft.

### AC-006 — Definition, instance and version adoption

Agent configuration distinguishes a shared published definition, a pinned workflow
use and an origin snapshot. Shared publication explains that consumers retain their
versions. Workflow-local parameters/instructions explain their limited scope.
Selecting another linked version first presents proposed differences and requires
an explicit adoption/cancel action; cancelling leaves the current binding intact.
Existing inline origins are not presented as synchronously linked definitions.

### AC-007 — Protect unpublished work and explain association saves

Changing a shared agent version or navigating away with unpublished edits warns
and offers staying or explicitly discarding. Browser unload uses the native guard.
Changing detail tabs preserves edits. Failed publication preserves the draft.
Workflow skill-association changes are reviewed with workflow identity, expected
version and affected sections before an explicit save creates a workflow version.
Cancel and failed CAS retain edits; no permission or runtime action is implied.

### AC-008 — Complete, accessible journeys

The changed flows provide keyboard labels/focus, responsive field/action layouts,
textual states beyond color, bounded lists and useful empty/error recovery. Tests
cover ready reuse, ambiguous topology, unavailable resources, stale pins, roles,
failed saves, cancellation and repeated actions. Feature documentation describes
the resulting end-to-end paths and source/test anchors, not just screen layouts.

## Delivery plan and commits

1. Commit this specification and its independent behavior/oracle matrix.
2. Add optional validated workflow task defaults and their editor; unit and API/
   serialization integration tests. Commit the contract and consuming helper.
3. Preserve task entry context, stabilize save/review/start actions, add summary,
   readiness/setup and recoverable configuration handoffs; unit and UI/boundary
   integration tests. Commit the task journey.
4. Deliver guided workflow creation and advanced-document compatibility; builder
   unit and creation integration tests. Commit the workflow journey.
5. Clarify agent scope, review version adoption, guard unpublished edits and review
   skill-association saves; unit and interaction integration tests. Commit it.
6. Run cross-flow regression/build/lint, independent review, reconcile feature docs
   and record evidence. Commit any coherent review fixes separately, then the final
   evidence/documentation. Rebuild the authorized local stack for visualization.

Independent work may proceed concurrently after specification review; each stage
is committed separately after its checks and diff review. This plan defines no
scheduled execution, real provider call or external write.

## Explicit exclusions

Server-side agent draft storage, forced catalogue migration, subgraph reuse,
automatic configuration/credentials, production rollout and quantitative usability
claims are excluded. Draft guard is browser-session protection, not durable backup.

## Maintainer decision — 2026-11-25

The maintainer approved simplifying task starts generally after reporting that
save/review/start controls were confusing for both newly configured and retry
tasks. This refinement replaces AC-002's separate console inspection buttons
with coordinated Run task and visible scope review; TASK-006 and TASK-007's
authorization and admission contracts remain unchanged. Local visualization and
commits are authorized; running the operator's real dependency-security request
is outside this UI implementation.

## Verification and evidence

See [matrix.md](./matrix.md) and [evidence.md](./evidence.md). Existing acceptance
owners above remain authoritative; this spec owns only the new journey behavior.
