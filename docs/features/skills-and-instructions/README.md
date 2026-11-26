# Skills and instructions

**Configuration → Skills & instructions** (`#/skills`) separates **Skills**,
**Workflow associations** (`#/skills/associations`) and **Inspect instructions**
(`#/skills/inspect`). Skills are reusable Markdown guidance, not executable
plugins or a way to grant tools, credentials or permissions.

For conversational creation or revision of managed skill text, see [Save a managed skill](../chat/README.md#save-a-managed-skill).

## Find, create or edit guidance

1. Search the catalogue by name/description/ID and filter status and origin.
   Open a skill to `#/skills/s/<id>/content`; detail also exposes its workflow
   usage and revision history.
2. **New skill** opens `#/skills/new`. Enter stable ID, title, description and
   Markdown body. Write/Preview switches allow inspection before saving.
3. Review Enabled and installation-default choices. Installation defaults still
   require workflows to opt in; they are not unconditionally injected everywhere.
4. Save explicitly as administrator. UTF-8 bodies have a 16 KiB limit. Save
   creates an immutable revision rather than changing historical content.
   Invalid content/conflicts must be repaired before another save.
5. For a current custom skill, edit and save another revision. The editor tracks
   unsaved work; navigation/reload must not be treated as a save.

Built-in planner, implementer, reviewer and verifier skills are clearly marked
read-only. **Create editable copy** opens a new custom skill populated with its
guidance. Choose a new non-reserved ID and save. The copy is independent: it
does not inherit the original history or silently replace existing associations.
The `weft-` built-in namespace is reserved.

## Associate and inspect

1. Open Workflow associations, select a saved workflow and review its selected
   skills and installation-default participation.
2. Select workflow/agent associations and choose **Review associations**. The
   review names the workflow, expected version and changed sections. **Cancel**
   retains the unsaved selection. **Save associations** updates the graph using
   the reviewed version as a precondition and creates a new workflow version;
   failure retains edits and requires a new review. This does not start agents
   or change tool grants. Node selections narrow applicable
   guidance according to the instruction contract; installation defaults remain
   independently applicable when enabled.
3. Use Inspect instructions for the workflow/agent to inspect the effective
   composition. Repository guidance requires the relevant workspace/access
   context; selecting a skill does not authorize reading arbitrary files.
4. For completed work, inspect the execution's recorded instruction snapshot.
   Current saved guidance can differ from what an older execution received.

Composition precedence, bounded repository steering and content provenance are
defined in the [instruction guide](../../instructions.md). Guidance remains
untrusted task context and cannot override tool policy. A missing/disabled skill
or unavailable snapshot needs explicit correction or a successful reload; it is
not evidence that instructions were applied successfully.

## Implementation and verification map

- [SkillsView](../../../web/src/components/SkillsView.tsx),
  [SkillAssociations](../../../web/src/components/SkillAssociations.tsx),
  [skill navigation](../../../web/src/components/skillsNavigation.ts),
  [SkillsView tests](../../../web/src/components/SkillsView.test.tsx).
- [API routes](../../../server/internal/api/server.go) for skills and instruction
  inspection; [instructions](../../instructions.md) for composition/snapshots.
- Related journeys: [agents](../agents/README.md), [workflows](../workflows/README.md).
