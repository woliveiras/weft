---
id: TASK-010
title: Multi-repository Tasks
summary: Give one agent access to several reviewed repositories in one task, verify their changes together and track each pull request.
status: implemented
scope: [workflow task defaults, shared repository selection, task drafts and admission, agent workspace, combined checks and review, per-repository delivery]
risk: high
risk_domains: [repository isolation, authorization, persistent checkout ownership, SCM side effects]
reversibility: Existing task formats remain readable. Existing multi-repository tasks and delivery receipts must remain inspectable if new admissions are disabled.
change_surfaces: [task domain and persistence, API and console, workspace preparation, native runtime and tool gateway, SCM delivery]
contracts: [versioned workflow task defaults, task draft and consent revision, checkout ownership, declared check evidence, review candidate, delivery receipts]
review_policy: The maintainer approved the workflow and task repository selection flow. The maintainer requested implementation against the specification and matrix.
navigation: [specs/README.md, specs/product-and-tasks/multi-repository-tasks/matrix.md]
documentation: [docs/features/workflows/README.md, docs/features/tasks/README.md, docs/task-workflows.md, docs/task-authorization.md, docs/native-coding-runtime.md, docs/api.md]
authority:
  granted: [draft specification, matrix, index update, local implementation and fixture verification]
  withheld: [live external SCM actions, deployment, release]
dependencies: [TASK-006, TASK-007, TASK-008, TASK-009, ENV-002, ENV-004, RUN-003, RUN-005, REPO-GUIDANCE]
---

# Multi-repository Tasks

An operator asks one agent to move a service's health endpoint. The backend
serves the new path, the frontend calls it and the infrastructure probe uses it.
The operator can review one task, see a test exercise all three checkouts
together and follow a separate PR for each repository that changed.

## Background and ownership

Before TASK-010, a task could select one registered project or inherit one repository from its
workflow. A registered-project task starts at a reviewed commit and owns a
private checkout. A workflow can already suggest one registered project to
new tasks through its versioned task defaults (TASK-009 AC-003). TASK-010
extends that suggestion, task selection, verification and completion to a
set of repositories. TASK-006 continues to own consent, TASK-007 admission,
TASK-008 each checkout and RUN-005 each push and PR.
[Project setup](../../../docs/features/projects/README.md) and
[task execution](../../../docs/task-workflows.md).

## Acceptance criteria

### AC-001 — Select and review the repositories

In the task editor and API, an operator can save, reload and review one draft
with multiple registered projects. Each entry has a stable task-local name
(such as `infra`, `backend` or `frontend`), a project ID and a full base
commit. When PR delivery is requested, review also resolves the target
repository, base branch and SCM connection for each potential PR. Local work
can be reviewed without a delivery connection. Reordering the list does not
change which project a name or target identifies.

In **New task → Workflow & repositories**, a single **Repositories** dropdown
selects one or more registered repositories. The workflow editor uses the same
control for **Suggested repositories** (AC-008). The field displays the selected
names and count. Opening it or typing filters the options by name or source.
Each option identifies the source, ref and abbreviated commit so registrations
with the same name can be distinguished. Each toggle immediately updates the
current editor and leaves the dropdown open. Filtering, Escape, Tab and clicking
outside preserve the selection. Arrow keys move between options and Enter
toggles the active option. Escape closes the list with focus on the field.

Each task row shows the repository, its workspace directory, its source ref
and abbreviated base commit. **Review task** shows the same set from the
saved draft revision. The operator can expand the full base commit and, when
PR delivery is requested, see the destination repository, target branch and
SCM connection beside each entry. Returning to edit keeps the draft. Applying
a selection or reviewing it does not authorize execution.

Review identifies the offending entry for duplicate projects or aliases,
unsafe or colliding directory names, unavailable commits, mismatched delivery
targets or a competing workflow-owned repository. The operator can correct
the draft without losing the other entries. Review performs no remote writes.

### AC-002 — Admit the complete reviewed set once

Consent binds the saved revision, repository names, projects, commits,
delivery targets, required checks and execution environment. An edit to this
scope invalidates prior review and consent. Submitted scope is immutable.
Admission revalidates every binding and prepares an independent, task-owned
Git checkout and branch for each project at the reviewed commit. Two tasks
selecting the same project can work independently of each other and of dirty
files in the registered source.

One task dispatches its input once, after **all** checkouts are ready. If the
last checkout fails, the operator sees its name, the reason and the status
of the already prepared checkouts. Concurrent starts or a restart cannot
dispatch a subset or create a duplicate run. Uncertain ownership blocks
recovery for inspection. Cancellation retains work and existing effects.
Reviewed cleanup verifies ownership, branch, HEAD and cleanliness for each
selected checkout and reports each outcome under TASK-008.

### AC-003 — Work across checkouts in one execution

One native coding agent receives the repository names and working directories
in the same execution. It can read an API definition in one checkout, edit a
consumer in another and update the probe in a third. Each checkout has a
distinct directory under the task workspace. File, Git and shell operations
resolve to an explicit repository name or unambiguous task-relative path.
An ambiguous Git, diff or checkpoint request must identify a repository
before it can proceed. Repository instructions and skills retain their source
repository and local scope when the agent switches directories.

Existing workspace access, command grants, approval and network policies
apply. Weft's file and Git gateways confine operations to their bound roots.
In Docker the agent sees task-owned checkouts with private Git metadata.
Registered source directories and other tasks' checkouts stay outside the
mount. Native process execution keeps its documented host permission model.
Review identifies incompatible environments or replica layouts before start.
No fallback may broaden access to make another checkout reachable.

### AC-004 — Verify the combined change

The task lists project checks by repository, even when several are named
`test`. The operator can also select a declared integration check and review
its owning project, command, working directory, participating repositories
and whether it is required. For example, a check declared in `infra` can
start the task's backend, call it through the task's frontend and exercise
the configured probe. It runs through the existing check gateway and grants.
If the runner can read additional task checkouts, they also count as inputs
and appear in the reviewed participant list.

A result records the outcome and the workspace fingerprint of **each**
participant, including uncommitted changes. A required check counts as
passing only for the verified combination. A participant changing during
or after the check, or an incomplete fingerprint, prevents a current pass.
The task distinguishes failed, stale, not run and unable to run. Individual
project passes cannot satisfy a required integration check. A local-only
task can finish after its required checks and workflow gates are satisfied.

### AC-005 — Review one candidate and deliver per repository

The operator can inspect one review candidate with the base, proposed change
and applicable check results for each repository, plus the combined results.
A recorded approval covers that exact combination. Changing any member
invalidates the outstanding candidate approval before further delivery.

For PR delivery, the reviewed candidate identifies every changed repository
and its destination. After required checks and existing delivery gates pass,
the SCM gateway pushes the reviewed head and opens or reuses one PR per
changed repository against its own base branch. Unchanged repositories are
shown as unchanged and receive no push or empty PR. Weft shows each PR link
beside its repository in the same task. Each operation uses the connection
bound to its target. The agent never receives its credentials.

### AC-006 — Recover partial delivery and report the task outcome

If one PR exists and another fails, the task shows the existing PR, the
failed repository and the remaining work. It cannot report successful PR
delivery until every changed repository has a confirmed PR for the approved
candidate. A task requiring a PR with no changes exposes the unmet delivery
requirement under TASK-007.

After a retry, concurrent request, timeout or restart, reconcile each
repository's push and PR with the reviewed target, branch and head before
repeating an external write. If a provider created a PR but its response
was lost, recover the same PR. An unverifiable remote outcome stays visible
and blocks that retry. A failure or cancellation retains existing PRs.
Activity and the task's delivery view identify the task, repository, phase
and outcome without exposing credentials.

### AC-007 — Preserve existing tasks and callers

Existing zero- and single-repository requests, drafts, workflow defaults,
running tasks and historical results keep their existing API, working
directory and delivery behavior. Existing single-repository tool calls need
no new argument. An explicit list and a competing legacy project selection
produce a validation error. A client that only understands one repository
cannot overwrite a multi-repository draft by silently dropping its members.

### AC-008 — Reuse workflow suggestions in new tasks

In **Workflow → Settings → Task defaults → Suggested repositories**, an
administrator selects or clears suggestions directly in the dropdown. An
empty selection expresses no preference. Each toggle edits the workflow draft. Saving the workflow makes those suggestions
available to new tasks using that version. **New task with these defaults**
opens a new draft from the saved version. Saving pending changes enables
this action. The suggestions identify projects. The task records the
concrete base commits and reviews delivery destinations.
Saving suggestions neither initializes the workflow workspace nor grants
repository or delivery access.

For example, *Software Factory* suggests `backend` and `frontend`. A new
task starts with both selected. The operator adds `infra` for a health
endpoint change, or removes `frontend` for a backend-only change. These
edits belong to that task. Another task created from the same saved workflow
still starts with the original pair. Saving different workflow defaults
affects future tasks using the new version. An existing draft keeps its
selection, including an explicitly cleared list, when reopened or when the
project catalog finishes loading. An explicit project at task entry takes
precedence under TASK-009 AC-001. If a suggested project is unavailable,
the editor identifies it and lets the operator replace or remove it before
review can succeed.

## Scope and open decisions

- The first acceptance scenario uses a manually created task, registered
  projects and a shared Docker workspace. Existing repository import can
  supply projects before task setup. Native and per-replica execution require
  qualification for their respective workspace and permission models.
- This feature covers coordinated verification and PR creation. Cross-repository
  merge ordering, atomic releases, deployments and issue-trigger automation
  are outside its scope. Each PR follows its repository's review process.
- The maximum number of repositories per task is open. Qualification must
  cover at least three. Any limit must be visible at review and enforced by
  the API before preparation.

## Reconciliation

The [verification matrix](./matrix.md) maps the acceptance scenarios to the
unit, API, browser and Docker integration tests. The [task guide](../../../docs/features/tasks/README.md)
describes the operator flow.
