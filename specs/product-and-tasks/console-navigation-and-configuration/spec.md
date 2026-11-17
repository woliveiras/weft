---
id: TASK-002
title: "Console Navigation and Configuration"
summary: "Organize daily work and configuration, preserving route compatibility and contextual guidance."
status: implemented
scope: ["console navigation and configuration"]
navigation: ["specs/README.md","specs/product-and-tasks/console-navigation-and-configuration/matrix.md"]
documentation: ["docs/interface-navigation.md","docs/getting-started.md","docs/brand/brand-guidelines.md"]
dependencies: []
authority:
  granted: [historical documentation]
  withheld: [implementation, deployment, production qualification]
---

# Console Navigation and Configuration

Organize daily work and configuration, preserving route compatibility and contextual guidance.

## Behavior and invariants

- Work and Configuration keep stable destinations, supported deep links and retained drafts. Viewer navigation remains read-only.

## Acceptance criteria

### AC-001 — Destinations and legacy links

Work and Configuration keep stable destinations, supported deep links and retained drafts. Viewer navigation remains read-only.

### AC-002 — Discover and edit agents

Graphs expose searchable graph and agent selection. Agent settings separate instructions, tools and isolation, show effective access and review graph changes before saving.

### AC-003 — Contextual first use

Fresh and returning browsers use direct routes and contextual help. Historical browser-local guide state does not mount the removed global panel or discard saved drafts.

### AC-004 — Responsive presentation

Common forms and controls remain usable at narrow widths and in the supported themes. Long event labels remain readable alongside actors.

## Explicit exclusions

- The global resumable onboarding panel was withdrawn. Human walkthrough completion is governed by PROD-04.

## Open decisions

- The 2026-10-17 graph-screen feedback needs confirmation in the deferred unaided walkthrough. Preserve the observed screenshot and the reported feedback as separate evidence.

## History

- 2026-10-17. Navigation and visual consolidation were activated. Developer feedback led to UX-01–05 on agent discovery, event layout, tool-call counts and MCP setup.
- 2026-10-18. The maintainer withdrew global resumable onboarding following reported layout breakage. Contextual help and direct navigation became the first-use path. Browser-local progress was left in place and ignored.
- 2026-10-19. Phase A grouped daily work and configuration and retained legacy deep links.

## Reconciliation

- [interface-navigation.md](../../../docs/interface-navigation.md), [getting-started.md](../../../docs/getting-started.md), [brand/brand-guidelines.md](../../../docs/brand/brand-guidelines.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
