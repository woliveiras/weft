# Chat

Open Chat from the left navigation or the console's home URL. Start with
**New chat** above **Recents** on the right. On a narrow screen, open that
panel from **Sessions**.

## Choose the model

An administrator selects a configured provider and model in
**Models → Chat model**. The console opens stored conversations and manual
pages without a usable model. If interpretation becomes unavailable, use
**Retry saved request** after restoring the model or budget. Supply provider
credentials only in the existing Models configuration form.

When a saved conversation stays **Untitled chat**, use **Retry title** to
generate a title from the first request. The transcript and any open reviews
remain available while the title is generated.

## Inspect installation state

Ask which workflows are active, which providers are configured, how much the installation spent this month or which approvals are pending. The reply shows a dated snapshot from the corresponding Weft service and a link to inspect it. Lists show up to eight entries. A dependency failure leaves the result unavailable.

Use `/inspect` to see the supported resources, or enter a shortcut such as `/inspect budgets`. These explicit reads work without a Chat model and incur no inference cost. Connection inspection uses stored status. Testing reachability remains a separate action.

## Open a console page from a conversation

Ask “Where can I inspect my model budget?” or “Where do I enter the MCP connection credentials?” Follow the link in the reply to the relevant console page. Enter credentials in its setup form. Use browser Back to return to the same conversation. Opening a page leaves decisions, saves and execution to the controls on that page.

When a connection needs browser OAuth, follow the Chat link to Connections and complete the normal provider consent. After **Complete OAuth connection**, use **Return to chat** to return to the originating message. Weft checks the conversation with the signed-in identity before offering the link.

## Follow a task

Ask Chat to show the current state of a task or follow it until it settles.
Use a task ID or a unique exact title. For example, “Let me know when my
release notes review finishes.” If Weft cannot resolve a unique task, supply
the ID from Tasks. Open Approvals to inspect held requests.

Start another chat or close the browser. Weft keeps observing active tasks.
Recents shows the running indicator and signals an unread result, with a
green dot for success. Failure, cancellation and a required decision have
their own accessible status. Open the original conversation and follow the
task result link to its evidence. If the provider is down, the monitor
continues to read task state without calling that provider.

Completed watches and task inspections show the pinned base commit, checkout state and available delivery evidence for each repository. A confirmed PR receipt shows the PR number. **View task result** opens Changes & checks for that attempt. A retried task can also link to its previous attempt.

## Review a pull request

Ask “Revise o PR 418.” If several running workflows or connections qualify, name the workflow and connection. The reviewer needs a native event node, one exclusive ingress, a single input per turn, an explicit model and a named repository read grant. Connections with interactive read approval require the existing manual path.

The preview identifies the repository, source and base commits, model and workflow version. **View workflow** opens the selected node. **Analyze PR** checks the revisions again and sends the bounded diff to the workflow model for one billed, tool-free analysis. Existing channel and turn gates still apply. The draft appears in the conversation with **View execution**. A missing receipt leaves the outcome unknown. Requesting analysis grants no publication permission.

## Prepare a task environment

Ask to prepare the environment of a saved task by ID or unique name. An administrator can review the pinned workflow, task revision, repository commits and saved build definition before confirming. The session reports readiness when the native environment matches the reviewed definition. Use **Inspect task** for build diagnostics, existing preparations or approval holds. After readiness, ask to start the saved task. The start card identifies the prepared build, its completion time and task authorization. Confirmation rechecks readiness and admits the task once.

## Correct a task draft

Identify the draft by its ID or unique title and specify the replacement title, request, or outcome. Compare the current and proposed text, then select **Save draft correction**. The draft keeps its workflow and repository bindings. When it uses a generated channel payload, the title and request update there as well. Open **Tasks** to review custom payloads, resolve incomplete bindings or change repositories. A newer draft or workflow revision requires a fresh review.

## Save a managed skill

An administrator can ask to create a skill with a name and instruction text, or revise an existing skill by ID. Compare the text and current audience on the review card, then choose **Save skill revision**. New skills start disabled. A revision preserves the skill’s activation, scope and description. **View skill** opens its saved content. Built-in skills remain read-only.

## Create a reusable agent

As an administrator, describe the agent's name and what it should do. Chat
shows the proposed ID, instructions, selected model, read-only workspace and
empty tool grants. Check the definition and choose **Create agent** or
**Decline**. If there is exactly one active proposal, type
“Confirm this proposal” or “Decline this proposal” to decide it. Open **View
agent** after the catalog confirms version creation. Adding the definition
to a workflow and running it are separate actions.

A stale or expired review requires a new proposal. If a response is lost,
return to the conversation to check the catalog receipt before trying again.

## Publish shared agent instructions

As an administrator, name the catalog agent and describe the shared
instruction change. Compare the complete current and proposed instructions
and choose **Publish agent version**. The new version appears in Agents.
Each workflow continues to use its own pinned version until it adopts a
different one.

## Adopt a catalog version in a workflow

Name the workflow, node and version to adopt. Review the old and new shared
instructions before choosing **Adopt agent version**. The save changes the
pin of the selected node and preserves its local instructions and parameters.
Later turns in the reusable workflow pick up the saved version at rollout.
A task that already started keeps the workflow copied at admission. Use the
workflow editor to review versions that change runtime, grants or other
shared settings.

## Create a workflow using an existing agent

As an administrator, name the workflow and the catalog agent it should use.
If the agent has multiple versions, give the version number or explicitly ask
for its latest saved version. Review the exact version, task input and
inherited grants before choosing **Create workflow**. This saves a stopped
workflow with one agent and leaves task creation and execution for separate
actions. Open **View workflow** to inspect the saved graph in the editor.
Use Workflows to design a custom topology or configure required agent
parameters.

If another person saves that workflow ID while you are reviewing it, Chat
shows a conflict and you can ask for a fresh proposal with a different ID.
If the creation outcome is temporarily unknown, inspect Workflows before
asking to try again.

## Prepare a task draft

As an operator or administrator, name the workflow and describe the task.
Chat shows the workflow version, request, outcome and selected agents before
**Create task draft** saves anything. Open **View task draft** to inspect its
settings and review what it needs to run. Use Tasks when the work requires a
repository or an input that cannot be determined from the saved workflow.
If someone edits the workflow before you confirm, ask for a new proposal.

## Start and follow a saved task

As an administrator, ask Chat to start the saved task by ID or title. Review
the task and workflow revisions and their authorization, then choose **Start
task**. The start goes through the task's normal budget and approval checks.
Chat follows the admitted task in the same conversation and provides a
separate link when its result is available.

Use Tasks to configure and start work with repositories, environment
preparation or delivery settings. If the task or its authorization changes
while you are reviewing, inspect the updated setup and request a fresh start
proposal.

For registered repositories, the start card shows each alias, project revision, pinned commit and declared checks. A concurrent project edit invalidates the proposal. Issue-backed drafts identify the connection and issue. For a task configured to deliver changes through SCM, inspect and start from Tasks.

## Cancel a task attempt

As an administrator, name the running task to cancel. Confirm **Cancel task
attempt** after checking its task and run IDs. The cancellation interrupts
work and closes pending approvals for that attempt. Chat follows the request
until the task record confirms cancellation. Its history and captured
workspaces remain available. Use Tasks for draft or environment controls.

## Review task authorization

As an administrator, name the task and the desired mode. Review the workflow
version, repository scope and declared tool grants before choosing **Save
task authorization**. This records consent for that task. A running task can
only narrow its current authorization. Use **Open Settings & access** to
inspect the saved scope or edit a project preference.

## Decide a pending approval

As an operator or administrator, ask to approve or reject a pending message
or turn request by approval ID. You can name the task if it has exactly one
pending request. For an agent question, give the approval ID and your answer.
Review the exact request, decision and any response text before confirming.
Open Approvals for the full request and for environment, delegation or tool
decisions. A recorded decision can unblock work. Follow the task or execution
to see its outcome.

## Prepare a retry after failure

Ask Chat to retry the failed manual task by its ID or title. Review the
original attempt and choose **Prepare retry draft**. Open **View retry**
to review the linked task with its copied request and workflow version.
Starting the retry requires its own authorization and review. For
trigger-owned tasks, use the trigger's recovery controls.

## Control a workflow or agent

Name the workflow and, for a single-agent change, the node. Choose **Pause
new turns** to let current work continue, **Drain and stop** for a graceful
stop, or **Interrupt now** to cancel active turns in the selected scope.
Start and resume go through the workflow's usual runtime and budget checks.
Chat shows the target, requested state and observed runtime state. An
ambiguous request such as “Deactivate the reviewer” needs a choice about
trigger intake, new turns, draining or immediate interruption.

If the workflow or run changed during review, open the workflow to inspect
its current state before confirming a fresh proposal. Trigger intake has
separate configuration controls.

## Change instructions for one workflow

Name the workflow and node, then describe the replacement for that node’s **local instructions**. Review the old and proposed text and the pinned catalog version. **Save workflow instructions** creates a new workflow version. The reusable agent’s instructions and existing task copies stay on their recorded versions. Future turns use the normal workflow rollout. **View workflow** opens the selected node in the editor.

## Guide the next turn of a running agent

Specify the workflow ID, node ID and replica, or explicitly select any replica. Ask for one message on the next turn and choose **Send guidance** after checking the run and message. The node’s operator chat receives the message. A queued message can only be picked up by a matching replica in the selected run. Current turns continue with their existing input.

The card shows when the runtime picked up the message. The runtime can return it to the queue after an interrupted delivery. A pickup receipt establishes delivery to the runtime. Inspect the execution separately to determine what the agent did.

## Review installation configuration

As an administrator, ask for a concrete change such as “Set the task budget to two dollars with an eighty percent warning.” Review the object names, current revisions, field changes and any intake consent before choosing **Apply configuration**. A plan can include up to eight objects. The installation service validates the complete plan before applying its first change.

Each result names the object and its outcome. If a plan stops midway, inspect the applied objects, correct the failure and request a fresh plan. An unknown outcome requires inspection of the installation. Use the existing setup form or installation CLI to enter or rotate credentials. Chat can preserve existing credentials.

## Archive and access

Use **Archive conversation** to move a conversation out of Recents. In
**Archived**, open it to view new results or restore it. Archiving keeps the
watch and read state. Active and archived conversations are retained without
automatic expiration. The Archived tab shows an unread count even while you
are browsing Recents. Use **Older conversations** to load the next page.

In an authenticated installation the owner can access their conversations.
Auth-disabled local installations show **Shared local history**. People using
that local installation can access the same conversations. Private history is
scoped to the authenticated static or OIDC principal.
