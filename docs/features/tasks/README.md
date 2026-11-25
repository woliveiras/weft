# Tasks

A task combines a work request with a reviewed workflow version and records its
own execution. Its normal entry is **Tasks → New task** (`#/tasks/new`). Existing
requests open at `#/tasks/<id>`. The list searches title, task ID and issue and
filters by status/workflow, with the most recently updated records first.

**Create task** from Projects opens `#/tasks/new?project=<id>` with that
registered repository selected. **New task** from a workflow opens
`#/tasks/new?workflow=<id>` with its current saved version and compatible input
suggestions. These entry contexts apply only to a new task after discovery;
they do not replace an existing draft or later explicit choices. Unknown IDs
remain visible for correction rather than selecting a replacement resource.

## Before starting

- An appropriate [workflow](../workflows/README.md) must be saved. Task entry and
  result agents must be enabled; task workflows support `once`/`event` agents,
  rather than arbitrary continuous or scheduled loops.
- A real model provider is needed for model-backed work. Echo-only work is a
  separate case. Provider discovery failure is not proof that none exists.
- Choose a [project](../projects/README.md) for a registered repository and
  reviewed task worktree commit. Without a project, the task inherits workflow
  workspace initialization; it is empty only if that workflow has no repository.
  A project and a workflow-level repository cannot both own the task workspace.
- Configure [source control](../connections/README.md) for managed push/PR
  delivery. Selecting a repository alone does not configure delivery.
- Operator/admin can edit and save requests. Administrative authority is
  required for task submission/start, environment preparation and authorization.

## 1. Describe and configure the draft

The ordinary editor keeps **Request**, **Workflow** and **Project** visible.
The summary explains the selected version, repository/base, execution environment,
delivery, access and budget. **Advanced settings** contains title/goal, optional
model interpretation, environment overrides, entry/result agents, channels,
message type/JSON, issue source and SCM hand-off. Missing or invalid required
advanced configuration opens that section automatically; it is never hidden
behind a closed disclosure while it blocks launch.

The workflow version defaults to the current version when no positive version
was selected. An existing pinned version is retained until explicitly changed.
The server can still reject start if the source workflow has changed since the
draft was reviewed; retaining a pin does not guarantee admission of stale work.

Saved workflow **Settings → Task defaults** (`spec.task`) can suggest entry and
result agents, channel/message type, project and source-control connection.
Authored defaults take precedence over topology inference. A project selected
at task entry or in the editor takes precedence over the workflow project
preference. Selecting a workflow deliberately replaces routing suggestions;
late resource loading preserves explicit choices. The saved task captures a
concrete workflow version and selected project base commit, rather than a
moving "current" alias. Defaults never contain execution consent or grants.

When authored defaults are absent, the UI suggests bindings when there is one
unambiguous queue ingress/receiver or a single enabled agent, and can infer one
terminal result agent. Ambiguous entry, result or message choices remain explicit. Generated channel payloads
follow the request while they remain generated; custom JSON is not silently
rewritten. Channel delivery sends its JSON, not an additional direct copy of
the request. Consult the workflow's expected message contract.

Execution environment is a task compatibility choice:

- **Use workflow environment** inherits each agent's configured isolation; this
  can mean Docker or process execution.
- **Sandbox** requires Docker isolation for every enabled agent.
- **Local worktree** requires native Weft and separates files, not process or
  network permissions.

## 2. Run the task or save a draft

**Run task** is the primary action for new tasks, saved drafts and retries. It
validates required fields, saves outstanding edits and checks the acknowledged
revision automatically. It never calls the request interpreter. **Save draft**
is the alternative when the operator wants to return later; saving does not
start work. Unsaved edits retain the browser navigation guard.

Saved drafts load a read-only launch review automatically. Before execution,
the screen shows workflow/version, input, repository/base, environment, grants,
access mode and applicable budget. Run checks these facts again. If they still
match the displayed review and admission permits it, the explicit click starts
the task. Newly discovered scope is shown inline for another explicit **Run
task** click. There is no separate Save → Review requirements → Start checklist.
A changed authorization scope, unavailable dependency or stale workflow version
blocks execution with its correction beside the task. Scope changes never
silently choose Full access or replace a pinned version.

A failed save retains local edits and prevents review/start. If saving succeeds
but review fails, the acknowledged revision is retained; retry does not create
another task. Start errors are shown without replacing manual input. Repeated
clicks cannot dispatch concurrent starts. Loading and successful/failed checks
have explicit feedback. Server revision fencing and one-time admission remain
authoritative even if configuration changes between inspection and start.

**Resolve request**, inside Advanced settings, is optional and may call a model.
It preserves selected title, project and environment while rebuilding bindings.
Invalid proposals retain the request and explain the missing configuration.
Interpretation never grants tools or execution consent. The console continues
to use save/review/start endpoints; `/tasks/submit` keeps its existing API contract.

## 3. Access modes and mandatory approvals

The access review describes the receiving agents, tool grants and execution
boundaries. Task modes are:

| Mode | Effect |
| --- | --- |
| Ask | Granted tool calls require task decisions by default |
| Workspace | Eligible workspace operations can run without repeated prompts; other calls still ask |
| Full | Reviewed granted tools can run without repeated task prompts |

Mandatory approvals, tool constraints and installation policy still apply in
every mode. Workspace mode is not available for command configurations that
cannot provide its isolation contract. Full access is not a sandbox selector.
Only explicit administrative confirmation changes authorization; a model
proposal cannot supply consent. Running tasks can reduce autonomy subject to
the authorization contract; a call already in flight may finish.

## 4. Prepare an environment when necessary

If the toolchain image is missing, the task presents preparation as its next
step instead of only a raw graph-validation error. Preparation creates the
task-owned execution copy and can require a `task_environment` approval before
building. This is distinct from an agent tool-call approval.

The task enters `environment_pending`. **Environment & logs** contains the build
state, bounded logs and a Follow control. Loading, unavailable snapshots and
confirmed absence are distinct states. A failed build can be inspected and
explicitly retried. Preparation finishing does **not** automatically start
agents: readiness automatically refreshes the launch review, then the operator
uses **Run task** after inspecting the visible scope. Failed inspection blocks
start and offers retry; no extra inspection button is required. See [environments](../environments/README.md).

## 5. Start, monitor and review delivery

Starting uses the saved revision and a fresh admission decision. Work runs in
the task-owned execution graph, with a snapshot of the selected template;
changing a reusable workflow does not edit that submitted task configuration.

The tracking page has a status/action summary and separate sections:

| Section | What the operator can establish |
| --- | --- |
| Overview | Prominent agent plans and recorded result; completed steps are agent claims |
| Environment & logs | Preparation state, logs, retry and prepared-task start decisions |
| Changes & checks | Workspace selection, Git changes/history, declared checks, recorded tool evidence and checkpoints |
| Activity | Executions, decisions, messages and events in the task's scope |
| Request | Original work and issue context |
| Settings & access | Source workflow/version and reviewed execution permissions |

Progress polls every three seconds. **Refresh task** shows a busy state and
acknowledges refresh even if status did not change. Failed refresh retains the
last valid snapshot and reports failure; cost refresh failure is separate from
task failure. A missing first snapshot is not displayed as an empty environment.

A recorded result is not proof of passed tests, a commit or remote delivery.
Use declared check records and tool outcomes for verification. Managed SCM
delivery records branch/push/PR outcomes separately and can require approval
before opening the PR. A configured review route is another execution; task
success does not imply merge. Historical execution links open the task-owned
inspection experience, not an editable reusable graph.

## Retry a failed manual task

Open the failed task, review the explanation beside **Try again**, then select
the button. This prepares a separate editable attempt with the same
request, workflow/version, agent/input bindings, repository/base commit and
source-control connection. It does not start agents. The new attempt links to
the original; its approvals, environment, workspace and cost belong to it.
The original history, plans, files and costs remain available.

Review the new draft and use **Run task**. Its launch review and admission checks
are the same as for an ordinary task; source-task consent is not inherited.
If the workflow changed, explicitly select the desired version and repair any
incompatible bindings; retry does not silently upgrade it. Inspect external
state and existing PRs before repeating work that may already have produced
side effects. This is a new attempt, not resume at a specific tool call.

When an approval has status **expired**, the task explains that the old decision
can no longer be approved. A new attempt needs its own approvals. Other failures
may coexist; expiry is not automatically diagnosed as the only cause.

Creation shows progress and errors. Repeating a request after a lost response
opens the same attempt, including later edits or execution. Deleting that retry
draft does not permit the original request to recreate it. Operators/admins can
prepare retries; readers cannot. Issue-trigger tasks use trigger recovery;
nonfailed tasks do not offer this action.

## Stop, delete and recover

- **Delete draft** requires confirmation and the matching revision. It is
  permitted only while the task is still a draft with no execution graph or
  environment approval. Operators/admins can delete eligible drafts.
- Deletion records a tombstone, hides the request from ordinary reads and
  prevents delayed saves/interpretation from resurrecting it. It does not delete
  repositories, credentials, workspaces or execution history.
- Prepared or executed tasks cannot use draft deletion. **Stop task** requests
  cancellation and retains evidence/files. It does not undo external actions.
- Checkpoint restore is a separate confirmed action against a selected agent
  workspace; it is not automatic failure rollback.
- Cleanup has a separate administrative preview/confirmation boundary. Stopping
  a task is not permission to remove persistent resources.

Authentication/permission errors require the appropriate role; repeated clicks
do not change it. Stale revisions, malformed JSON, incompatible environments,
missing source bindings and budget limits are actionable blockers, not a reason
to silently substitute settings or start a different workflow.

Task IDs and execution graph IDs are distinct. Short task IDs retain the legacy
`task-run-<suffix>` mapping. Long IDs (including deterministic retry IDs) use a
bounded `task-run-h-<digest>` graph name. The original task ID remains in the
graph's `weft.task` label and the task's `executionGraphId` binding. Authorization,
delivery and execution-evidence navigation verify that ownership rather than
reconstructing a long task ID from its graph name.

## Implementation and verification map

The main HTTP handoffs are below. The table locates operations; payload and
precondition details remain in OpenAPI.

| Operation | HTTP handoff | Authority |
| --- | --- | --- |
| Save/create draft | `POST /api/v1/tasks`, `PUT /api/v1/tasks/<id>` | Operator/admin |
| Interpret request | `POST /api/v1/tasks/prompt` | Operator/admin |
| Combined API submission, separate from the normal console launch flow | `POST /api/v1/tasks/submit` | Admin |
| Inspect admission | `GET /api/v1/tasks/<id>/review` | Viewer or higher |
| Confirm task mode | `PUT /api/v1/tasks/<id>/authorization` | Admin |
| Prepare/start/stop | `POST /api/v1/tasks/<id>/prepare-environment`, `/start`, `/cancel` | Admin |
| Follow execution | `GET /api/v1/tasks/<id>/progress` | Viewer or higher |
| Retry failed manual task | `POST /api/v1/tasks/<id>/retry` with revision/confirmation | Operator/admin |
| Delete eligible draft | `DELETE /api/v1/tasks/<id>` with revision/confirmation | Operator/admin |
| Preview/confirm cleanup | `GET` / `POST /api/v1/tasks/<id>/cleanup` | Admin |

| Boundary | Source |
| --- | --- |
| Form, coordinated launch and contextual entry | [TasksPage](../../../web/src/components/TasksPage.tsx), [launch scope](../../../web/src/components/taskLaunch.ts), [task defaults](../../../web/src/components/taskDefaults.ts), [entry context](../../../web/src/components/taskEntryContext.ts), [task types](../../../web/src/api/tasks.ts), [HTTP client](../../../web/src/api/client.ts) |
| Tracking, plans and preparation | [TaskDetails](../../../web/src/components/TaskDetails.tsx), [TaskPlan](../../../web/src/components/TaskPlan.tsx), [TaskEnvironment](../../../web/src/components/TaskEnvironment.tsx) |
| Access and evidence | [TaskAuthorization](../../../web/src/components/TaskAuthorization.tsx), [TaskEvidence](../../../web/src/components/TaskEvidence.tsx), [TaskCheckpoints](../../../web/src/components/TaskCheckpoints.tsx) |
| Draft persistence and execution | [taskconfig](../../../server/internal/taskconfig), [taskrun](../../../server/internal/taskrun), [task API](../../../server/internal/api/tasks.go) |
| Regression coverage | [draft E2E](../../../web/e2e/task-drafts.spec.ts), [prompt E2E](../../../web/e2e/task-prompts.spec.ts), [entry and review tests](../../../web/src/components/TaskEntryFlow.test.tsx), [journey tests](../../../web/src/components/TaskJourney.test.tsx), [draft deletion tests](../../../web/src/components/TaskDraftDelete.test.tsx) |

Owning guides: [task workflows](../../task-workflows.md),
[authorization](../../task-authorization.md), [HTTP contract](../../../api/openapi.yaml).
