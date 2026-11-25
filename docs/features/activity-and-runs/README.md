# Activity, runs and evidence inspection

This journey answers what is running, what needs a decision and what actually
happened. It does not start work merely because a record is opened.

## Installation overview

1. Open **Work → Activity** (`#/activity`) and choose the time window.
2. Inspect running/waiting/failed/finished indicators and **Needs you**. Decisions
   are made at the linked review surface; a dashboard count is not a percentage
   of task completion.
3. Inspect running workflows and each agent's state/current step, then follow
   links to its panel or saved workflow.
4. Filter recent history by status/workflow/agent. Recent execution records are
   bounded and windowed; an empty window is not proof that work never ran.
5. Use Health for installation diagnostics and Refresh for a new snapshot. Event
   stream updates and refresh failures are different from agent execution errors.

## Workflow execution investigation

Open the saved workflow and choose the relevant section:

| Section | Inspection path |
| --- | --- |
| Runs | Search/filter execution records, newest first, open a run and inspect result/tool calls |
| Activity | Choose execution scope, then Overview, Approvals, Messages or Events instead of one unbounded page |
| Messages | Select a recent message card to trace its workflow chain; payload/metadata disclosures remain independently operable |
| Events | Filter type/node and control autoscroll/pause for the viewer |
| Files | Select the relevant workspace and inspect files, Git changes and evidence |

A run is an agent execution record, not necessarily an entire workflow or task.
Multiple turns/agents/replicas can contribute to a task. Summaries and completed
plan steps are agent-generated claims; tool/check records establish observed
outcomes. Token/cost fields only describe their recorded scope.

Message traces show relationships between messages and deliveries; retries and
acknowledgments are not equivalent to business success. Autoscroll/Pause in an
event viewer affects following output, not runtime pause. Grafana links open a
separate observability system whose availability/retention can differ.

## Task-owned evidence

Use the task detail's Overview for prominent **Agent plans** and the result;
use **Environment & logs** for build progress and **Changes & checks** for
verification. Activity can narrow linked executions/messages/events. These are
separate scopes, so task success need not mean all recorded calls succeeded.

Legacy task execution graph links route to the task-owned inspection page.
They must not expose a reusable graph editor or fail merely because task copies
are excluded from the normal workflow catalogue. Checkpoint restore, stopping
work and cleanup are explicit mutations beyond inspection and retain their
own permissions/confirmations.

Load failures must be distinguished from empty results. Preserve the last valid
snapshot as stale when supported, retry the failed read and follow the task's
refresh feedback. Do not interpret stale status as a fresh remote PR/Git check.

## Implementation and verification map

- [ActivityPage](../../../web/src/components/ActivityPage.tsx),
  [RunsView](../../../web/src/components/RunsView.tsx),
  [TaskActivity](../../../web/src/components/TaskActivity.tsx).
- [MessagesExplorer](../../../web/src/components/MessagesExplorer.tsx),
  [EventsView](../../../web/src/components/EventsView.tsx),
  [FilesView](../../../web/src/components/FilesView.tsx).
- [TaskExecutionPage](../../../web/src/components/TaskExecutionPage.tsx),
  [TaskDetails](../../../web/src/components/TaskDetails.tsx),
  [activity tests](../../../web/src/components/TaskActivity.test.tsx).
- [Navigation guide](../../interface-navigation.md), [events](../../events.md),
  [operations](../../operations.md), [tasks](../tasks/README.md).
