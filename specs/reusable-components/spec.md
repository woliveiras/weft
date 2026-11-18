---
id: RELEASE-2
title: Reusable graph components
summary: Save, version, share and reuse agents and subgraphs in the visual editor.
status: in-progress
scope: [Release 2, graph components]
review_policy: Maintainer approved the installation agent catalog slice
navigation: [specs/README.md, specs/reusable-components/matrix.md, specs/reusable-components/evidence.md]
dependencies: [TASK-002, TASK-003, TASK-006]
---

# Reusable graph components

Allow users to reuse agents and subgraphs across graphs without manually
copying definitions or losing local configuration.

## Behavior and invariants

Component operations and sharing follow installation permissions. Insertion
and updates preserve existing local settings.

## Acceptance criteria

- **AC-001.** An authorized user saves an existing agent or subgraph and can
  list, read, update, delete and share the resulting versioned component.
- **AC-002.** A user inserts a compatible version in the visual editor, with
  validated parameters and defaults. Invalid parameters or incompatible
  versions surface a validation error without changing the graph.
- **AC-003.** A user can see the version in use, available versions and the
  proposed diff before updating an instance. A successful update preserves
  local configuration.

## Approved agent catalog slice

- Configuration → Agents presents a searchable grid of agent cards. Every
  saved workflow agent is available for reuse automatically, including agents
  created before the catalog existed. The system registers the effective
  definition of each previously unregistered inline agent as version 1.
  Existing workflow documents keep their configuration. Task execution copies
  are excluded. Viewers and operators can browse. Administrators publish
  immutable versions and save bindings using existing graph revision checks.
- Each agent card opens its shared settings. The Workflows tab follows Parameters
  and links to the agent configuration in each workflow that uses it, including
  its originating workflow while its original node remains inline.
- A binding identifies an agent ID and positive version. Workflow instances keep
  their node ID, channels, loop, replica settings, workspace, limits and
  approvals. The saved graph contains the resolved definition and effective
  parameter values. Changing the catalog does not change existing graph versions.
- Each workflow node can supply local instructions. The runtime renders the
  shared instructions first and then appends the node's local instructions.
  Local instructions can reference graph parameters and the node's instance
  parameters; instance values take precedence. Tool grants, runtime and model
  are owned by the shared definition for pinned nodes. Omitted catalog tools grant nothing. Read-only workspace in a
  definition is a minimum. All existing installation policy, tool and task
  consent checks apply to the materialized graph.
- Parameters use named strings in this slice, with optional defaults and
  required values. Workflow instances supply values for declared names. Inline
  graph parameters retain their existing behavior. An absent version or invalid
  binding fails with a field error and leaves the saved workflow untouched.
- Administrators can add an existing agent when creating a workflow or when
  editing its canvas. A catalog version must be selected explicitly. Origin
  links continue to locate the original inline node until that node is linked
  to a catalog definition.
- The pilot uses the code-review graph. Subgraph components, deletion and
  cross-installation sharing remain deferred.

## History

- 2026-10-19 delivery plan. Reusable components were outside Release 1.
- 2026-11-18. The maintainer approved the agent catalog, explicit version choice,
  workflow parameters, gradual migration and the code-review pilot.
- 2026-11-18. The maintainer approved automatic catalog registration, agent
  cards, workflow-local instructions and a Workflows tab in agent details.

## Reconciliation

- [Behavior and oracle matrix](./matrix.md).
- [Verification evidence](./evidence.md).
- [Console navigation](../../docs/interface-navigation.md).
