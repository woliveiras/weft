# Console navigation

The console groups daily work and configuration for the same user. This does not
introduce roles or change API permissions.

| Group | Destination | Scope |
| --- | --- | --- |
| Work | Activity (`#/activity`) | What is running, what needs a person and recent executions across all workflows, and per agent/workflow health; see below |
| Work | Tasks (`#/tasks`) | Find saved requests in the task history table. Search by title, ID or issue and filter the current page by status or workflow. Open a row's task link for requirements, execution and progress. See [task workflows](task-workflows.md). |
| Work | Approvals | Existing approval and application-review flows, including pending count |
| Configuration | Projects (`#/projects`) | Registered repositories and task worktrees; see [registered projects](task-workflows.md#registered-project-worktrees-2026-10-19). |
| Configuration | Workflows (`#/graphs`) | Existing graph editor, activity, files, messages and advanced YAML |
| Configuration | Connections (`#/connections`) | Installation MCP connections, OAuth setup, discovered tools and call history |
| Configuration | Models (`#/models`) | Model providers, credentials and explicit connection/inference tests |
| Configuration | Skills & instructions (`#/skills`) | Existing installation instruction management |
| Configuration | Administration (`#/administration/health`) | Installation health and policy sections |

Configuration is always visible, with a fixed heading and a divider after Work.
On narrow screens navigation wraps and scrolls within a bounded area. Existing
graph URLs and editor drafts retain their behavior.
The workflow catalogue uses the Workflows heading and New workflow action. Existing graph URLs and API names remain unchanged.

Main destinations share the available width and the same content edges: 32 px
inline padding on desktop and 16 px on narrow viewports. Text can keep its own
reading-width limit without constraining the page, tables or card grid. Canvas
and log workspaces retain their existing layout.

Administration has independent hash routes for `health`, `network`, `resources`,
`retention`, `tools` and `delegation`. Opening a section loads only that section's
resources. Health checks show loading, last completion time and next steps when a
service is unavailable; they do not test model inference. Use Models for explicit
provider tests.

Policy forms show unsaved changes and require a current/proposed review before
confirmation. A failed save retains edits and requires a new review; revision
conflicts remain enforced by the existing APIs. Navigation and reload ask before
discarding unsaved policy/profile input. Drafts remain in browser memory and are
not recovered after a confirmed reload.

- **Network policies** separates build/setup and agent access. The impact table
  combines host/name, admission and environment-type filters. Preview shows added
  and removed hosts. Existing work retains its network until explicitly stopped.
- **Shared images & caches** loads a read-only preview on entry for admins. Its
  table filters images, caches and retained usage. Selection remains visible
  across pages/filters, is capped at 100 and requires explicit cleanup review;
  the server rechecks eligibility. Deferred cleanup reservations are separate.
- **Blocked access history** configures only blocked-access retention, per
  environment. Lower limits can permanently discard records; increasing limits
  cannot restore discarded records.
- **Tool catalog** keeps descriptions and required capabilities visible, with
  combined filters and 20-row pages. Listing never grants a tool to an agent.
- **Delegation** groups execution, token and size limits. Profiles use a bounded
  searchable table and dedicated pages at
  `#/administration/delegation/profiles/new` and
  `#/administration/delegation/profiles/<id>/edit`. Reload saved profile refreshes
  the revision/comparison baseline while retaining local edits. Profiles inherit
  the parent's model and can only select its existing grants; an explicit
  delegation grant and existing approvals still apply.

Existing `#/integrations`, `#/integrations/connect?...` and
`#/integrations/oauth?...` links still open the MCP destination. Existing OAuth
callback URLs and query validation are unchanged. New navigation uses Connections.
Provider management previously located under Integrations is now under Models.

The top bar has a **Theme** selector (System, Light, Dark). System follows the
operating system's light/dark setting; an explicit choice is stored in this
browser only (`localStorage` key `weftTheme`) and is not installation
configuration. If the browser blocks storage, the choice applies to the current
tab and the selector says so. Colours follow [the brand guidelines](brand/brand-guidelines.md).

Roles and API authorization remain unchanged. Viewers inspect; operators can perform
existing permitted tests; administrative edits remain admin-only. An OAuth grant
never assigns tools to agents. Credentials remain managed by the existing server APIs.

## Activity

`#/activity` answers "what is happening and what needs me?" across every workflow
and task run, from one bounded request (`GET /api/v1/activity`, see
[api.md](api.md#activity-view)). It never decides anything itself; each row links
to the view where the work is inspected or decided.

- **Counters** for the selected window (last 1 h, 6 h, 24 h or 7 d): agents
  running, items waiting for a person, failed executions and finished
  executions. Clicking a counter focuses the page (Running now, Needs you, or
  history filtered to failed or succeeded); clicking it again or **Show
  everything** clears the focus. `—` means the count could not be read, `n+`
  that it is a lower bound.
- **Needs you**: pending tool approvals, agent questions, hand-off (gated
  message), turn, delegation and environment approvals and human reviews, oldest
  first, linking to Approvals, to the run and to the task.
- **Running now**, grouped per workflow or task run: each agent replica's state
  (running, waiting for capacity, awaiting approval or review, idle…), its
  current plan step, elapsed time, tokens and cost so far, linking to the agent
  panel and run details.
- **Recent history**: executions started in the window with status, agent,
  workflow, duration, tool calls, tokens, cost and error, filterable by status,
  workflow and agent; each row opens its run details.

Window, filters and focus live in the URL
(`#/activity?window=6h&status=failed&workflow=…&agent=…&focus=history`), so
reloads and shared links keep them. The page loads once and then refetches,
throttled to one request per 1.5 s, when the event stream reports
`execution.*`, `replica.*`, `approval.*`, `graph.*` or `agent.plan` events.
A failed refresh keeps the last data and says when it is from; a section the
server could not read, or that the role cannot see (human reviews for viewers),
is stated on the page instead of being shown as empty.

### Health

The **Health** tab (`#/activity?tab=health`, from `GET /api/v1/activity/health`,
see [api.md](api.md#activity-health)) shows, for the last 24 h or 7 d, which
agents and workflows fail, slow down or cost the most. **Group by** switches
between agents and workflows.

- **Rankings**: Fails most (failure rate), Slowest (p90 duration) and Most
  expensive (cost), each linking to the overview history filtered to that agent
  or workflow (failed runs only for Fails most).
- **Table**, busiest first: runs, failure rate, median and p90 duration, tokens,
  cost and the most common errors, normalized so ids and paths do not split one
  error into many and credentials, paths and payload text are not shown. Each
  error links to its latest run; each row links to its filtered **History**, its
  **Last run** and its **Last failure**.

Switching to Health keeps a 24 h or 7 d window and clears the overview's focus
and status filter. The tab refetches, throttled to one request per 5 s, on
`execution.*` and `cost.recorded` events. A failed section (executions or cost)
is stated instead of shown as empty; `—` marks a value that could not be read or
has no finished run.

## Project pages

`#/projects` lists registered projects with name, ID, repository, pinned commit,
execution preference and registration date. Search and environment filters combine.
Server paths and environment preference forms expand within each row.
Administrators use **Add project** to open `#/projects/new` and review an existing
checkout, or choose **Import from URL** at `#/projects/import`. Both flows retain
explicit repository-access confirmation. Registration and selecting a completed
import return to Projects. Each row retains its Create task entry point.

## Connection pages

`#/connections` lists installation connections. The MCP section supports
name/service/ID search and a status filter. Source control has independent
name/service/repository search and a test-status filter. Search and status combine,
and Clear filters resets the current section. Source control test statuses reflect
checks performed while the list is open. Reloading the page resets them to
Not tested. Filters align horizontally on wider screens and stack on narrow ones.
The MCP section's **Add connection** link opens
`#/connections/new`, and template links use `#/connections/connect?...`.
**Add connection** under Source control opens `#/connections/source-control/new`.
Saving or cancelling returns to the list. A source control test reports its
progress and whether the authenticated identity can push to the repository.
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

## Agents

Open **Configuration → Agents** to find agents by name or description. Every
saved workflow agent appears as a reusable card, including agents from earlier
workflows. An agent's **Workflows** tab, next to **Parameters**, lists where it
is used and links directly to the agent configuration in each workflow.

When creating a workflow, choose **Add existing agent**. On the canvas, open
**Add agent** and choose **Choose from catalog** in the side panel. To create a new agent, enter its name and description, then continue to the full Agent configuration page. Cancelling or pressing Escape closes the creation form without adding a node. Search the
catalog, select a version and supply its parameter values. Publishing a version in the catalog leaves saved workflow
versions pinned. In the workflow's agent configuration, **Agent version**
shows the pinned version and lists the published versions of that agent.
Select a version, then review and save the workflow to adopt it.

Agent settings share a draft across tabs. **Unpublished changes** appears
beside **Publish** whenever the draft differs from the published version.
Select **Publish** to include all edits in a single version.

Edit **Workflow instructions** in the workflow's agent configuration under
**Instructions**. This text runs after the shared instructions and applies to
that instance. The workflow owns channels, loop, replicas, workspace and run
limits. Installation policy and task consent apply to the resolved configuration.

## Field help

Choose the question mark beside a setting to see what it controls and how to
configure it. Help opens separately from the field, so you can consult it during
editing or review. Close it with **Close** or **Escape** to return to the same
control.

## Agent configuration

Open **Agent configuration** in a graph, or **Configure agent** in its canvas
inspector. Select an agent and use General, Instructions, Tools & permissions or
Advanced. Deep links have the form
`#/g/<graph>/agent?agent=<node>&section=<general|instructions|tools|advanced>`.
Reload and browser history preserve the selected agent/section; unsaved drafts
remain in memory and are not persisted across a page reload.

General contains identity, runtime and model fields. Instructions edits the prompt
directly in the graph draft. Tools & permissions reuses the existing grant pickers,
approval gates and isolation fields. Search MCP tools by name or browse their
scrollable list. Advanced contains loop, replica/workspace and
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
fields not represented by dedicated form controls. The inspector opens on Overview; Configuration, Output, Messages and Runs are separate sections. Output, message history and turns load when selected. An unsaved node has no runtime history or enabled execution controls. Remove stays contextual to the selected agent or channel and opens the existing affected-channel confirmation.

The workspace toolbar separates workflow changes from execution of the saved version. Draft status, validation, saving, discard and conflict recovery retain their existing semantics. At narrow widths, the inspector overlays the canvas instead of squeezing it out of view. No schema or runtime permission
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

## Approval inbox

Approvals uses a single table for approval requests and application reviews,
ordered by request creation time descending. Search matches title, ID, workflow,
agent or reviewer; status, request type and workflow filters combine. Pending is
the initial status. The API returns up to 200 approval requests per selected
status (200 total for All statuses); table pages contain 20 records. Application
reviews are included when available to the signed-in operator. This is a recent
scope, not an exhaustive archive. Missing historical dates are identified and
sort last.

Select a request title to open its details in a keyboard-accessible dialog.
Closing it makes no decision and returns focus to the title. Approval, rejection,
question answers and application preview/review actions retain their existing
APIs and authorization. Decision failures preserve the entered feedback for retry.
Environment decisions require an administrator. The list keeps healthy sources
usable when another source fails, with an explicit refresh action.

## Workflow catalogue

The Workflows table shows name, description, ID, operational status, saved
version, agents, channels and update time. The optional start time remains below
the update time. Updated sorts descending with a deterministic ID tie-breaker;
missing historical update dates sort last. Search matches name, description or ID
without case sensitivity and combines with the exact status filter. Filtering
resets the page. Pages contain at most 20 workflows and omit task execution copies.

Expand the agent count to configure an agent in that workflow. Select the name
or unused row space to open the canvas; agent links, actions and disclosures do
not navigate the row. The status help explains that Running may mean waiting for
an event, rather than actively processing a task.

Authorized operators retain Start, Stop, Pause and Resume. In-flight actions
prevent repeated submission and expose progress; errors preserve the list for
retry. Administrators create workflows and open More actions to delete, with the
existing confirmation and deferred-cleanup review preserved. Viewers retain
navigation without mutation controls. Loading, failed refresh, empty catalogue
and filtered-empty states are distinct, with an explicit refresh action. Narrow
screens keep horizontal overflow inside the labelled, keyboard-focusable table.

### Compact navigation

Use **Collapse navigation** at the top of the primary menu to show an icon rail; **Expand navigation** restores the labels. The browser remembers the choice. Accessible destination names, the current-page marker and pending-approval counts remain available, and hovering or focusing an icon shows its name. Toggling changes only the layout: it preserves the current route and in-memory workflow drafts. If browser storage is unavailable, a message explains that the choice applies only to this session.

### Models configuration pages

Models opens on configured **Providers**, with search and a combined default/unavailable/plugin filter. **Inference plugins**, **Usage & costs**, **Budgets** and **Model prices** have separate routes. Add and edit open full provider pages at `#/models/providers/new` and `#/models/providers/<name>/edit`; Back to providers and Cancel return to the list. Provider details, authentication and limits retain their help and explicit save/test actions. Stored credentials remain blank and write-only.

Usage shows one breakdown at a time, ordered by cost descending, with text search and 20-row pages. Report limits, unpriced calls and estimates remain visible. Budget and pricing forms retain the existing financial rules and require successfully loaded settings before edits can be saved.

### Editing budgets

In **Models → Budgets**, **Current month budgets** shows the UTC calendar period and each used budget’s monthly limit, spent amount and remaining balance. **Edit budget** opens the matching recurring definition directly below its summary; unused definitions are listed with their configured limit. Changes affect this month and future months, preserving recorded spending. A manual monthly raise remains a floor for the current ledger, so its effective limit can differ from the recurring definition. Cancelling performs no write; errors retain input, and success identifies the saved old/new limit. Removal requires confirmation and preserves spending history.

**Default budget per task** saves separately. Its ceiling applies to new runs; existing runs retain their limits. The default warning level can also be inherited by monthly budgets without an override. Neither save starts a task.

### Skills & instructions

The Skills catalogue shows name, description, origin, state, revision and update
actor/time, newest first. Search combines with state and origin filters; pages
contain at most 20 records. New skill opens a full-page editor. Existing skills
open Content, Workflows and History tabs at `#/skills/s/<id>/<tab>`.

The editor keeps changes in memory across its detail tabs. Save at the top right
creates a revision, while Unsaved changes distinguishes the draft. Write and
Preview share the same Markdown input; preview supports a bounded subset and
keeps HTML and images inert. Built-in content has explicit read-only labels.
Administrators can use **Create editable copy** to open a custom draft with a
new, available ID. Review and Save creates it; opening or cancelling performs no
write. The original stays unchanged, and workflow associations, history and
installation-default status are not copied. Conflicts retain
input. Leaving through Skills navigation asks before discarding changes;
browser reload warns, but drafts are not persisted across reloads.

Workflow associations has its own route, `#/skills/associations`. Select a
workflow to configure skills, installation defaults and repository guidance;
expand an agent to inspect or narrow its selection. Save associations uses the
workflow version and does not start it. The Workflows detail tab links to these
settings and to each agent's workflow configuration.

Inspect instructions at `#/skills/inspect` separates current saved configuration
from immutable execution records. Execution search and 20-row pages bound the
recent list; bodies expand on demand while provenance, hashes and omissions
remain visible. A failed read offers retry and does not invent an empty record.
