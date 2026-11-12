# Task-oriented Workflow Contracts

Status: reviewed on 2026-10-19. Durable drafts, task-owned execution copies, registered project worktrees, native packaging/editor handoff and contextual approval/evidence views are implemented as recorded below. Issue-source selection, environment preferences/preparation and reviewed cleanup are implemented with the limits documented below; external acceptance remains pending.
The current wire contract remains [OpenAPI](../api/openapi.yaml).

## Prompt-first tasks and authorization modes — incremental delivery

**Decision: 2026-10-19. PROMPT-01 editor, PROMPT-02 interpretation and PROMPT-03 public URL import and PROMPT-04 Git authentication are implemented;
PROMPT-05 conditional prompt submission is implemented; authorization modes remain planned.** New task starts with
the Request field, visible workflow/environment selectors and collapsed Review
settings. A new draft derives an editable display title from the first prompt
line, bounded to 200 UTF-8 bytes; explicit and loaded titles are preserved.
Ambiguous required fields open the settings disclosure on validation failure.
Saving still only persists a draft. Resolve request explicitly invokes the default
model; URLs are identified but never fetched. Run task can resolve and start after server review using the existing admission path.
The separate review/confirmation/start API remains available.
The primary view reports existing authorization policies instead of offering
unimplemented modes. No installation permission or shared workflow is changed.

### Implemented request interpretation (PROMPT-02)

**Resolve request** saves the original request before calling the configured
non-mock default model through the existing LLM gateway, without tools. A bounded
JSON proposal identifies the goal, an explicitly named workflow and a repository
URL quoted from the prompt. The server rejects unknown fields, invalid outcomes,
fabricated URLs and unsupported proposals. Model prose and provider error payloads
are not returned as repair instructions. The request itself is retained unchanged.

The visible workflow selector takes precedence. Otherwise a named existing workflow
is used, or `software-factory` when no workflow is named. There is no arbitrary
first-workflow fallback. Entry/result agents and exact channel message bindings
are derived from the actual graph structure, not model-supplied agent IDs. Multiple
entries/results or missing exact message types require Review settings. A resolved
snapshot pins the graph version; normal pre-start validation catches later changes.

Pending interpretation, missing/unavailable models, invalid proposals, ambiguity
and repository prerequisites are persisted on the same task draft. Reload Tasks
and resolve again, or repair and save through Review settings. Compare-and-swap
revisions prevent a slow response from overwriting a newer edit. A process/network
failure can leave a pending draft; inspect Tasks before submitting a new task.
The API may return a storage failure after the initial draft was persisted.

Resolve request identifies repository URLs without fetching them. Use Prepare repository
to explicitly import a public repository, or select a registered project, then verify
the project and pinned commit before saving. Resolving
rebuilds entry/result/channel bindings; the current project/environment/title/issue
settings are retained and normal review revalidates their compatibility. Only these
whitelisted settings can accompany interpretation; they cannot add grants.

Resolution never edits workflows, adds grants, chooses an authorization mode or
starts agents. Unresolved drafts cannot be admitted. Normal Review requirements
continues to expose model, MCP connection, grant, environment and policy repair,
followed by a separate explicit reviewed start. Model interpretation quality with
live providers and human acceptance remain external validation, not fixture evidence.

### Public repository imports (PROMPT-03)

Open **Import from URL** in Projects, or **Prepare repository / Import repository
from URL** in a task. An administrator confirms the download and chooses `HEAD`,
a full `refs/heads/...` or `refs/tags/...` reference, or a full commit hash. Fetching
arbitrary commits depends on the remote server allowing that request. A ready
import records its immutable commit and project; **Use imported project** selects
it without saving or starting the task. Review settings and explicitly start as
before. Operators can select existing imported projects; only administrators can
start, retry or cancel imports. No tools, grants or graph definitions are changed.

The importer accepts public HTTPS smart Git HTTP endpoints on DNS hostnames.
GitHub owner/repository URLs are normalized to clone URLs. Credentials, query
strings, IP literals, private/reserved DNS addresses and redirects are rejected.
Use a canonical clone URL. The transport pins validated public DNS addresses and
does not inherit proxy credentials, Git credential helpers, host Git configuration
or cookies. Authentication failures require explicit selection/repair through PROMPT-04;
anonymous import never triggers a credential search.

Each download is a shallow metadata/object snapshot in a new managed directory
under the server workspace root's `.repositories` directory. Import does not check
out files, run hooks, recurse into submodules, download LFS content, install
dependencies or execute repository commands. Subsequent task admission creates an
isolated worktree at the pinned commit using the existing project mechanism;
managed checkout ignores host Git filters/configuration. Worktrees separate file
changes, not operating-system authority. Native paths are on the host; Docker
paths are on the server/container.

States and byte progress are visible and persisted for resume: fetching, verifying,
publishing, ready, cancel_requested, cancelled, failed and interrupted. Live bytes
are available from the process performing the import; final bytes are persisted.
There are at most two active imports per server process, with a three-minute
network/process deadline and 128 MiB cumulative incoming transfer limit per attempt.
These are not a global disk quota or an expanded-worktree size limit. Cancellation
is available before publishing. Interrupted operations reconcile after their stored
deadline; successful uncertain publication recovers the existing project.

Identical canonical URL/reference requests reuse the same active operation or ready
snapshot, including concurrent requests. Reusing `HEAD` does not refresh a moving
branch: the displayed commit stays pinned. Choose a different explicit ref/commit
for another snapshot. Failed/cancelled/interrupted imports require an explicit retry
with the current revision and use a fresh directory. Partial attempts and existing
checkouts are preserved, never overwritten or automatically deleted. Multi-server
reuse requires access to the same managed project path. Retained attempts may need
separately reviewed cleanup.

Unit, real smart-HTTP Git/API integration and disposable Chromium tests cover import,
reuse, cancellation, failure/retry, permissions and reviewed task execution. They do
not qualify live public hosts, live private-provider compatibility, corporate networks, Windows
process handling or automatic task admission.

### Private Git authentication (PROMPT-04)

**Git authentication** is selected explicitly for each import attempt. The same
administrator confirmation, URL validation, transfer limits and task admission
permissions apply. MCP connections/OAuth and agent tool grants are independent.
The choices are:

- **Public repository (no credentials)**: unchanged anonymous import.
- **HTTPS username and token (this attempt only)**: enter a provider-supported
  username and a token with repository read access. Weft sends HTTP Basic
  authentication only to the selected canonical clone endpoint. Values remain in
  request/worker memory, never in import records, Git config, subprocess arguments,
  environment variables, URLs, events or browser storage. The form clears the token
  after submission, including errors; retry requires re-entry. Use HTTPS when
  accessing a remote Weft installation.
- **Use local Git credential helper** (native profile only): explicitly authorize
  `git credential fill` for this HTTPS host/path, from a fresh directory outside
  repository configuration. It uses the native process user's existing global/
  system helper configuration. Helpers may execute local commands. Weft requests
  credentials but never calls approve/store/erase, reconfigures Git or launches an
  interactive login. Terminal/askpass prompting is disabled and resolution is
  bounded to 15 seconds. A helper's own behavior remains the host user's authority.
- **Use local GitHub CLI authentication** (native profile only): explicitly call
  `gh auth token --hostname <selected host>` using the native process user's stored
  CLI login. Weft ignores GH/GITHUB token environment overrides and does not run
  login/setup-git. The returned token is used as HTTPS Basic password with username
  `x-access-token`. Provider/enterprise compatibility requires external acceptance.

A server/container cannot select native host authentication through the UI or API.
It uses an explicit per-attempt token; no host credential copying or remote helper
agent is required. Native mode refers to the OS user running Weft, not necessarily
the user of a remote browser. These choices do not change task execution permissions.

Local helper output must provide an HTTPS username/password pair; unsupported
credential formats, missing tools, unavailable login and oversized output fail with
sanitized repair instructions. HTTP 401/403 explains that repository read access,
token validity or organization approval may need repair. No upstream/helper stderr
is shown. Configure the selected tool yourself or select a token and retry; Weft
never silently falls back to another credential source. Cancelled/interrupted
attempts retain files and require an explicit new attempt.

Only the selected authentication mode is persisted. Authenticated imports use a
separate URL/ref reuse key from anonymous imports. Within the existing installation
ownership model, a ready authenticated snapshot is shared like any registered
project: selecting it does not revalidate remote credentials or refetch HEAD.
Changing token accounts does not create per-user isolation. Agent permissions still
apply to task execution; cached files are not revoked when a remote token expires.

Private repository means authenticated content, not private-network access: public
DNS/HTTPS checks remain enforced. SSH, private IP hosts, credential vault management,
automatic refresh, credential revocation and push authentication are not part of this
import flow. Tokens from helpers are used only in the fixed upstream HTTP bridge;
Git fetch/checkout still uses isolated configuration and never receives them.

Synthetic real Git HTTP, API, helper subprocess and browser tests cover successful
access, refusal/repair, secret non-persistence, redirect isolation, native-only
selection and unchanged grants. Real private repositories, corporate SSO and OS
keychain prompts remain authorized external acceptance; no real credentials were
read or installation authentication changed for these tests.

### Conditional prompt submission (PROMPT-05)

**Run task** is the administrator-only execution action beside the prompt. Its
visible explanation authorizes a separate execution of the selected/named/default
workflow using existing permissions. It does not add grants or select a broader
authorization mode. Operators retain Save draft / Resolve request; starting still
requires the same administrator authority as the existing start endpoint.

The server saves the original request, resolves it through the existing bounded
interpreter, reviews the effective task requirements and calls the existing task
admission only when `canStart` is true. It revalidates during admission, preserves
workflow versions, owned execution copies, queues, project commits/worktrees and
all tool/message approval gates. No repository credential lookup, clone, environment
build or approval acceptance is inferred from prompt text.

Missing models, ambiguous bindings, invalid model output, missing projects or
unprepared environments leave the task saved. Requirements reload on submitted
drafts. Repair the indicated configuration, explicitly import/select the project or
prepare/approve its environment, then use **Continue task**. Continuing a prompt
reinterprets the request and rebuilds bindings; manual bindings remain available
through Save draft and the existing reviewed Start task path. Runtime approvals
remain visible in task progress and the global approval inbox. Resolving, saving,
reloading or replaying a submission never grants approvals or starts a second run.

`POST /api/v1/tasks/submit` requires `confirm: true` and a bounded client key. A durable
reservation scoped to the API identity binds that key to the exact input fingerprint
and task ID before model/launch side effects. Same-key replays only return the current
task; changed input returns conflict. Concurrent retries cannot create another launch.
A lost response can be retried with the identical key/input. A reservation interrupted
before its draft exists remains uncertain and is not automatically replayed; inspect
Tasks before explicitly submitting a new request. Reservations are retained, not
automatically expired. After reload, continue an existing draft rather than creating
a new task for an uncertain submission. Distinct new keys represent distinct requests.

Launch errors retain existing interrupted/preparing diagnostics and are never
redispatched automatically. The browser retains the key on request failure; successful
blocked submissions can be repaired with a new attempt against the same draft/revision.
No background auto-resume is implied by configuration changes. Synthetic integration
and browser evidence does not qualify live model interpretation or external accounts.

### Primary experience

New task starts with one prompt field. The user can include a repository URL,
the requested change, a workflow name and constraints, for example:

> In https://github.com/example/project, implement GitHub authentication using
> Software Factory. Work in a local worktree and leave the changes for my review
> in VS Code. Do not commit or push.

Resolve the objective, repository/project, workflow and execution preferences
from that request and previously reviewed configuration. Use a named workflow,
or a configured default (initially Software Factory when available and compatible).
Do not select an arbitrary graph or fabricate missing model/access configuration.
The workflow's agents and channels coordinate the work; the user does not need
to manually fill entry agents, message types or payload JSON for an unambiguous,
supported workflow. Keep those bindings inspectable under **Review settings**.

Submitting the prompt can start work when all prerequisites and applicable
permissions are already satisfied. Save the original request and resolved task
configuration durably. Ask only for a necessary clarification, missing model or
repository access, incompatible workflow, or required approval; retain the task
and resume after resolution. Admission must remain idempotent across retries and
reload, version-fenced and independently scoped from the reusable source graph.
Prompt interpretation is untrusted input, never permission to add tools, connect
accounts, override installation policy or alter shared workflow definitions.

Public URL import is available from New task and Projects after explicit confirmation.
Private Git authentication is an explicit per-attempt selection (PROMPT-04).
MCP OAuth access does not automatically authorize Git clone. Native work remains on the user's host; Docker paths remain
on the server/container. Worktrees separate changes, not OS authority.

Keep the prompt, progress, required action, result and changes on the task page.
Progress should describe observable stages such as obtaining the repository,
preparing the worktree, planning, working and waiting for review. Events, tool
calls, traces, costs/usage and execution-tree controls remain available as details;
no security or observability feature is removed. Repository/configuration content
cannot grant authority merely by appearing in an agent's input.

### Configuration and approvals

**Review settings** is collapsed by default. Workflows, models, connections,
skills/instructions and installation policies stay under Configuration. This is
an interface organization for the same user, not a new role model; existing
backend role checks remain authoritative. Do not restore the global onboarding.

Show the same pending approval in the task and the global **Approvals** inbox,
with the operation, target, execution location and consequences. Both views act
on one durable decision and audit history. Approval resumes the blocked work;
rejection/cancellation must have an explicit outcome. An action that policy allows
only after approval can enter this queue. A policy prohibition cannot be bypassed
by approving it: explain the block and link to configuration review instead.
Missing configuration and ambiguous intent are repair/clarification states, not
fake approval requests. Any repair must be revalidated before execution resumes.

### Authorization mode selection

Expose the mode beside the prompt and throughout execution, for example:
**Software Factory · Local worktree · Full access**. The selection applies to the
task and participating agents within their effective grants, including delegated
children without permission amplification. These are planned presets, not claims
that the current gateway already supports task-level mode overrides.

| Mode | Intended behavior |
|---|---|
| Ask for approval | Ask for commands, modifications and external actions classified as requiring approval by the effective policy. Reads need not all become approval prompts. |
| Workspace access | Preauthorize applicable commands and modifications in the task worktree; additional access requires approval when policy permits. This label is not a claim of OS confinement for native commands. |
| Full access | Preauthorize the broad, explicitly reviewed capability set for the task, without individual prompts inside that scope. It does not grant unavailable tools or override mandatory installation policy. |

Choosing Full access requires a concise review showing actual execution location,
accessible files, network, external operations, and whether commit and push are
included. Ordinary use must not repeat this confirmation for every command.
A project may remember an explicitly confirmed preference; scope changes must not
silently inherit broader authorization. Record the actor, mode, reviewed scope,
policy/workflow version and time so automatic approval is auditable. Exact
persistence fields and policy precedence require a design/implementation review.

Authorization mode and environment are independent. Full access in a sandbox is
bounded by that sandbox's effective isolation and network/resource policies. Local
commands execute with the OS user's authority; a worktree alone cannot enforce a
filesystem or network boundary. Before shipping Workspace access for arbitrary
native commands, define enforceable limits or require an explicit escalation;
do not promise containment based on the command's working directory.

MCP tools still require explicit per-agent grants. Selecting Full access cannot
silently authorize a new connection, add tools, expose credentials or grant child
agents more than their parent. Mandatory installation limits and prohibitions
remain active and visible. Commit/push must be represented explicitly in the
reviewed scope; neither follows merely from a code-editing request. Restrictive
user instructions must not be silently broadened by prompt interpretation.

Allow reducing the mode and stopping the task. Revalidate pending and subsequent
actions after a mode change; already dispatched external effects cannot be undone
by changing a selector. Define cancellation behavior for in-flight commands.
Continue recording actions and decisions even when individually preauthorized.
An **Approve for me** risk-evaluation mode is deferred until there is a separately
designed and tested evaluator; do not imply such an evaluator currently exists.

### Validation before delivery

Require unit, integration and browser tests for prompt resolution, ambiguous or
missing inputs, import failure/cancellation/reuse, exact launch binding, automatic
start only within prior authority, reload/retry without duplicate work, contextual
approval/rejection, hard denials, all three modes, concurrent changes, stale policy,
delegated permission limits and retained audit evidence. Include native versus
sandbox semantics and visible limitations. Real vendor credentials, private remote
repositories and user acceptance remain in the final external validation round.

## Issue-trigger readiness and delivery

Open **Graph settings → Issue triggers** on a saved workflow. Select the
tracker, entry and result agents, and source control connection. Saving
records consent for the current source, gateway operations, workflow,
authorization and readiness settings. Edit the trigger and then review
and resume it to consent to a changed scope. The prototype uses the new
trigger policy when a trigger is reviewed again.

GitHub triggers use the official GitHub MCP endpoint with a static
repository-scoped token. Jira Cloud triggers use the official Atlassian MCP
endpoint with a scoped API token that also works at the Atlassian API
gateway. The same reviewed connection serves MCP search and specific
tracker API operations. OAuth and connections with restricted operation
catalogues require separate qualification for trigger API operations.
Provision the GitHub labels in the repository. Manual GitHub requeue
checks for the repository's Maintain or Admin role. Jira requeue checks
project administration permission, which requires the calling account
to have the Administer Jira global permission when checking other users.
[GitHub connector](mcp-integrations.md#github) ·
[Jira permissions API](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-permissions/#api-rest-api-3-permissions-check-post)

Readiness uses the entry agent's configured model in a separate tool-free
call. It checks the issue and eligible replies for a clear objective,
verifiable acceptance and scope where materially ambiguous. The installation
limits are one call, 16 KiB of input, 512 output tokens and 10 seconds.
The monetary check uses the price catalogue and
`readinessMaxMicros` in installation YAML, which defaults to 100,000
micro-units of the catalogue currency per assessment. An unreliable
verdict holds the issue with the reason. No task or execution resources
are created for a held issue.

The trigger replaces the selection label with `weft:needs-attention` and
posts questions on the issue. A substantive reply by the human author or
configured human owner, or a verified maintainer manually adding
`weft:todo`, requests reevaluation. After two question rounds, Weft asks
for operator intervention. The maintainer can clarify the issue and
requeue it for the same readiness and consent checks. Turning readiness
off preserves existing holds. An Operator or Admin can use **Resolve hold**
in the trigger to release one without changing the issue or starting work.
Enabled triggers also release holds when current tracker evidence shows the
issue is ineligible or out of scope. The resolution reason appears in both
Activity and the trigger. Activity and the trigger show the reason, round and any pending
tracker update. Other issues can advance while one is held. At ten holds,
new evaluations stop until a hold resolves.

- `weft:todo` selects issues by default. Each trigger can use a custom selection label.
- `weft:wip` means Weft claimed the issue.
- `weft:needs-attention` means Weft has a question or encountered a failure. The issue comment names the next action.
- `weft:done` means Weft opened a PR and human review is pending. A task that finishes without a PR needs attention.

GitHub PRs target the default branch. For a GitHub issue, Weft appends
`Closes #N` in the same repository or `Closes owner/repo#N` across
repositories. Jira status transitions and automatic closure of Jira
issues on merge are outside this lifecycle.
[Trigger API](api.md#issue-triggers)

## Vocabulary and existing foundations

| Term | Meaning | Current implementation |
|---|---|---|
| Project | Registered repository and execution preferences | Explicit repository registration with immutable initial HEAD and reviewed execution preferences. |
| Workflow | Saved graph defining agents, channels and policies | `domain.Graph`, with versioned definitions. |
| Task | One user request with input, chosen project/workflow and review outcome | Revisioned request and execution-copy binding in installation settings. |
| Graph run | Graph lifecycle from start through stop | `GraphRecord.RunID`; starting an already running graph is not a new run. |
| Execution | One turn of one agent replica | `domain.Execution`, including graph version, run ID, input/output IDs and parent execution ID. |
| Workspace | Files where work executes | Shared graph root or per-replica worktree; neither implies a per-task directory. |

Evidence reviewed: `server/internal/domain/{graph,state,message}.go`,
`server/internal/engine/{control,publish,replica}.go`,
`server/internal/workspace/workspace.go`, `web/src/App.tsx` and
`web/src/components/RunsView.tsx`. Message injection currently reads the latest
graph definition, stamps graph/run metadata and publishes through the engine.
Message history recording is best-effort. Runs currently lists agent turns and
links events from a bounded recent window; it cannot be the authoritative task
ledger. Worktrees are keyed by graph/node/replica and reused across turns.

## Navigation and scope

Work contains Tasks, Projects and Approvals. Configuration contains Workflows,
Connections, Models, Skills & instructions and Administration. These are groups
for the same user; existing viewer/operator/admin checks remain unchanged.
Workflow configuration retains graph editing and direct graph/agent URLs.
Existing Runs, Activity, event, message and diagnostic routes remain accessible.
Configuration drafts must survive navigation as they do today; durable task drafts
must additionally survive reload. No global onboarding overlay is reintroduced.

Outcome presets (understand, implement, investigate, review) prepare editable
instructions and expected outputs. They grant no tools, create no hidden agents
and do not change a shared graph. A synthetic demonstration is clearly labeled
and cannot masquerade as real model/MCP evidence.

## New task and workflow compatibility

1. Describe the request or choose a preset; optionally supply an issue URL/ID.
2. Select a registered project when repository access is needed and a saved
   workflow, such as Software Factory.
3. Review the resolved input target, workflow version, models, effective grants,
   required connections, execution location and environment. Start explicitly.

A workflow needs a reviewed launch binding: either a specific node receiving an
operator instruction, or a channel receiving a typed payload. For a channel,
validate the type, payload and target against the saved topology. Multiple possible
entry points require selection; never choose the first node or inject the request
into every agent. Graphs with background, startup or cyclic behavior need a
supported launch/completion binding before being advertised as task-ready.

The launch binding also declares the terminal result source (a terminal output
channel or supported target execution outcome). Intermediate messages and model
claims of completion cannot close a multi-agent task. Unsupported completion
semantics produce an actionable incompatibility before launch.

Persist the reviewed graph ID/version and resolved launch parameters. Revalidate
at admission: a changed graph or permissions requires renewed review, not silent
use of a newer definition. Starting a stopped workflow can trigger other agents;
show that effect explicitly. Do not promise historical-version execution unless
the engine can bind that version for the whole task. Initially reject incompatible
active graph versions rather than updating a running graph behind the user.

An issue source uses an explicitly chosen MCP connection and granted read tools.
The server validates source/tool availability and preserves the request draft
through authorization repair. Retrieved issue text is untrusted task data. Local
MCP stdio processes remain a Phase B capability, not an assumed Docker feature.

## Durable metadata and admission

Add revisioned project/task metadata through the existing persistence boundary;
no second scheduler, broker or execution engine. Schema, migrations and public
endpoint shapes must be implemented with store conformance and API tests before
being added to OpenAPI. The global Tasks page requires bounded installation-wide
listing; a graph-only endpoint proposal is insufficient for this navigation.

A project record needs an opaque ID, display name, repository reference/location,
execution-location capability, revision and preferred environment. Registering a
path grants no new filesystem access. Native paths on the user's computer are
unavailable to a remote server unless explicitly supported; secrets do not belong
in repository URLs or project metadata.

A task record needs ID, revision, actor, title/request, optional project reference,
workflow ID/version, launch binding, reviewed environment, timestamps, admission
key, status/blocker, authoritative message/execution links and result references.
Store only credential references. Apply existing content limits and redaction;
request text and tool payloads must not become metric labels or audit log dumps.

Creation saves a draft. Start atomically claims its revision and persists an
admission intent before dispatch. Retries with the same key and content return
the original task; conflicting content is rejected. The admission intent and
engine dispatch need a durable deduplication/reconciliation handshake. A broker
publish timeout is not proof of failure: mark it unresolved and reconcile rather
than blindly publish again. Do not infer exactly-once remote tool side effects.

Server-owned task correlation propagates through operator inputs, channel
messages, turns and delegated children; client/runtime supplied IDs cannot attach
work to an arbitrary task. A turn must not batch unrelated tasks. Persist explicit
membership rather than infer it from time, actor, node, run ID or recent events.
Historical executions lacking task identity remain historical runs, not invented
tasks. Restart recovery loads durable bindings and reports uncertain/gapped state
without presenting success or automatically repeating work.

## Concurrent reuse and workspace boundaries

Selecting a workflow reuses its saved definition. Admission creates an explicitly
reviewed execution copy (`task-run-<task suffix>`), preserving normalized runtime,
model, grants, policies and channel definitions. The review explains that starting
runs the entire workflow and the request targets only the selected entry agent.
This replaces the earlier proposed shared-graph serialization approach: a copy
reuses existing graph-scoped queues, memory, approvals, children and managed
workspaces without changing the original definition or adding a second engine.
Copies are visible in workflow listings and linked from task details; they are
not silently created or usable as templates. Legacy graph mutation routes reject
the reserved namespace. Task control routes verify the persisted ownership binding.

Two tasks therefore have distinct managed roots and engine state. This does not
isolate OS processes or external resources referenced by tools. Existing absolute
workspace paths and environment definitions are currently blocked at admission;
registered local project worktrees and task-owned environment preparation remain
pending. No automatic commit, push or cleanup occurs.

## Lifecycle, results and interruption

Task states are draft, queued, running, succeeded, failed, cancel_requested,
cancelled and interrupted. A separate blocker describes approval, capacity,
connection repair or uncertain dispatch; approvals are not successful completion.
Terminal status comes from the declared completion binding and settled relevant
work. A successful intermediate agent turn cannot imply whole-task success.

The task view presents request, progress, required user action, result, changes
and test evidence. Tool outcomes remain distinct from agent summaries. Partial
telemetry is explicitly identified. Details link to existing executions, events,
approvals, traces, token budgets and file inspection without broadening access.

Cancel targets task-owned queued input and running roots/descendants through the
existing control mechanisms. It must not stop unrelated graph activity. Keep
cancel_requested until workers/leases settle; an unavailable runtime does not
justify claiming cancellation completed. If task-scoped cancellation is not yet
supported, show that limitation instead of substituting Stop all. Retain bindings
and budget/accounting evidence needed for reconciliation under retention policy.

Review readiness is separate from execution success. A result may be ready for
human review without a commit. Open in VS Code is available only for an accessible
local workspace; remote paths are not presented as local paths. Commit, push,
merging and cleanup are separate explicit decisions. Browser file inspection
remains read-only; manual editing/review in the user's editor is supported.

## Capability gates and delivery dependencies

FLOW-02 may implement navigation while preserving existing execution views. It
must not relabel all agent turns as complete user tasks. FLOW-03 owns durable
metadata/admission and launch compatibility before enabling New task. FLOW-04 adds
issue input; FLOW-05 adds capability-aware environment selection; FLOW-06 adds
aggregated tracking; FLOW-07 adds review handoff. Native unavailable capabilities
remain visibly unavailable until LOCAL tasks deliver them.

Local worktree executes with host OS authority and is not a sandbox. Show brief
English help linking to [Why Git Worktrees Matter](https://woliveiras.com/posts/why-git-worktrees-matter-coding-agents/).
Sandbox selection retains existing environment approval and runtime policies.
No automatic fallback from sandbox to native, secret import or expanded grants.

## Required implementation acceptance matrix

| Boundary | Required evidence |
|---|---|
| Navigation | Old deep links, OAuth return, keyboard/narrow layouts, preserved drafts and role-denied controls. |
| Admission | Missing/ambiguous targets, invalid payloads, stale graph/revision, denied grants, unavailable model/connection and idempotent duplicate start. |
| Persistence | SQLite/filesystem/PostgreSQL adapter coverage as applicable; reload, publish timeout, crash between intent/dispatch and restart reconciliation. |
| Scope | Two requests sharing a workflow cannot mix messages, memory, files, approvals or results; legacy paths respect admission constraints. |
| Completion | Multi-stage/cyclic workflow boundaries, tool failure with agent success, missing events and declared result evidence. |
| Cancellation | Pending inputs, running parents/children, repeated cancel, unavailable runtime and no impact on unrelated tasks or token totals. |
| Review | Dirty checkout preservation, unsupported remote editor handoff, no automatic commit/push and protected cleanup. |

Each behavior delivery requires unit and real component-boundary integration tests,
relevant browser E2E, make test, make lint and make build. This design-only delivery
requires documentation/link checks, not new runtime tests. Human walkthrough,
external provider consent and clean native installation remain final acceptance;
no implementation, deployment or external validation is claimed here.

## FLOW-03 foundation — durable drafts (2026-10-19)

Implemented: operator/admin creation and revision-protected editing of drafts,
viewer reads, bounded cursor listing, explicit saved workflow version and enabled
entry-agent selection, five editable outcome choices, reload/resume and a read-only
requirements preview. Drafts use existing installation settings CAS storage under
`tasks/drafts/`; no new database service or execution engine is introduced. The
UI saves on explicit request, not automatically; save before leaving an unsaved
form. A stale graph version or revision does not overwrite the draft.

The review endpoint returns current graph/provider/environment validation plus
blockers. Draft review blocks start until an enabled result agent is selected and current requirements pass.
Saving/reviewing never injects a message, starts an agent, modifies a graph or grants
a tool. The task form currently targets an agent; channel bindings, registered
projects, current effective grant presentation, project-specific admission, channel-target input and broader workflow completion semantics remain pending.

`task.draft_save_requested` records metadata-only mutation intent before storage
CAS. Audit failure blocks the save. This event does not claim a save succeeded;
the revisioned draft is the authoritative outcome. History contains no request
body in this event. SQLite and filesystem reopen tests prove draft persistence;
API tests prove revision/version errors and viewer denial. This foundation is not
full FLOW-03 completion or standalone installation acceptance.

## FLOW-03 execution copies (2026-10-19)

Admin-only start preserves the existing graph-creation permission. Operators can
prepare drafts, and viewers can inspect evidence. Start claims the draft revision
before preparing resources, records metadata-only transition intents, inserts a
stable operator-message ID, then starts the copy. Repeated start requests return
the existing state without another dispatch. Submitted requests are immutable.
Uncertain partial preparation remains visible and is never automatically retried
by the task API. Normal engine recovery/redelivery rules still apply to work
already admitted; this is not an exactly-once guarantee for external tool effects.

Progress derives from persisted copy executions and current replica, queue and
approval state. The explicitly selected result agent supplies the result; active
work and pending approvals prevent success. Failed/interrupted turns prevent a
success projection. At the 1,000-execution inspection cap, completion is unknown.
The stored admission status and observed progress are separate; the list displays
the observed state when available. Missing telemetry is not evidence of success. Once/event
workflows are supported initially; unsupported modes are blocked before start.

Stop affects only the owned copy, waits for stopped workers in the progress view,
and retains queued messages, workspace files and history. It does not restart
retained messages. Generated summaries remain distinct from tool outcomes, with
links to execution details and workspace inspection. This increment does not
complete FLOW-03, FLOW-06 or the standalone installation.

## Registered project worktrees (2026-10-19)

Projects registers an absolute repository root on the Weft server, with admin
write/viewer read checks. Registration resolves symlinks, verifies Git and records
an immutable initial HEAD; it does not start agents or copy uncommitted files.
The task explicitly selects a project and full commit. Review and admission
recheck the registered path and commit. A workflow with its own repository/path
override cannot silently replace this choice.

Admission creates `weft/task-run-<id>` in the managed task workspace from the
reviewed commit. Existing directories or branches cause an error, never force
replacement. Failure preserves partial resources for inspection. Source dirty
files and HEAD remain unchanged; different tasks have different worktrees and
branches. Git hooks and fsmonitor are disabled for these management commands;
repository-configured checkout filters can still run, so task start authorizes
local repository processing under the server OS account. Worktrees do not isolate
OS permissions. No commit, push, merge, source reset or cleanup is automatic.

The UI explains server versus container paths and links worktree background
information. A container-mounted repository still runs commands in the container;
this delivery does not pretend it is the user's host. Native packaging and explicit editor handoff are delivered below; reviewed project execution preferences are available. Existing file/diff inspection is linked from task progress.

## Structured input and retained outputs (2026-10-19)

Tasks can select an external queue ingress with exactly one entry agent and no
routing condition. Message type and bounded JSON payload are reviewed explicitly;
channel type filters are validated before admission. Request prose remains task
context and is not silently converted into a provider-specific schema. For the
Software Factory example, select `requests`, type `feature.request`, and provide
the documented `title` and `description` payload. **Software Factory v2**
(`software-factory-v2`) uses the same `requests` input; declare the project's
checks so its implementer and verifier can run them with `check.run` (see
[native-coding-runtime.md](native-coding-runtime.md#software-factory-v2-nat-10)). The Docker example requires its external runtime; the preinstalled native variant
uses native agents and the configured default model. A template is not a runtime
availability proof.

The existing engine publishes the input once after the durable task claim, before
starting workers, preserving message approval gates. An uncertain publication
leaves the copy stopped for inspection rather than replaying the task submission.
Retained audit/terminal output groups with no consumer do not count as runnable
work. Rejected or expired message approvals produce a failed observed outcome
once active work settles, including when no turn has begun. Informational graph
warnings are displayed separately from runtime requirements that block start.
Task completion itself never invokes commit/push, but an agent may perform actions
already authorized by its workflow grants; review those grants before starting.


### Native editor handoff

Task progress offers an encoded `vscode://file` link only when the API identifies
an actual native-profile launch, the browser uses a loopback hostname, and the
user confirms that browser and Weft run on the same computer with VS Code
installed. This confirmation is local to that displayed workspace and is not a
permission grant. A tunnel can also use loopback; it must not be mistaken for
same-computer execution. Server/container installations continue to display their
server-side path without a host-editor link. Opening the editor is explicit and
does not commit, push, clean up or change tool permissions. The UI does not claim
to detect an installed editor. Browser tests inspect the link without launching
an external application.


### Synthetic first task

Administrators can explicitly prepare an echo-only example from Tasks. The page
previews the behavior and requires confirmation before saving an example workflow
and task draft through the existing authorized APIs. It neither starts the graph
nor calls a model/tool/account. Normal task review and start confirmation still
apply. Partial preparation errors retain records and direct users to inspect them;
there is no automatic retry of uncertain writes. The example is labeled synthetic
throughout and does not claim repository analysis. Operators/viewers do not receive
a shortcut around the existing graph-creation permission.


### Task review evidence

Task progress embeds the existing read-only Git inspection on demand: current
status, tracked-file diff against HEAD (including staged changes), bounded history
and observed workspace operations. Nothing is fetched until Inspect changes is
selected. Untracked file contents remain available through Workspace files.
Execution links open Tool calls for command/test evidence; a successful agent
summary does not prove test success, commit or push. Declared checks and their recorded runs are loaded with the evidence
(read-only graph and events requests) and listed under Checks. The inspection is current
workspace state, not an immutable execution snapshot. Existing read permissions,
Git path validation and observation limitations remain unchanged.

For registered-project tasks, Git inspection resolves the source repository only
from the persisted task/project binding, verifies graph ownership and expected
workspace location, and retains the existing bounded private metadata snapshot.
Changing a worktree's `.git` pointer cannot select arbitrary external metadata.


### Reviewed workflow input suggestions

Choosing a workflow suggests its entry only when there is one eligible external
queue channel (or one enabled agent with no ingress), and its result only when
there is one terminal agent. A single literal message type is filled; wildcard
or ambiguous types remain an explicit choice. Suggestions use the actual graph,
not its name or template label. All fields remain editable and backend review
still validates the saved version, input contract, runtime and grants.
Generated title/description JSON tracks request edits only while it still equals
the generated payload; customized JSON is preserved. Goal presets provide editable
initial instructions, and switching goals never replaces a customized request.
These instructions are not permission restrictions or new tool grants.


### Contextual decisions and execution evidence

Task progress shows pending approvals for its owned execution copy. Decisions use
the existing approval API and still appear in the inbox. Viewer and operator/admin
boundaries remain unchanged; environment approval remains administrator-only.
Unavailable or bounded-out approval telemetry is distinct from an empty list.

Selecting an execution loads its bounded tool event history inline. Completed
execution status does not imply successful tools or passing tests. Recorded token
usage is shown with the parent/child double-counting caveat. An expandable execution
tree reuses existing budget, child interruption and whole-tree controls, including
confirmation and server-side authorization. Activity and diagnostics are also
expandable inside the task page. Existing detailed routes remain available.


### Reviewing agent worktrees

Under **Changes and checks**, select **Workspace to review** to inspect the
shared task workspace or a prepared agent replica. Each scope uses the existing
read-only Git inspector; native editor handoff requires the same-computer
confirmation for the selected path. A missing status snapshot is not evidence
that a workspace was deleted. Agent worktrees are independent: changes there
need not appear in the shared task workspace or the registered source checkout.
The interface does not infer a successful push or passing tests from completion.


### Selecting execution location

**Execution environment** is retained with the draft. **Use workflow environment**
keeps the saved workflow settings. **Local worktree** requires a native Weft
installation, a registered project and process isolation for every enabled agent.
**Sandbox** requires every enabled agent to use Docker isolation; review probes
only the configured sandbox daemon with a bounded read-only request. An
unavailable daemon blocks sandbox admission without affecting local tasks.
Selection does not rewrite the workflow or grant tools; review agent environment
settings when a workflow is incompatible. For a workflow with an explicit environment definition, review it and choose
**Prepare task environment**. The task keeps a stopped copy while the existing
environment manager builds it. After readiness, select **Review prepared task
requirements**, confirm, and **Start prepared task**. Builds never start agents
automatically. Failed builds retain diagnostics and require an explicit retry;
stopping preparation retains environment state, images and caches. An approval
for an agent-generated environment belongs to its original graph and is not
transferred to a new task copy; use an explicitly reviewed workflow definition.

Administrators can save a **Preferred execution environment** on the Projects
page. It is suggested when a project is selected for a task; it does not change
existing drafts or bypass workflow/capability validation. Concurrent edits require
refreshing the project before saving again.

## Issue sources

Select GitHub or Jira, an explicitly granted connection, and an issue reference in
New task. GitHub accepts `owner/repository#123` or a `https://github.com/.../issues/123`
URL. Jira accepts `PROJECT-123` or an HTTPS `/browse/PROJECT-123` URL plus the site's
cloud UUID. The chosen cloud ID is authoritative; a URL hostname is not proof
that it matches that site. Credentials, queries and fragments in URLs are rejected.
GitHub Enterprise URL mapping is not included in this selector.

Saving and reviewing store/validate only the reference. The selected entry agent
receives a bounded read instruction in its task execution copy: GitHub
`issue_read` with `method: get`, or Jira `getJiraIssue`. The source workflow and
its grants are unchanged. An exact grant is required; current catalogue and
connection checks still apply before admission and at each gateway invocation.
Approval, argument constraints, scope failures and quotas retain their existing
behavior. The model may fail or decline the read: inspect recorded tool outcomes,
not just the task summary. Retrieved issue text is untrusted data, not authority
for changing instructions or granting access.

Unavailable grants remain visible on saved drafts. Repair the connection and
workflow in Configuration, return to the same draft, select the current workflow
version if changed, and review again. No automatic MCP import or permission
expansion occurs. Local stdio connections use the same exact tool contracts.

Contracts reviewed against the [GitHub MCP tool catalogue](https://github.com/github/github-mcp-server/blob/main/README.md)
and [Atlassian supported tools](https://developer.atlassian.com/cloud/rovo-mcp/guides/supported-tools/).
Catalogue availability depends on the server version, account and scopes. Automated
TLS fixture/browser coverage does not qualify a real Jira site or GitHub account.

### Delivery verification

Task evidence labels checks, commits and push separately. Checks are not
independently verified merely because an agent completed: the Checks entry
lists every declared check of the task, with "not run" or the latest
recorded `check.completed` run per agent replica (filtered to the selected
workspace): passed, failed with parsed failures, could not run, or stale
when the worktree changed after the run. It claims "all passed" only when
every declared check passed on the current workspace, and names test or
check-definition files changed in the task, since a pass does not vouch for
a rewritten test. With no declared check it says "Not independently
verified". The entry refreshes every 15 seconds. Commands run through `shell.exec` remain evidence to inspect in Tool
calls, not a verified result. Checks are declared by the workflow
(`spec.checks`) and the project (`PUT /api/v1/projects/{id}/checks`) and merged
into the task copy when it is prepared; see
[Native coding runtime](native-coding-runtime.md#verification-loop-nat-05). Local commits are observable through the selected worktree's
Git history; task completion does not infer a commit. Remote push state is not
verified by this screen and no network fetch is performed to infer it. The task
lifecycle never commits or pushes automatically; existing granted agent tools
remain visible in the pre-start review.


## Following a prompt task

The task page shows the request, current stage, next action, result and workspace
changes together. Pending approvals use the same durable IDs as the global inbox;
approval resumes the existing workflow, while rejection remains in decision history.
Stopping preserves files/history and requests cancellation, without undoing external
effects. A refresh error does not imply success. Tool outcomes, traces, usage and
delegation trees remain available under evidence and diagnostics. Review configuration
when a mandatory policy denies an operation; approval cannot override that denial.
Task authorization modes and their native limitations are documented in
[Task authorization](task-authorization.md).
