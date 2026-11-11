---
id: RELEASE-2
title: Reusable graph components
summary: Save, version, share and reuse agents and subgraphs in the visual editor.
status: deferred
scope: [Release 2, graph components]
review_policy: Maintainer approval after Release 1 is complete and stable
navigation: [wip/TASKS.md, specs/reusable-components/matrix.md]
dependencies: [Release 1 completion and stability, maintainer authorization]
---

# Reusable graph components

Allow users to reuse agents and subgraphs across graphs without manually
copying definitions or losing local configuration.

## Behavior and invariants

Component operations and sharing follow installation permissions. Insertion
and updates preserve existing local settings.

## Acceptance criteria

- **AC-001.** An authorized user saves an existing agent or subgraph and can
  list, read, update, delete and share the resulting versioned component.
- **AC-002.** A user inserts a compatible version in the visual editor, with
  validated parameters and defaults. Invalid parameters or incompatible
  versions surface a validation error without changing the graph.
- **AC-003.** A user can see the version in use, available versions and the
  proposed diff before updating an instance. A successful update preserves
  local configuration.

## Open decisions

- Component format, parameter and compatibility rules, update semantics,
  and how installation permissions apply to shared components.

## Reconciliation

- Governing input: [Release 2 plan](../../wip/PLAN.md#release-2--reusable-components).
- [Behavior and oracle matrix](./matrix.md).
- Review the deferred scope before implementation. Update reuse documentation
  and record fresh unit, integration and browser results during delivery.
