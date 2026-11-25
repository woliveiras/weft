# Tasks

A task combines a work request with a reviewed workflow version and records its
own execution. Its normal entry is **Tasks → New task** (`#/tasks/new`). Existing
requests open at `#/tasks/<id>`. The list searches title, task ID and issue and
filters by status/workflow, with the most recently updated records first.

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

All three setup sections are visible; repository and receiving-agent choices
are not hidden inside a Review settings disclosure.

| Section | Inputs and decisions |
| --- | --- |
| **1. Describe the work** | Request, goal and title. Title initially follows the request's first line until edited. **Resolve request** optionally asks the default model to interpret the request and save a proposal. |
| **2. Workflow & repository** | Workflow, version dropdown, project/registered commit and execution environment. **Import repository from URL** exposes the import flow when no registered project is suitable. |
| **3. Input & delivery** | Entry agent, result agent, direct instruction or channel delivery, message type/payload for channels, optional issue source and source-control hand-off. |

The workflow version defaults to the current version when no positive version
was selected. An existing pinned version is retained until explicitly changed.
The server can still reject start if the source workflow has changed since the
draft was reviewed; retaining a pin does not guarantee admission of stale work.

The UI suggests bindings when there is one unambiguous queue ingress/receiver
or a single enabled agent, and can infer one terminal result agent. Ambiguous
entry, result or message choices remain explicit. Generated channel payloads
follow the request while they remain generated; custom JSON is not silently
rewritten. Channel delivery sends its JSON, not an additional direct copy of
the request. Consult the workflow's expected message contract.

Execution environment is a task compatibility choice:

- **Use workflow environment** inherits each agent's configured isolation; this
  can mean Docker or process execution.
- **Sandbox** requires Docker isolation for every enabled agent.
- **Local worktree** requires native Weft and separates files, not process or
  network permissions.

## 2. Save, interpret or continue

**Save draft** persists an editable request revision; it does not start agents.
Edits are local until saved. Leaving an unsaved task form triggers the existing
dirty-form protection. Saving configuration invalidates earlier review consent.

**Resolve request** is optional and may call a model. It preserves selected
title, project and environment choices while rebuilding input bindings. Invalid
proposals retain the original request and explain which setup needs repair.
Use **Configure task manually** to move to the relevant visible sections.
Interpretation never grants tools or execution consent.

For a **new** request, **Run task** uses the submission endpoint: it can
interpret, save and start if admission and authorization requirements are met.
Otherwise it returns a saved task and the next required review/preparation step.
Submission replay protection prevents a retry from creating duplicate work.

For an **existing draft**, **Continue task / Review and run** does not reinterpret
the request. It validates the local configuration, saves changes and reviews
that saved revision. Missing fields receive inline guidance, an invalid-state
border and an actionable setup message. Failed save/review must preserve the
operator's edits. A successful review is not itself an execution start.

## 3. Review requirements and authorize access

**Review requirements** checks the saved configuration, model, workflow
version, agents, bindings, repository, environment and permissions. Review
results are usable only for that task revision; dirty fields require saving and
reviewing again. A version conflict requires an explicit current-version choice
and renewed review, not repeated authorization refreshes.

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
agents: review prepared requirements again, confirm the selected action and
use **Start prepared task**. See [environments](../environments/README.md).

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

## Implementation and verification map

The main HTTP handoffs are below. The table locates operations; payload and
precondition details remain in OpenAPI.

| Operation | HTTP handoff | Authority |
| --- | --- | --- |
| Save/create draft | `POST /api/v1/tasks`, `PUT /api/v1/tasks/<id>` | Operator/admin |
| Interpret request | `POST /api/v1/tasks/prompt` | Operator/admin |
| Submit a new request | `POST /api/v1/tasks/submit` | Admin |
| Inspect admission | `GET /api/v1/tasks/<id>/review` | Viewer or higher |
| Confirm task mode | `PUT /api/v1/tasks/<id>/authorization` | Admin |
| Prepare/start/stop | `POST /api/v1/tasks/<id>/prepare-environment`, `/start`, `/cancel` | Admin |
| Follow execution | `GET /api/v1/tasks/<id>/progress` | Viewer or higher |
| Delete eligible draft | `DELETE /api/v1/tasks/<id>` with revision/confirmation | Operator/admin |
| Preview/confirm cleanup | `GET` / `POST /api/v1/tasks/<id>/cleanup` | Admin |

| Boundary | Source |
| --- | --- |
| Form, continuation and review | [TasksPage](../../../web/src/components/TasksPage.tsx), [task defaults](../../../web/src/components/taskDefaults.ts), [task types](../../../web/src/api/tasks.ts), [HTTP client](../../../web/src/api/client.ts) |
| Tracking, plans and preparation | [TaskDetails](../../../web/src/components/TaskDetails.tsx), [TaskPlan](../../../web/src/components/TaskPlan.tsx), [TaskEnvironment](../../../web/src/components/TaskEnvironment.tsx) |
| Access and evidence | [TaskAuthorization](../../../web/src/components/TaskAuthorization.tsx), [TaskEvidence](../../../web/src/components/TaskEvidence.tsx), [TaskCheckpoints](../../../web/src/components/TaskCheckpoints.tsx) |
| Draft persistence and execution | [taskconfig](../../../server/internal/taskconfig), [taskrun](../../../server/internal/taskrun), [task API](../../../server/internal/api/tasks.go) |
| Regression coverage | [draft E2E](../../../web/e2e/task-drafts.spec.ts), [prompt E2E](../../../web/e2e/task-prompts.spec.ts), [journey tests](../../../web/src/components/TaskJourney.test.tsx), [draft deletion tests](../../../web/src/components/TaskDraftDelete.test.tsx) |

Owning guides: [task workflows](../../task-workflows.md),
[authorization](../../task-authorization.md), [HTTP contract](../../../api/openapi.yaml).
