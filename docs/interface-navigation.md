# Console navigation

The console groups daily work and configuration for the same user. This does not
introduce roles or change API permissions.

| Group | Destination | Scope |
| --- | --- | --- |
| Work | Tasks (`#/tasks`) | Persisted request drafts and requirements preview; execution admission is pending. Existing Runs remains agent execution history. |
| Work | Projects | Disabled until project registration is implemented. |
| Work | Approvals | Existing approval and application-review flows, including pending count |
| Configuration | Workflows (`#/graphs`) | Existing graph editor, activity, files, messages and advanced YAML |
| Configuration | Connections (`#/connections`) | Installation MCP connections, OAuth setup, discovered tools and call history |
| Configuration | Models (`#/models`) | Model providers, credentials and explicit connection/inference tests |
| Configuration | Skills & instructions (`#/skills`) | Existing installation instruction management |
| Configuration | Administration (`#/administration/health`) | Installation health and policy sections |

Configuration can be collapsed with its button (Enter or Space with keyboard
focus). Navigation to another destination reveals the group; reload/direct links
start expanded so the active destination is visible. Collapse is session UI state,
not installation configuration. On narrow screens navigation wraps and scrolls
within a bounded area. Existing graph URLs and editor drafts retain their behavior.
The graph list still uses its existing Graphs heading and graph actions.

Administration has independent hash routes for `health`, `network`, `resources`,
`retention` and `tools`. Opening a section loads only that section's resources.
Health checks do not test model inference; use Models for explicit provider tests.

Existing `#/integrations`, `#/integrations/connect?...` and
`#/integrations/oauth?...` links still open the MCP destination. Existing OAuth
callback URLs and query validation are unchanged. New navigation uses Connections.
Provider management previously located under Integrations is now under Models.

Roles and API authorization remain unchanged. Viewers inspect; operators can perform
existing permitted tests; administrative edits remain admin-only. An OAuth grant
never assigns tools to agents. Credentials remain managed by the existing server APIs.

## Connection pages

`#/connections` lists installation connections with name/service/ID search and a
status filter. Administrators start creation with **Add connection** at
`#/connections/new`; template links use `#/connections/connect?...`.
Saving an enabled connection automatically discovers tools when authentication is
available. OAuth connections awaiting consent require authorization first, then
perform discovery. Disabled connections remain inactive. Saving and discovery have
separate progress and error states: a discovery failure preserves the saved
connection, with Test / Refresh tools available for retry. No agent is created and
no tool grants are added. API clients still use the separate save and refresh
endpoints; this automatic sequence belongs to the console save flow.

Saving opens `#/connections/c/<id>/overview`. The detail sections are:

- **Overview**: discovery state, OAuth authorization/revocation and saved agent grants.
- **Tools**: explicit discovery/refresh, searchable tool names, descriptions and schemas.
- **Usage & history**: existing redacted audit events and filters, always scoped to this
  connection, including after clearing filters or moving to older pages.
- **Settings**: configuration/quota editing and the existing safe deletion workflow.

The list also links to `#/connections/history` for cross-connection history, including
removed connections. History loads on explicit request. Quotas shown are configured
limits, not inferred consumption counters. Graph grants shown on Overview come from
saved graph definitions (including defaults when a node has no override); running
versions can differ and each call still undergoes gateway authorization.

Legacy `#/integrations` setup and OAuth return pages retain the existing combined
setup flow for compatibility. New global navigation uses the list/detail pages.
Editing preserves revision checks, blank-credential semantics, role restrictions,
DCR uncertainty handling and explicit OAuth consent. Missing connections and API
failures are shown explicitly; navigation never registers or authorizes a client.

Agent-access links explicitly open configuration in the named graph. A connection
context banner provides a return link, retained across graph tabs and run details.
Connection readiness checks do not execute tools. Agent execution is a separate,
optional end-to-end check using explicit grants and the selected model.
Developer usability validation and installation activation remain separate from
automated test coverage.

## Agent configuration

Open **Agent configuration** in a graph, or **Configure agent** in its canvas
inspector. Select an agent and use General, Instructions, Tools & permissions or
Advanced. Deep links have the form
`#/g/<graph>/agent?agent=<node>&section=<general|instructions|tools|advanced>`.
Reload and browser history preserve the selected agent/section; unsaved drafts
remain in memory and are not persisted across a page reload.

General contains identity, runtime and model fields. Instructions edits the prompt
directly in the graph draft. Tools & permissions reuses the existing grant pickers,
approval gates and isolation fields. Advanced contains loop, replica/workspace and
turn-limit settings. The boundary summary distinguishes default grants from agent
overrides and shows read-only workspace access and configured isolation. It is a
draft summary, not proof of currently effective runtime access: installation policy,
constraints and gateway checks still apply.

Section and agent changes share the existing whole-graph draft. **Review & save**
lists all changed document paths, including other agents, channels, graph settings
and extension fields, before **Save graph draft** runs server validation and the
existing version check. Values are not copied into the review; inspect their editor
sections or Definition. Validation/dependency failures and revision conflicts keep
the draft available for correction or the existing explicit conflict workflow.

Viewers inspect a read-only page; operators and administrators can edit under the
existing backend authorization. Opening a section never grants tools or starts an
agent. Execution controls remain on the graph's operational views. The existing
canvas inspector and Definition editor remain available for compatibility, including
fields not represented by dedicated form controls. No schema or runtime permission
contract changes are introduced by this organization.

## Runs

Open **Runs** in a graph to search/filter its latest 100 execution records.
`#/g/<graph>/runs?execution=<id>&section=<result|timeline|tools|messages|diagnostics>`
opens a dedicated detail, even when that execution is outside the list window.
The detail verifies the returned execution belongs to the selected graph before
showing its contents or loading associated records. A missing execution and a
failed request are explicit error states with retry controls.

- **Result** shows the persisted summary/error, execution outcome, recent blocked
  tool calls, linked approvals and a link to the current graph workspace.
- **Timeline** and **Tool calls** show exact execution-ID matches within the latest
  300 events for that agent, ordered by sequence. Tool denial does not overwrite
  the persisted execution status. This window is not a complete execution trace.
- **Messages** shows explicit execution/input/output ID matches within the latest
  100 graph messages; a shared agent or timestamp never establishes attribution.
- **Diagnostics** shows recorded identifiers, version, replica, iteration and trace
  context. Its agent link opens current configuration, which can differ from the
  execution's graph version.

Loaded sections refresh every five seconds and offer manual retry. Failed requests
are not presented as fresh data or successful empty results. Approval controls reuse
the existing role gates (environment decisions remain admin-only); opening a run
never starts an agent or grants permissions. Graph execution controls remain in the
operational views. Workspace links inspect current files, not historical snapshots
attributed to a run. Activity remains available and links to the new detail.

These pages use existing authorized APIs. Full historical pagination, inferred task
progress, replay/retry controls and execution-specific workspace snapshots are not
introduced by this interface change.


## Graph selector and agent list

The header's **Select graph** control searches graph names and IDs, shows saved
runtime state and marks unsaved graph drafts. Switching to a visited graph restores
its last destination. The canvas has a searchable agent list: select an agent to
inspect its panel or use **Configure** to open its dedicated configuration page.
Names reflect the local draft; runtime state belongs to the saved graph and is
explicitly unavailable when status loading fails. On narrow screens the list moves
above the canvas.

Visited graph editors remain in memory for the current page session. Switching
graphs or installation destinations preserves each graph's draft, selection and
editor state; inactive editors pause their effects. Nothing is automatically saved
or granted. Draft saves retain their original base revision, so an external update
still requires conflict resolution. Missing graphs and failed reloads retain the
local editor and display an error instead of silently discarding edits.

Drafts are not persisted in browser storage and do not survive closing or reloading
the page. A browser leave warning is registered while graph drafts are dirty.
Existing role gates, server validation and optimistic concurrency remain in force.


## Shared presentation

Console pages share a heading scale, section spacing, bounded form widths and
responsive cards. Primary actions use the accent button; utility actions remain
neutral and destructive actions retain their danger treatment. Skills uses the
same button component without changing its disabled fieldsets or permissions.
Keyboard focus is visible on links, buttons, inputs and disclosure summaries.
Reduced-motion preferences disable decorative transitions.

Optional provider-test guidance, instruction precedence and canvas gestures use
native expandable help next to the relevant controls. Opening help does not call
an API or change a draft. Security notices, validation failures and permission
boundaries remain visible. Browser coverage checks keyboard disclosure, narrow
layouts and viewer restrictions; this is not a full accessibility certification
or a claim of developer usability acceptance.


## Usability follow-ups

Graph cards open from their main surface or focused title button (Enter/Space).
Action buttons and agent configuration links remain independent. Cards list agents
with explicit Configure links, preserving existing graph drafts when revisited.
Events wrap long event types and actors, switching to a stacked layout on narrow
screens to avoid column overlap.

Run results identify the summary as agent-generated and distinguish recorded tool
attempts from invocation and lifecycle events observed in the bounded history.
Native runtime calls through the gateway are counted once in new records, including
failed attempts. Historical stored totals are preserved and may include the earlier
duplicate count; the count disclosure explains this limitation. Missing events are
not inferred to be zero attempts, and execution completion does not imply that
every tool succeeded.

## First-use navigation

The global Getting started panel has been removed following layout feedback.
Use the main navigation directly: Graphs, Connections, Models, Administration,
Approvals, and Skills & steering. Contextual help remains available in each screen.
Existing browser-local guide progress is ignored; no stored application data,
graphs, drafts, connections, permissions, or execution history is deleted.
