# Workflows

**Workflows** (`#/graphs`) lists saved reusable graphs. Search matches name,
description or ID; status filtering and client pagination operate on the loaded
set. Rows sort by most recently updated, then ID. Agent counts expand into
configuration links. Task-owned execution graphs do not appear as reusable
workflows.

## Create the definition

1. An administrator selects **New workflow**. The current creation experience
   is a modal containing a YAML/JSON definition, not a form wizard.
2. Paste a definition, upload YAML/JSON or adapt the supplied echo example.
   The example contains continuous/event echo agents; adapt their loop modes
   before intending to use the workflow as a task template.
3. Optionally add an existing agent from the catalogue picker. Choose its
   explicit version and parameter values. This adds a node to the document;
   it does not replace example agents or wire channels automatically.
4. **Validate** checks the document. **Create** also performs server validation,
   persists the first graph version and opens the canvas. It does not start it.
   Errors leave the document available for repair.

A workflow needs its receiving agents, channels, runtime/model choices, tools
and workspace/environment configuration to agree. Installation providers and
connections are separate prerequisites, not objects created by graph creation.

## Assemble and configure

The workflow workspace has **Graph**, **Agent configuration**, **Runs**,
**Activity**, **Files**, **Events**, **Messages**, **Definition** and **Settings**.
These share one graph draft; navigating between them does not save that draft.
Sessions are retained in memory while navigating the console, not across a
browser reload.

On the canvas:

1. **Add agent** opens a small creation drawer. Enter a name/description for a
   new node or choose an existing catalogue agent/version. Cancel/Escape closes
   without adding a node.
2. A new inline node joins the draft and can be completed in Agent
   configuration. A catalogue choice joins as a versioned binding. Both need
   graph save; new inline nodes become catalogue entries on saved registration.
3. Connect agents through channels and inspect the selected channel's delivery,
   endpoints and routing configuration. Channel contracts must match producers
   and consumers; adding agents alone does not connect them.
4. Inspect a node in its compact drawer. Overview, configuration, output,
   messages and runs are separate views. Full configuration lives on its
   dedicated page, rather than one long inspector form.
5. Contextual **Remove** previews affected configuration. Node/channel removal
   edits the draft; persistent removal happens only on graph save.

Agent configuration separates General, Instructions, Tools & permissions and
Advanced. Linked versions own shared fields; local instructions/parameter
values and instance settings belong to the workflow. See [agents](../agents/README.md).

## Configure workflow-wide settings

| Settings section | Responsibility |
| --- | --- |
| General | Stable ID, display name/description and message-chain hop limit |
| Workspace | Repository initialization and Git tracking for the workflow workspace |
| Environment | Discover/select a toolchain definition and inspect/build its image |
| Network | Environment network/hosts within the installation policy ceiling |
| Triggers | Independently saved issue-trigger resources and their consent |
| Parameters | Workflow prompt substitution values |
| Safety | Workflow-level execution safety settings |

Workspace initialization is distinct from selecting a project for a task.
Existing workspace contents are not replaced just because a clone URL changes.
Environment builds and trigger saves have their own operations; **Save graph**
does not imply either happened.

## Review and save

Form/canvas/Definition edits contribute to the same draft. Definition is the
advanced YAML view, with saved version history and export. Review-and-save
shows changed paths before persisting. Validation identifies affected fields;
the server remains authoritative even if client validation succeeded.

Saving uses the base graph version as a concurrency precondition and creates a
new saved version. A conflict keeps the draft and offers explicit recovery:
reload the latest definition and discard local work, or deliberately replace
the latest definition through the supported overwrite path. Neither selection
should occur silently. **Discard** is another explicit loss of local draft work.

Publishing an agent version does not update this graph automatically. Saving
this graph does not rewrite an already submitted task's snapshot. Running
replicas may not reflect the currently displayed unsaved draft.

## Execute or use as a task template

Operators/admins can control saved workflows. **Start / Run all**, **Stop all**,
**Pause** and **Resume** are runtime operations against saved configuration.
Unsaved changes require attention because execution uses saved settings. A
running event agent may be idle while waiting for a message. Immediate kill
has its own confirmation and interruption/redelivery implications.

Direct workflow execution is useful for message-driven services and loops.
Starting a [task](../tasks/README.md) is different: the task records a request,
reviews admission/authorization and creates a separate task-owned graph.
Continuous/scheduled agents are not automatically valid task entry points.

Administrative workflow deletion is separate from stopping execution and from
resource cleanup. It does not remove the reusable catalogue entries that were
registered from that workflow. Do not infer that deleting a template rolls back
remote changes or removes task history.

## Implementation and verification map

- [GraphList](../../../web/src/components/GraphList.tsx): table, creation
  document, catalogue insertion, lifecycle controls and deletion entry point.
- [GraphWorkspace](../../../web/src/components/GraphWorkspace.tsx),
  [GraphCanvas](../../../web/src/components/canvas/GraphCanvas.tsx),
  [GraphSettings](../../../web/src/components/GraphSettings.tsx) and
  [AgentConfiguration](../../../web/src/components/AgentConfiguration.tsx):
  shared draft, navigation, canvas and configuration.
- [API server](../../../server/internal/api/server.go): graph create/update,
  validation, version reads and runtime-action permissions.
- [GraphWorkspace tests](../../../web/src/components/GraphWorkspace.test.tsx),
  [GraphList tests](../../../web/src/components/GraphList.test.tsx) and
  [canvas tests](../../../web/src/components/canvas/GraphCanvas.test.tsx).

Owning references: [console navigation](../../interface-navigation.md),
[execution model](../../execution-model.md), [graph schema](../../../schemas/graph.schema.json).
