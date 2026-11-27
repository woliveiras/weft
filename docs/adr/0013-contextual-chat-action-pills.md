# 0013 — Compose a chat request with contextual action pills

**Status:** Proposed

## Context

New chat gives examples in prose. An operator who wants to follow a task must
still formulate the request or know a slash command. The console already has
recognizable resource names, authorized lookups and a review step for changes.
We want someone to be able to identify the intended action and its target
before sending the first message. Whether this helps people unfamiliar with
prompting is a usability hypothesis.

[TASK-013](../../specs/product-and-tasks/contextual-chat-action-pills/spec.md)
defines the planned interaction and its acceptance criteria.

## Decision

Place optional **Tasks**, **Agents**, **Workflows** and **Mais** pills alongside
the new-chat composer. **Mais** is a text-only disclosure. It reveals
**Projects**, **Connections**, **Models** and **Skills & instructions** for
configuration and inspection. Administration is reached through manual
navigation. This decision governs what the pills offer. The existing
[conversational console contract](../../specs/product-and-tasks/conversational-console/spec.md)
governs independently typed requests.

Choosing a category reveals actions with concrete verbs. When an action
requires a target, the operator can search authorized resources and choose
one. Show the resulting action and target at the composer, where the operator
can change or remove the choice and add their own words. Browsing can read
inventory. Only **Send** or its existing keyboard equivalent submits a turn.
Domain changes still require their existing review and authorization.

Offer choices the current role can use. For example, viewing configured
models and configuring a model have distinct eligibility. If a step belongs
in a manual editor, label the destination explicitly and preserve the draft
on return. Platform onboarding has a separate scope.

## Consequences

- Following a task can start with a category, a verb and a recognizable task
  title. The request sent to Chat identifies the exact task even if names repeat.
- Intermediate clicks create neither transcript turns nor model calls. People
  who already know what to ask can type directly or use a slash command.
- Secondary configuration takes an additional click. The console must keep
  offered actions aligned with real capabilities and guide recovery when a
  selected resource or permission changes.

## Alternatives

- **Show every category in the first row.** Configuration competes with daily
  work for space, particularly on narrow screens.
- **Use an ellipsis for secondary categories.** Its meaning overlaps with
  conversation-level controls. The visible word **Mais** names the choice.
- **Send a starter prompt on click.** This creates a conversation turn before
  the operator has selected a target or added details. The chosen approach
  preserves the opportunity to explore and correct the request.
