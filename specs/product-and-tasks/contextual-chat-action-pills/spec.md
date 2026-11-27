---
id: TASK-013
title: Contextual Chat Action Pills
summary: Compose a first chat request by choosing an action and an authorized target before sending.
status: planned
scope: [new chat, guided composition, resource selection, manual configuration handoff]
risk: medium
risk_domains: [target selection, authorization, unintended submission, draft loss]
reversibility: Additive composer controls using the existing chat and manual entry points.
change_surfaces: [web/src, chat request handling, web/e2e, docs/features/chat]
contracts: [draft context, explicit send, role-aware choices]
review_policy: Review unit and integration results against the behavior matrix before claiming delivery.
navigation: [specs/README.md, specs/product-and-tasks/contextual-chat-action-pills/matrix.md, docs/adr/0013-contextual-chat-action-pills.md]
documentation: [docs/features/chat/README.md]
dependencies: [TASK-002, TASK-012]
authority:
  granted: [decision record, specification, local documentation commits]
  withheld: [implementation, deployment, remote push]
---

# Contextual Chat Action Pills

An operator can compose a request from recognizable choices, add details in
their own words and decide when to send it. [ADR 0013](../../../docs/adr/0013-contextual-chat-action-pills.md)
records the decision. This specification owns the interaction **before the
first message**. [TASK-012](../conversational-console/spec.md) owns session
creation, authorized discovery, accepted messages, operation review and
execution. [TASK-002](../console-navigation-and-configuration/spec.md) owns
manual destinations.

## Acceptance criteria

### AC-001 — Enter through a category or type directly

In a ready new chat, present **Tasks**, **Agents**, **Workflows**, then
**Mais** near the composer. Use the visible word **Mais** alone. Opening it
reveals eligible secondary categories in this order: **Projects**,
**Connections**, **Models**, **Skills & instructions**. Closing it returns
to the primary choices. Administration is accessible through the existing
manual navigation and has no entry in the pill hierarchy.

The free-text field and slash completion are available from the first step.
An operator can leave the guide at any level and send their own request.
An incomplete branch contributes no hidden context to that request.

### AC-002 — Reveal an available action in context

Selecting a category opens a set of verb-labelled actions. The following
paths define the initial coverage. Use existing domain capabilities and show
only actions available for the current role and installation.

| Category | Initial choices for an eligible user |
| --- | --- |
| Tasks | Follow a task, find a task, or see running tasks |
| Agents | Find a catalog agent or prepare a request to create one |
| Workflows | Find a workflow or see active workflows |
| Projects | Open project registration or select an existing project to configure |
| Connections | Inspect configured connections or open connection settings |
| Models | View configured models or open model settings |
| Skills & instructions | Find a skill or open the Skills & instructions editor |

Label a manual destination with an explicit verb such as **Open connection
settings**. Selecting that link navigates to the named screen. Returning
restores the originating unsent text, choices and focus. Credential entry
belongs to the existing settings or consent surface. A viewer who may inspect
models sees the inspection action. A settings action requiring administrator
rights is offered only to a user who currently has those rights.

### AC-003 — Choose the intended resource

For **Tasks → Follow a task**, provide a searchable list showing authorized
task titles and states. Distinguish same-name results by scope or visible
identifier. Selecting a result binds its stable ID and displays a
recognizable target beside the action in the composer. A category or search
term alone cannot select a resource. Apply the same selection rule to other
actions that need an existing target.

Show distinct outcomes for zero matches, partial results and a failed lookup.
Offer a way to refine the search or retry. Preserve the text and current
branch on failure. If a selected resource disappears or access changes
before dispatch, the current authorization check governs the outcome. Let
the operator replace or clear the invalid choice without substituting a
different resource or disclosing inaccessible records.

### AC-004 — Edit locally and send once

Back, change and clear operate on the guided selection while preserving
user-authored text. Changing a parent clears choices that depend on it.
Browsing and selecting may read authorized inventory. They create no
session, transcript entry, model call, watch or domain change. Unsent
choices have the same tab, draft and owner boundaries as unsent text.

A complete intent such as **Follow a task** plus the chosen task can be sent
without additional prose. **Send**, Ctrl+Enter and Command+Enter submit the
same visible intent, exact target when applicable, and optional user text.
One accepted submission produces one user turn. Retrying uncertain delivery
uses the existing request key. A submitted change request follows TASK-012
AC-004 review and authorization. Selecting a pill or sending a request
cannot confirm the resulting proposal.

### AC-005 — Operate the guide with keyboard, touch or assistive technology

Use named buttons for choices and links for navigation. Expose whether
**Mais** is expanded. Support keyboard activation, Escape to close the
disclosure and focus return to its trigger. When drilling into a category,
make the next choices and current selection discoverable to a screen reader.
Back restores focus to the choice that opened the branch. At narrow widths
the choices wrap, with all labels and the composer reachable by touch and
keyboard. Text entry retains the existing slash and send shortcuts.

## Scope and verification

The first delivery slice is **Tasks → Follow a task → select task → Send**.
The second adds the other categories, manual handoffs and role-specific
availability. Exercise keyboard access and failure recovery in each slice.
A model outage uses the existing TASK-012 readiness and recovery paths.
Archived chats retain their read-only behavior.

Platform onboarding, including a first-repository setup walkthrough, is
outside this work. Administration through the pills and suggestions after a
reply are also outside scope. Existing independently typed operations remain
governed by TASK-012, including its conversational parity criteria.

The [behavior and oracle matrix](matrix.md) defines planned unit and real
boundary integration coverage. For formative usability verification, ask
someone unfamiliar with prompting to follow a named task, change their
selection and say when monitoring starts. Record unaided completion, the
selected target and the participant's prediction before Send. Usability
and implementation evidence are pending.
