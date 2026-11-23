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

Complex settings expose field help beside the label. Help explains the effect,
dependencies or value format. Opening it leaves the field values unchanged.
The popup supports keyboard access, closing with Escape and returning focus
to the help control, including when the field is read-only.

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

- 2026-11-19. Maintainer refinement: Projects belongs to Configuration. Configuration remains visible with its divider and fixed heading; it has no collapse control. The approval inbox uses a newest-request-first table, combined filters and 20-row pages. Details and existing decision actions open on demand without changing authorization or approval contracts.

- 2026-11-19. Maintainer refinement to workflow discovery and responsive presentation: the Workflows catalogue uses a newest-updated-first table with combined search/status filters, 20-row pages and expandable workflow-local agent links. Lifecycle actions, administrator deletion confirmation and deferred cleanup remain intact; loading, failure and empty states are explicit.

- 2026-11-19. Maintainer refinement to canvas editing: separate draft and execution toolbar groups; named creation before full agent configuration; a sectioned inspector opens on Overview and defers activity requests until selected. Draft-only nodes show no runtime history or enabled controls. Contextual Remove preserves affected-element confirmation, and the inspector fits narrow viewports.

- 2026-11-19. Maintainer refinement: the primary navigation toggles between labelled destinations and an icon rail to leave more canvas space. The browser remembers the preference; accessible labels, approval counts, keyboard/pointer name hints, active destinations and drafts remain available in both presentations. Browser-storage failure does not prevent session-local toggling.

- 2026-11-19. Maintainer refinement to Models: providers, inference plugins, usage, budgets and prices have separate destinations. Provider creation/editing use full pages with grouped fields instead of configuration modals. Provider search/filtering, a highest-cost-first usage breakdown with search and 20-row pages, explicit dependency failures and aligned responsive forms preserve existing API/credential/budget semantics.

- 2026-11-19. Maintainer refinement to budget comprehension: current-month cards expose the UTC period, separate limit/spent/remaining facts and an inline Edit budget form. Monthly edits preserve recorded spend, existing manual-raise floors and administrator-only writes. Confirmation identifies the saved old/new recurring limit. Default per-task ceilings and inherited warning levels use a separate form/save; failure retains edits and cancellation restores focus without writing.

- 2026-11-19. Maintainer refinement to Skills & instructions: a newest-updated-first catalogue with combined search/status/origin filters and 20-row pages replaces the combined form. Full-page details separate content, workflow usage and revision history. Draft content persists across detail tabs, Save creates a revision and deletion confirms history retention. Association settings expand agents on demand; inspection distinguishes saved configuration from immutable execution snapshots. Explicit loading/error/retry and read-only built-ins/viewers preserve instruction/grant contracts.

- 2026-11-20. Maintainer Administration refinement: health checks expose refresh state and completion time; network policies group modes and show current/proposed changes with bounded admission-impact tables; shared-resource previews load on entry and preserve explicit cleanup confirmations; blocked-access retention names its precise scope and reductions; tool/profile catalogues use search, filters and 20-row pages. Delegation profiles use dedicated editor routes; grouped limits and review summaries retain inheritance, grants and CAS. Dirty state warns before navigation/reload, failed writes retain drafts and require a new review. No installation policy, default, security boundary or stored-data format changes as part of the UI rollout.

- 2026-11-20. Maintainer responsive-layout refinement: all main destinations use the available width and shared inline padding (32 px desktop, 16 px narrow), including loading, empty and error states. Reading-width limits remain local to explanatory text. Canvas/log workspace layouts remain intact.

- 2026-11-23. Maintainer task-detail refinement: Overview places live agent plans before the result, with agent/replica identity, current step, earlier-attempt disclosure and explicit empty/loading/error states. Stable task-scoped query links separate Overview, Changes & checks, Activity, Request and Settings & access, preserve browser back/forward and leave approvals/environment/budget holds visible across sections. A compact header identifies workflow/version, recorded execution timing, snapshot freshness and recorded spend; plan completion is not a verified task percentage. Full results remain readable on demand, delivery records are distinguished from fresh remote verification, and raw grants are secondary to readable permissions. Evidence inspection, checkpoints and task controls preserve their existing roles, confirmations and ownership. Completed tasks do not show an active Stop action. The existing TASK-007 AC-005 task-owned execution link contract remains applicable after settlement.

## Reconciliation

- [interface-navigation.md](../../../docs/interface-navigation.md), [getting-started.md](../../../docs/getting-started.md), [brand/brand-guidelines.md](../../../docs/brand/brand-guidelines.md).
- [Behavior and oracle matrix](./matrix.md).
- [Historical evidence](./evidence.md).
