# Chat

The message field stays at the bottom while the conversation scrolls above it.
Type `/` for commands. Use the arrow keys and Enter or Tab to complete a suggestion.
Send with **Ctrl+Enter** or **Command+Enter**, or select **Send**. Your message appears immediately with a sending label.
The field clears and stays locked while Weft prepares its reply. If delivery
cannot be confirmed, the text returns to the field for another attempt.

Open Chat from the left navigation or the console's home URL. Start with
**New chat** above **Recents** on the right. The magnifier next to **Recents** and **Archived** searches titles across the chosen list. Filtering keeps the current conversation and draft open.


Model-assisted turns run in the controller using Eino. The server owns session history, authorization and pending reviews. To roll back orchestration for new turns, start the controller with `WEFT_CHAT_ORCHESTRATOR=direct`. Existing reviews can be confirmed after restart. [Architecture and local verification](../../../specs/product-and-tasks/conversational-console/matrix.md#local-eino-pilot)

## Compose a first request with action pills

In a new chat with a configured Chat model, choose **Tasks**, **Agents** or
**Workflows** above the message field. **More** opens **Projects**,
**Connections**, **Models** and **Skills & instructions**. Available choices
follow your role. Open **Administration** from the main navigation.

Choose **Tasks → Follow a task** and search by task name or paste a full task
ID. Each result shows the title, current state and a short ID. A project
reference appears when available. Scroll the results to compare tasks with
the same title. If the list shows only part of the inventory, refine the
search to see more specific matches.

Select a task to review its full ID above the message field. **Change task**
returns to the results with your notes intact. Choose **Send and follow**
or use the Send shortcut to start watching. **Find a task** uses the same
picker and opens the result with **View task**. Agents, workflows, projects
and skills use this picker too. When a selection completes a request,
choose its named action or use **Add details** to include a note. You can also clear the selection or
send your own text while exploring an incomplete branch.

An exact selection is recorded as `/follow <task-id>` or
`/lookup <area> <id>`. Additional lines stay with the saved request.
These commands use the selected ID directly. If the resource disappears,
choose a new target. For an incomplete search, refine the name or enter
the exact ID. A failed search offers **Retry search**.

Links labelled **Open** take you to the named editor. Browser Back restores
your place in the guide and your unsent text. To prepare a catalog agent,
enter its name and purpose, then select **Send**. Review the proposed
definition before confirming creation.

## Choose the model

An administrator can select a configured provider and model directly in Chat.
Choose **Choose Chat model here** during first use, or send `/inspect chat-model`
and use that action in the reply. **Save Chat model** keeps the current conversation
and unsent message. The same setting is available in **Models → Chat model**. The console opens stored conversations and manual
pages without a usable model. If interpretation becomes unavailable, use
**Retry** after restoring the model or budget. Supply provider
credentials only in the existing Models configuration form.

When automatic naming fails, **Untitled chat** shows a notice and **Edit title** in the header. Hover or focus a sidebar title for its full name, last activity and status. Use its three-dot menu to **Rename**, **Archive** or **Unarchive** that conversation. Select an archived conversation to read it, then restore it to send another message.

When inference fails, the saved reply identifies the next step. A budget rejection opens Budgets, an unavailable model opens Chat model settings, and a busy model can be retried after a slot becomes available. With a proposal open, **Confirm** and **Decline** and their explicit text equivalents (“confirm this proposal” or “decline this proposal”) use the reviewed operation directly. Weft still checks the current role, revision and expiry before acting.

Weft replies support formatted lists, emphasis and code, including when you reopen a saved chat. Resource links and action buttons appear below the reply.

## Inspect installation state

Ask which workflows are active, which providers are configured, how much the installation spent this month or which approvals are pending. The reply shows a dated snapshot from the corresponding Weft service and a link to inspect it. Lists show up to eight entries. A dependency failure leaves the result unavailable.

Ask “How many tasks succeeded?”, “Is anything running now?” or “List the three most recently updated failed tasks.” The Chat model chooses the task query and writes its answer using the returned evidence. Counts include every matching task. Lists show up to eight matches, ordered by last update. A follow-up such as “Which ones failed? Use the same format” can reuse the previous list size and presentation while querying the new status.

Use `/inspect` to see the supported resources, or enter a shortcut such as `/inspect budgets`. These explicit reads work without a Chat model and incur no inference cost. Connection inspection uses stored status. Testing reachability remains a separate action.

Use `/inspect execution <id>` or ask about an exact execution to see its observed state, tree root and number of active executions. **View execution** opens the selected execution. A cancellation request and the execution's eventual terminal status are shown separately.

An operator can ask to cancel an execution tree or only a particular delegated child. The review identifies the root, selected execution, workflow and run. Confirmation records a cooperative request. Chat continues to observe the reviewed scope and reports whether an execution actually reached the cancelled state. **View execution** opens its native evidence.

Ask about the delegation policy or type `/inspect delegation` for the saved approval mode, limits and policy revision. Open **Delegation** to inspect or edit the full policy.

### Inspect monthly model usage

Ask “Which provider and model cost the most last month?” or request a monthly breakdown by user, project, workflow or task run. Chat uses the same retained cost records as **Models → Usage & costs**. Each group includes calls, tokens, recorded cost, estimated cost and unpriced calls. Search and page through up to 100 groups per dimension, with 20 groups per page. The installation total always covers the whole month. Reports use UTC calendar months.

## Find a resource by name

Ask “Which workflows mention release?” or type `/find workflows release`.
Enter `/find` for the available resource types. Use `/find nodes reviewer`
to search workflow agents across reusable workflows. Each node result names
its parent workflow. A numbered reply shows up to eight matches. Answer
`2`, `o segundo`, a distinguishing part of the displayed name, or
`/choose 2`. You can reopen the conversation before answering.

When a request such as “follow the release task” identifies several tasks,
the choice resumes that request. For an operation that changes Weft,
inspect the review and confirm it separately. If the search is incomplete,
narrow the name or choose an entry that was actually found. The explicit
`/find` and `/choose` commands work without a configured Chat model.

| To find | Resource names for `/find` |
| --- | --- |
| Work and execution | `tasks`, `workflows`, `nodes`, `agents`, `projects`, `executions`, `approvals` |
| Automation and extensions | `skills`, `triggers`, `review-routes`, `tools`, `plugins` |
| Configured connections | `scm`, `mcp`, `issue-connections` |
| Configured model providers and budget definitions | `models`, `budgets` |

## Open a console page from a conversation

Ask “Where can I inspect my model budget?” or “Where do I enter the MCP connection credentials?” Follow the link in the reply to the relevant console page. Enter credentials in its setup form. Use browser Back to return to the same conversation. Opening a page leaves decisions, saves and execution to the controls on that page.

When a connection needs browser OAuth, follow the Chat link to Connections and complete the normal provider consent. After **Complete OAuth connection**, use **Return to chat** to return to the originating message. Weft checks the conversation with the signed-in identity before offering the link.

## Follow a task

To open a specific task from Chat, ask “Me de o link para a task `tsk_…`”
with its exact ID. **Open task** takes you to the task’s Overview. Chat
checks that the ID exists before showing the link. It can do this without
reading the task’s execution state. If the previous reply identified one
task, you can refer to it by asking for its link in the same conversation.

Ask Chat to show the current state of a task or follow it until it settles.
Use a task ID or part of its title. For example, “Let me know when my
release notes review finishes.” If several tasks match, choose one in the
conversation to start watching it. Open Approvals to inspect held requests.

Start another chat while Weft follows the task. With a named static credential or shared local history, Weft also checks for results while the browser is closed. With OIDC, Weft reconciles task state on your next authenticated visit. The task continues in the execution engine in all three cases.
Recents shows the running indicator and signals an unread result, with a
green dot for success. Failure, cancellation and a required decision have
their own accessible status. Open the original conversation and follow the
task result link to its evidence. If the provider is down, the monitor
continues to read task state without calling that provider.

Completed watches and task inspections show the pinned base commit, checkout state and available delivery evidence for each repository. A confirmed PR receipt shows the PR number. **View task result** opens Changes & checks for that attempt. A retried task can also link to its previous attempt.

### Inspect workspace checkpoints

Ask for the latest checkpoints of a task ID or workflow ID. Chat reads the same Git checkpoint list as the workspace panel and reports the total and up to eight of the newest entries, including the node, tool and execution when recorded. A task uses its admitted execution copy. If the workspace has multiple repository aliases, name the checkout you want. Restoring a checkpoint remains a separate administrator action in the workspace panel.

## Review a pull request

Ask “Revise o PR 418.” Chat checks the PR on configured repository connections
that an existing saved workflow can read. The lookup uses PR metadata. Give a
PR link, `owner/repository` or the connection name to narrow the search. When
the same number appears in multiple repositories, choose the displayed number,
repository or a distinguishing title fragment. “In that repository” refers to
an explicit selection in the conversation. A new request by number searches
again. If some repositories could not be checked, Chat marks the search
incomplete and asks for a choice or narrower scope.

Chat resolves the reviewer after the repository. Choose a reviewer if several
qualify. The reviewer needs a running native event node, one exclusive ingress,
a single input per turn, an explicit model and a named repository read grant.
When a saved reviewer is stopped, Chat can prepare a separate **Start** review.
For a missing reviewer, ask for a reviewed Graph in Chat or inspect Workflows.
Connections whose grants require interactive read approval use the existing
manual path.

The preview identifies the repository, source and base commits, model and workflow version. **Inspect pinned diff** opens the bounded provider diff in the conversation. It checks the current saved read grant and both reviewed revisions, without starting analysis. If the revisions change or the bounded diff is unavailable, request a fresh review. **View workflow** opens the selected node. **Analyze PR** checks the revisions again and sends the bounded diff to the workflow model for one billed, tool-free analysis. Existing channel and turn gates still apply. The draft appears in the conversation with **View execution**. A missing receipt leaves the outcome unknown.

For a ready GitHub PR, select the draft comments to post and choose **Review selected comments**. Compare the file, changed line, comment text and source commit, then select **Publish**. The reviewer workflow version needs exact `scm.pull_request.read` and `scm.pull_request.comment` grants scoped to the selected connection. A grant that requires a separate tool approval cannot authorize this operation. If the source commit or base changes, analyze the new revision. For Bitbucket Cloud, inspect the pinned diff and read the draft in Chat. Inline publication is unavailable because its comment endpoint does not document a way to pin the write to the reviewed commit. Data Center also supports pinned inspection and draft analysis.

Chat shows a provider ID for each published comment and labels failures and unconfirmed results separately. **Reconcile comment receipts** checks the provider for an existing comment at the original anchor after a lost response. It does not send another comment. The conversation retains the selection, confirmation and receipts when you reopen it.

## Prepare a task environment

Ask to prepare the environment of a saved task by ID or unique name. An administrator can review the pinned workflow, task revision, repository commits and saved build definition before confirming. The session reports readiness when the native environment matches the reviewed definition. Use **Inspect task** for build diagnostics, existing preparations or approval holds. After readiness, ask to start the saved task. The start card identifies the prepared build, its completion time and task authorization. Confirmation rechecks readiness and admits the task once.

## Build a reusable workflow environment

As an operator, ask “Build the environment for the shared build workflow.”
Review the saved workflow version and definition before choosing **Prepare
environment**. Chat follows the build and reports readiness for the reviewed
definition. Starting the workflow takes a separate decision. Use **Inspect
workflow environment** for logs, repository-backed builds and reset controls.

## Correct a task draft

Identify the draft by its ID or unique title and specify the replacement title, request, or outcome. Compare the current and proposed text, then select **Save draft correction**. The draft keeps its workflow and repository bindings. When it uses a generated channel payload, the title and request update there as well. For a channel-bound draft, you can also ask to replace its complete JSON payload in a separate edit. Compare the channel, message type and both payloads before saving. The change preserves the other draft fields and repository bindings. Use **Tasks** for payloads outside the Chat size or content limits or incomplete bindings. A newer draft or workflow revision requires a fresh review.

To replace a local checkout, name the existing alias and the new registered project. Compare both project IDs, base commits and checks, then select **Save checkout replacement**. The other repositories and the custom input keep their saved values. If the task delivers through pull requests, include the new connection and base branch in the same message, for example `connection=docs-upstream branch=release/v2`. The card shows both PR destinations. The connection must match the replacement project’s Git origin. Confirmation saves the draft. Starting it and publishing a PR have separate controls. A changed project or connection requires a fresh review.

## Save a managed skill

An administrator can ask to create a skill with a name and instruction text, or revise an existing skill by ID. Compare the text and current audience on the review card, then choose **Save skill revision**. New skills start disabled. A revision preserves the skill’s activation, scope and description. **View skill** opens its saved content. To enable or disable a managed skill, add it to or remove it from installation defaults, or delete it, name the skill and requested change. Check the affected audience and confirm. A workflow reference blocks deletion. Previous revisions remain available. Built-in skills remain read-only.

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

Name the saved workflow and the registered projects to work with. Each selected project has a checkout alias. The draft card displays its project ID, revision and base commit. Weft checks the project revisions again before saving. Missing or ambiguous projects require clarification. Creating the draft leaves the task ready for a separate review and start.

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

Chat can prepare a saved task’s environment and separately start it against the ready build. Use Tasks for a checkout that still needs configuration or for external delivery. If the task or its authorization changes during review, inspect its updated scope and request a fresh proposal.

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
or turn request by approval ID or task name. When several requests are pending
on that task, choose one by its displayed number, ID or title. Weft checks
that your choice is still pending on the same task. For an agent question,
include the answer you want Weft to send.
Review the exact request, decision and any response text before confirming.
Open Approvals for the full request and for environment, delegation or tool
decisions. A recorded decision can unblock work. Follow the task or execution
to see its outcome.

## Increase a current budget

As an administrator, ask to raise the current allowance for a named task
attempt or a current month ledger. Give the new total and the ledger's
currency. Review the ledger, revisions, old and proposed limits and any
held runs before choosing **Increase current budget**. Chat shows the saved
limit and whether each affected run resumes, is still blocked or has an
unverified outcome. A recurring monthly cap or a default for future runs
requires its own installation configuration review.

If the administrator's Chat inference is already blocked by an exhausted
budget, raise the allowance in **Models → Budgets** and retry the saved
request. Chat inference uses the installation's normal budget admission.

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

To resize one agent, specify a fixed replica count or an autoscale ceiling.
Review the node, saved version, current count and proposed count. The fixed
count is a runtime override. An autoscale ceiling creates a workflow revision.
Setting a fixed count of zero drains the node and stops it. A positive count
on a stopped node applies when it next runs. Use **View node** to inspect the
policy and active replicas on the workflow canvas.
Chat shows the target, requested state and observed runtime state. An
ambiguous request such as “Deactivate the reviewer” needs a choice about
trigger intake, new turns, draining or immediate interruption.

If the workflow or run changed during review, open the workflow to inspect
its current state before confirming a fresh proposal.

## Pause or resume issue intake

As an administrator, name the trigger and specify future issue intake. For
example, ask “Pause future issue intake for Ready issues.” Review the
connection, issue selection, workflow and any pending admission before
choosing **Pause issue intake**. An admitted task continues through its own
lifecycle. A request to resume shows the fresh consent scope for the
selected trigger. **Open workflow triggers** opens its manual settings.

## Change instructions for one workflow

Name the workflow and node, then describe the replacement for that node’s **local instructions**. Review the old and proposed text and the pinned catalog version. **Save workflow instructions** creates a new workflow version. The reusable agent’s instructions and existing task copies stay on their recorded versions. Future turns use the normal workflow rollout. **View workflow** opens the selected node in the editor.

## Guide the next turn of a running agent

Specify the workflow ID, node ID and replica, or explicitly select any replica. Ask for one message on the next turn and choose **Send guidance** after checking the run and message. The node’s operator chat receives the message. A queued message can only be picked up by a matching replica in the selected run. Current turns continue with their existing input.

The card shows when the runtime picked up the message. The runtime can return it to the queue after an interrupted delivery. A pickup receipt establishes delivery to the runtime. Inspect the execution separately to determine what the agent did.

## Review installation configuration

As an administrator, ask for a concrete change such as “Set the task budget to two dollars with an eighty percent warning.” Review the object names, current revisions, field changes and any intake consent before choosing **Apply configuration**. A plan can include up to eight objects. The installation service validates the complete plan before applying its first change.

Use `/inspect projects` to find a registered project ID. Follow-ups can refer to the displayed order, such as “use the first project”. Ask for an explicit
environment preference such as “Set project `prj_…` to prefer sandbox for
new tasks.” To replace its checks, specify the intended list with each
command, workspace-relative directory and timeout as applicable. Review the
changes before applying. Chat uses the saved repository and resolves omitted
settings on the server. A preference guides future task setup. Checks remain
subject to each agent's tool permissions when a task uses them.

Each result names the object and its outcome. If a plan stops midway, inspect the applied objects, correct the failure and request a fresh plan. An unknown outcome requires inspection of the installation. Use the existing setup form or installation CLI to enter or rotate credentials. Chat can preserve existing credentials.

Administrators can type `/export` or ask to download the current installation configuration. **Download installation YAML** retrieves the export with the current credentials. The file uses the same secret-preservation markers as the CLI export. The conversation stores the download action and leaves the YAML out of its transcript and model context.

## Archive and access

In the conversation’s three-dot menu, select **Archive**, then **Confirm** to move it out of Recents. Close the menu to cancel. In
**Archived**, open it to view new results or restore it. Archiving keeps the
watch and read state. Active and archived conversations are retained without
automatic expiration. The Archived tab shows an unread count even while you
are browsing Recents. In both lists, a pending proposal contributes to the decision indicator only while your current role can decide it and the proposal remains valid. A task waiting for a decision also appears in its watch status. Use **Older conversations** to load the next page.

In an authenticated installation the owner can access their conversations.
Auth-disabled local installations show **Shared local history**. People using
that local installation can access the same conversations. Private history is
scoped to the authenticated static or OIDC principal. If your role changes, Chat filters historical operation details before displaying them or building model context.

When you switch chats before a reply arrives, Recents keeps that reply unread until you view it. A hollow dot indicates an unread reply and the green dot indicates a successful operational result. Both can appear alongside the running indicator.

Developers can run the [conversation evals](evals.md) to check tool selection, follow-ups and reviewed effects with repeatable fixtures.
