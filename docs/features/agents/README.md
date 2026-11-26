# Agents

In Chat, specify whether a change should guide a single running turn, edit a node in one workflow, or publish a reusable agent version. Publishing and adopting that version on a workflow are separate reviews. [Chat guide](../chat/README.md).

The catalogue at `#/agents` lists reusable agents as cards: icon, name, short
description and latest published version. Search includes name, ID, description
and origin workflow. Open a card for that agent's configuration; **New agent**
opens `#/agents/new`. Publishing requires an administrator.

## Create and publish

1. Choose a stable agent ID, name and description. IDs use lowercase letters,
   digits and hyphens and are limited to 64 characters. IDs are references, not
   editable display names after publication.
2. In **General**, select runtime, provider and model. A new catalogue agent
   starts with native runtime and Docker isolation defaults. External runtimes
   have their own endpoint/command/image requirements.
3. In **Instructions**, write shared guidance. This text applies wherever the
   selected version is used; workflow-specific additions belong in the workflow.
4. In **Tools & permissions**, configure isolation, workspace restrictions and
   explicit built-in/MCP grants. MCP discovery comes from installation
   connections; discovering a tool does not grant it.
5. In **Parameters**, declare named inputs, descriptions, defaults and required
   flags. Instructions can reference them as `@name`. Workflows provide values
   for each binding. Unknown or missing required values are validation errors.
6. Use the upper-right **Publish** action. Changes across all tabs belong to one
   local edit session; changing a field does not publish a version. The dirty
   indicator identifies unpublished changes.

Publication validates the complete definition and creates an immutable version.
An update uses the latest-version precondition; a concurrent publication can
fail instead of overwriting another version. Failed publication keeps the form
available for correction. Publishing does not start an agent or change workflow
bindings automatically.

The latest version is editable for an administrator; historical versions are
read-only. The version selector changes the viewed definition. This editor has
local state across tabs. Changing the viewed version or navigating away while
changes are unpublished asks whether to discard them; cancelling stays in the
editor. Browser reload/close uses the native unsaved-changes warning. Pending
publication blocks navigation and version changes. This protects the current
session; unpublished content is not a server-side draft or durable backup.

## Agents created inside a workflow

**Graph → Add agent** can create a new inline agent or select a catalogue agent.
New inline agents join the graph draft first. Saving a reusable workflow
automatically registers previously unregistered inline nodes in the catalogue,
using stable generated IDs and origin workflow/node metadata. Task execution
copies are excluded. Existing workflows are backfilled without rewriting their
graph definitions. There is no **Save as reusable** step.

There is an important current distinction: automatic registration snapshots the
origin node into the catalogue, but does not replace that origin node with a
catalogue binding. Later inline edits do not automatically publish catalogue
versions. Use the explicit linking path to adopt a shared agent version. The
catalogue entry survives changes/removal of its origin workflow.

## Reuse and customize per workflow

1. In the Add agent catalogue picker, search for the agent and select a specific
   published version. Review its definition and access requirements and provide
   the binding's parameter values.
2. Add it to the workflow draft. This does not persist the graph or start work.
3. Open **Agent configuration** for the node. The panel identifies the shared pinned definition separately from **This
   workflow** settings. Shared version fields are shown read-only. Workflow-local instructions, parameters and instance execution
   settings control this use of the agent.
4. Review and save the complete graph draft. The node records the selected agent
   ID/version and its local values.

The shared instructions are combined with local workflow instructions. Local
instructions do not grant tools or bypass installation policy. Two workflows can
use the same version with different parameters/instructions without modifying
each other.

For an already linked node, **Agent version** is a dropdown for that agent, with
the in-use version selected. Selecting a different version opens an inline comparison before changing the
binding. Expand changed sections to inspect current and proposed shared values,
then choose **Use this version** or **Cancel version change**. Adoption changes
the workflow draft only and still requires graph save. Values for parameters still declared are retained;
obsolete parameters are dropped and new defaults participate in resolution.
Read-only workspace restrictions are not silently relaxed by adoption. Validation
can still reject a newly required parameter or incompatible configuration.

## Find usages and recover

The agent detail **Workflows** tab lists current known bindings and origin-node
uses, with links to each node's configuration inside its workflow. It is not a
complete historical execution inventory or proof that running replicas use the
latest version. The card deliberately omits workflow names because one agent
can have many uses.

Read-only roles can inspect definitions; publishing and workflow changes need
the corresponding administrative permissions. Missing catalogue versions,
failed loading, invalid grants and publication conflicts are surfaced rather
than resolved by choosing another agent/version. No agent-delete console/API
flow is exposed by the current catalogue routes.

## Implementation and verification map

- [AgentsPage](../../../web/src/components/AgentsPage.tsx): cards, versioned
  editing, parameters, publication and usages.
- [Agent journey model](../../../web/src/components/agentJourneyModel.ts) and
  [journey tests](../../../web/src/components/AgentJourneys.test.tsx): draft
  guards, publication recovery and reviewed adoption.
- [AgentBinding](../../../web/src/components/AgentBinding.tsx): catalogue
  selection, binding parameters and version adoption.
- [AgentConfiguration](../../../web/src/components/AgentConfiguration.tsx):
  workflow-local configuration and execution boundaries.
- [Catalogue manager](../../../server/internal/agentcatalog/catalog.go) and
  [workflow registration](../../../server/internal/agentcatalog/workflows.go):
  immutable versions, validation and automatic reusable snapshots.
- [Agent API](../../../server/internal/api/agents.go),
  [binding tests](../../../web/src/components/GraphWorkspace.test.tsx) and
  [catalogue package tests](../../../server/internal/agentcatalog).

Continue with [workflows](../workflows/README.md),
[instructions](../../instructions.md) and [tool permissions](../../tool-permissions.md).
