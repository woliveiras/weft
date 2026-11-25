# Task setup and readiness

The task editor brings configuration facts and repair guidance together before
execution. Open it from **Tasks → New task**, **Projects → Create task**, or a
workflow's **New task** action. The last two carry the selected resource into a
new draft. See [tasks](../tasks/README.md) for the complete draft-to-delivery flow.

## Read the setup summary

After the request and workflow/repository choices,
**Task setup summary** shows the following facts. A configured resource is not
proof that the saved task can start; **Run task** automatically saves and checks admission.

| Fact | What to inspect or correct |
| --- | --- |
| Workflow & input | Saved workflow version, receiving agent and direct/channel contract; missing configuration remains visible |
| Repository | Registered project, repository and concrete base commit, or workflow repository initialization; otherwise an empty managed workspace |
| Model | Discovery loading/unavailable, no provider configured, or a configured provider whose per-agent model still needs review; echo agents do not need inference |
| Environment | Selected execution mode and configured agent isolation; review determines compatibility and whether preparation is allowed |
| Delivery | Named source-control hand-off or absence of one; existing workflow grants remain separate |
| Access | Read-only launch review, Ask default and any explicitly confirmed mode; Run authorizes the displayed scope |
| Budget | Applicable limits must be reviewed before start; recorded spend/admission remain authoritative |

Repository/provider discovery failures do not mean that no resources exist.
Unavailable selected IDs remain visible for repair. The SCM selector separately
shows loading, discovery failure with retry, a real empty result, and a saved
selection absent from the loaded list. Retrying discovery does not save or start
the task or silently select another connection.

## Prepare for the intended result

**Prepare this task for a specific goal** offers explanatory guidance, rather
than changing the task's settings or requesting a model:

| Setup goal | Required choices and boundaries |
| --- | --- |
| Run work in a repository | Registered project or workflow repository, compatible workflow/input, model, environment, permissions and budget |
| Deliver a pull request | Repository and source-control hand-off; required PR approval is a later, separate decision |
| Work from an issue | Issue service/reference and project scope; automatic polling belongs to workflow triggers |
| Review a pull request | Review workflow, exact PR identity/revision and explicit read/comment grants; reading does not grant merge authority |

Three integration types serve different responsibilities:

- **MCP** discovers external tools. Agents can call only tools explicitly granted
  to them, subject to connection restrictions and approvals.
- **SCM** holds credentials for managed Git and pull-request delivery. Selecting
  a project or granting an MCP tool does not configure this hand-off.
- **Jira issue access** supplies issue reads and configured lifecycle operations.
  Jira Cloud REST access and Atlassian MCP access have separate credential
  consumers; one being ready does not establish the other is ready. A Jira
  reference does not choose a Git repository or authorize SCM delivery.

The [Connections guide](../connections/README.md) describes the corresponding
configuration and tests. Goal guidance does not combine these resources or
broaden an agent's authority.

## Repair configuration and return

1. Correct task-local selections in the visible editor whenever possible.
2. An administrator can choose **Configure models and return** or **Configure
   connections and return** from setup guidance. The task first saves dirty
   edits, or creates the draft when needed, and waits for the server to
   acknowledge the saved task revision.
3. Only after a successful save does navigation open configuration with a local
   `returnTo` task destination. A failed save displays an error and keeps the
   edited task on screen; no configuration navigation or agent start occurs.
4. The configuration page offers **Return to saved task**. Perform any separate
   resource save required by that editor, then return. The return link itself
   changes neither authorization nor execution state.
5. Use **Run task** against the saved configuration. It inspects the latest
   requirements and presents newly discovered scope before execution. Environment
   preparation remains a separate explicit action.

Requirement-repair links also retain the saved task destination. They identify
model configuration and the MCP authorities referenced by reviewed grants.
Users without administrative authority see that an administrator must repair
the dependency; their ability to save a draft is not administrative permission.
The accepted return destination is restricted to local saved-task routes, not
external URLs or execution commands.

## Suggestions and persistence

Workflow `spec.task` defaults supply reusable suggestions, never tool grants,
credentials, consent or arbitrary input payloads. Authored entry/result/channel
defaults beat topology inference; explicit project context/task selections beat
the workflow's project preference. Loading context applies only to a new draft
and preserves later explicit choices. Saving captures a concrete workflow
version and selected project base. A future workflow publication does not
silently update that pin.

**Run task** saves exact edits, checks the acknowledged revision and starts only
when the refreshed scope matches facts already displayed to the operator. New
scope is shown for another explicit Run decision. It never calls `/tasks/prompt`
or the combined `/tasks/submit`. Optional **Resolve request** remains the model
interpretation action in Advanced settings. **Prepare task environment** remains
a separate operation and never starts agents on completion. See
[environments](../environments/README.md) and [approvals](../approvals/README.md).

## Implementation and verification map

- [TaskReadiness](../../../web/src/components/TaskReadiness.tsx),
  [TasksPage](../../../web/src/components/TasksPage.tsx),
  [TaskRequirementRepairs](../../../web/src/components/TaskRequirementRepairs.tsx).
- [Entry and return context](../../../web/src/components/taskEntryContext.ts),
  [task defaults](../../../web/src/components/taskDefaults.ts),
  [workflow defaults editor](../../../web/src/components/WorkflowTaskDefaults.tsx).
- [Entry/review integration tests](../../../web/src/components/TaskEntryFlow.test.tsx),
  [context unit tests](../../../web/src/components/taskEntryContext.test.ts),
  [repair integration tests](../../../web/src/components/TaskRequirementRepairs.test.tsx),
  [task draft browser tests](../../../web/e2e/task-drafts.spec.ts).

Product contract: [guided operator journeys](../../../specs/product-and-tasks/guided-operator-journeys/spec.md).
