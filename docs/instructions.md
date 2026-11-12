# Skills and project steering

Open **Skills & steering** in the console. Administrators can create Markdown
skills, edit them into new revisions, toggle enabled/default flags, inspect
history and delete them logically. Viewers can inspect content; operators can
run graphs but cannot edit skills or graph associations. These are installation
resources, visible to the existing installation roles, not private user records.
Do not put credentials or other secrets in guidance.

Each skill has a stable DNS-style `id`, `title`, `description`, `body`, `enabled`
and `global` flag. It is inert UTF-8 Markdown: frontmatter, fenced commands,
links and requests to grant permissions remain text. Nothing is executed or
fetched. This release does not support script bundles, MCP or delegation.

## Built-in role skills

Weft ships four read-only skills for coding workflows, listed with
`builtin: true` and selected by ID like any other skill:

| ID | Role |
|---|---|
| `weft-planner` | Read-only: map the code (`code.map`, `filesystem.search`, `code.outline`), write a plan with `plan.update`, name the checks |
| `weft-implementer` | Edit with the snippet and patch tools, verify with `check.run`, keep the plan current, do not stop before the checks pass |
| `weft-reviewer` | Read-only review of the diff against the request, with file and line findings |
| `weft-verifier` | Run the declared checks and report their real results |

IDs starting with `weft-` are reserved: creating, editing or deleting them
returns 422. Built-in skills are never installation defaults. Like all
guidance they are untrusted text: the tools they mention work only when the
agent is granted them.

## Association and precedence

Save graph/agent associations on the same screen. The whole graph is updated
with its version in `If-Match`; concurrent changes return a conflict and keep
the local edits. Inspection explicitly uses the **saved** configuration.

```yaml
spec:
  workspace: {path: example-project}
  instructions:
    installationDefaults: true
    skillIds: [review, conventions]
    projectSteering: true
  nodes:
    - id: writer
      skillIds: [conventions]
      prompt: Complete the assigned work
```

Existing workspace policy selects the graph directory or replica worktree.
`projectSteering: true` explicitly authorizes instruction reads from that
workspace. It does not authorize additional paths. The inspection API supports
`node` and `replica`; the console previews replica zero. Execution snapshots
always reflect the actual replica directory, including worktree contents.

Legacy composition order, from lower to higher guidance precedence:

1. Installation defaults (`global: true`), in ID order, only when
   `installationDefaults: true`.
2. Graph skill IDs, in declared order. A node's `skillIds` may narrow this list;
   omitted/null inherits, an explicit empty array selects none. Node narrowing
   does not remove explicitly enabled installation defaults.
3. Immediate `.weft/steering/*.md` files in bytewise filename order.
4. The agent prompt after parameter substitution.
5. Current operator messages, in the existing delivery order (task scope).

Later guidance wins for semantic conflicts; no attempt is made to interpret or
merge natural-language statements. Platform policies, tool grants, approval
requirements, limits and credentials are independent of this ordering.
Channel input payloads remain data under the existing runtime protocol.

Omitting `spec.instructions` preserves the original prompt and operator
messages byte-for-byte, including legacy size behavior. A new
`installationDefaults` opt-in makes the existing contract's “no application
skills when omitted” compatible with installation-wide defaults.

## Repository guidance

In **Skills & steering**, an administrator can enable **Discover repository
instructions and skills** for a saved graph. It starts disabled for new and
existing graphs. A node can inherit the graph policy, disable discovery for
itself, or add source blocks. The graph policy is the upper bound. This
setting is independent of **Authorize project steering**.

```yaml
spec:
  instructions:
    repositoryGuidance:
      blockedSources: [github]
    projectSteering: true
  nodes:
    - id: writer
      repositoryGuidance:
        blockedSources: [claude]
```

An empty `repositoryGuidance: {}` on the graph opts in to all supported
families. Setting `disabled: true` disables discovery. Omission on the node
inherits the graph policy. Families are `agents`, `claude` and `github`.

Discovery uses the selected clone or replica worktree root, even when the
agent works in a configured subdirectory. It selects the first present,
unblocked instruction in the order `AGENTS.md`, `CLAUDE.md`,
`.claude/CLAUDE.md`, and `.github/copilot-instructions.md`. An invalid
selected file is recorded as an omission. Discovery lists immediate
`SKILL.md` packages under `.agents/skills`, `.claude/skills` and
`.github/skills`. It has no recursive discovery, imports, executable hooks
or remote fetches.

Relative package-directory aliases are allowed between supported skill roots
in the same checkout. Both the alias and canonical source must be allowed,
and a canonical package is catalogued once. Files, references and legacy
steering still reject symlinks. A console skill shadows a matching repository
name when the graph or agent selects its ID and its catalogue entry fits.
Installation defaults alone leave matching repository skills eligible. If
the selected console body exceeds the remaining budget on activation, it
releases its active catalogue slot. Weft offers the matching repository
catalogue entry if it fits, and the agent can explicitly activate it.
Activity retains the original shadowing and records the activation refusal
and the fallback outcome. Distinct repository packages sharing a name
prefer `.agents`, `.claude`, then `.github`, with canonical relative
paths in bytewise order to break ties. A v2 `skills-lock.json`
`skills.<name>.computedHash` is an advisory version declaration. Observed
SHA-256 digests describe the bytes Weft used.

In enabled executions, the order from lowest to highest is repository
skills, the selected repository instruction, installation defaults,
graph/agent console skills, legacy `.weft/steering`, the agent prompt, and
operator messages. A global skill explicitly selected by the graph or agent
takes the selected rank once. The initial model request has skill names, descriptions and
selection IDs. The native runtime can request a skill with
`guidance.load {"skill":"repository:example"}` or
`guidance.load {"skill":"console:example"}`. A later request can add a
package-relative textual reference to an already activated repository skill.
Only the chosen content is added, in the original skill's precedence slot.
The prompt preamble identifies checkout sources. Each source is framed with
a header and closing line. Every payload line is quoted, with control line
separators escaped, so source text cannot introduce a header, closer or task.
Reference reads use the existing `filesystem.read` gateway and its path
constraints, rate limits and approvals. Discovery and Activity identify the
reference from the checkout root. Authorization confines the reference to the
agent's working directory, including any `workspace.subdir`, and matches
path grants relative to that directory. The tool metric and span identify them as `guidance.reference`,
while grants and approvals use `filesystem.read`. The
`weft_guidance_load_duration_seconds` histogram counts and times progressive
catalogue and body loads by stage and outcome. Their spans are
`guidance.load.catalogue` and `guidance.load.body`. These measurements carry
no file paths or guidance text. The loader rechecks the live checkout identity and metadata.
A detected mismatch requires a later execution to rediscover the skill.
Runtimes without progressive support fail before dispatch. Graphs without
effective discovery retain legacy delivery for console skills.

Each body, root instruction or reference is limited to 16 KiB and package
metadata to 2 KiB. Discovery accepts at most 128 candidates per family and
128 active catalogue slots per execution. An over-limit family is omitted as a
whole. Each execution can request up to 16 distinct package references.
A request refused by the turn's tool call limit appears in Activity as
`tool_call_limit`. The shared
8,192-byte guidance budget reserves the mandatory prompt and operator
messages first, then admits whole optional entries from higher to lower
precedence. Activations spend the balance including framing. Retries of
applied reads spend no additional guidance budget. This is a conservative
context bound, while provider accounting still includes the full model
request each time it is sent.

## Bounds and rejection

- Managed bodies and repository files: 16 KiB each, UTF-8, no NUL, nonblank.
  Titles/descriptions have 200/1000-byte limits.
- At most 128 graph skill references and 128 immediate repository entries.
  If the directory exceeds this cap, the whole project set is omitted with
  `file_count_limit`; filesystem enumeration order never picks a random subset.
  Installation defaults load at most 128 IDs; further defaults are reported
  by an aggregate `entry_count_limit` omission.
- Each skill retains at most 128 revisions; further updates fail explicitly.
  Existing records remain readable. A future archival API can extend this
  bound; there is no silent pruning.
- Opted-in guidance has an 8192-byte conservative token budget (one UTF-8 byte
  charged per possible token, including guidance framing). This is not an
  exact model tokenizer or the full model context window. The existing model
  gateway still checks the full request, including tools/messages/system text.
- Agent and operator task text reserve budget **before** optional guidance.
  If mandatory content exceeds the budget, execution fails before the runtime
  is called, records the reason, and restores consumed operator messages.
  Otherwise oversized optional entries are omitted whole, never truncated.
- Duplicate skill references are graph validation errors. Duplicate IDs across
  defaults/graph selections load once and record `duplicate`. Identical project
  bodies load once and record `duplicate_content` on later files.
- Missing/disabled/deleted selected skills are recorded as omissions. Corrupt
  settings or failed storage reads/writes fail execution, never masquerade as
  an empty successful load.
- Missing directories, unreadable entries, invalid text, unsupported extensions,
  symlinks and special files produce explicit omission reasons in legacy
  steering. Steering searches only its immediate directory. Directory/file
  symlinks encountered there are rejected. Reads open each component
  relative to a pinned directory without following symlinks. Nonblocking
  opens prevent FIFO substitution from hanging a turn. Hard links are ordinary workspace
  files; deployments must prevent untrusted creation of cross-boundary hard
  links at the filesystem/sandbox layer.

## Persistence, API and audit

`GET/POST /api/v1/skills` and `GET/PUT/DELETE /api/v1/skills/{sid}` expose the
registry. Writes require admin; PUT/DELETE require a positive `If-Match`.
Creation requires a fresh ID. Reads include `history`; individual reads/writes
return an ETag. Invalid fields return 422, malformed JSON/headers return 400,
and stale versions return 409. DELETE creates a disabled tombstone rather
than removing data; PUT can restore it in a new revision. Deletion is refused
while any latest or active graph version references the skill, a conservative
superset of the original active-graph guard. Concurrent graph edits and skill
updates are not a cross-resource transaction: snapshots always record what was
actually resolved, and a disabled/missing skill is transparently omitted.

Storage uses existing settings CAS, one `instructions/skills/{id}` document
with current revision and retained history. The successful atomic write stores
actor/time/revision together; this is the authoritative mutation audit even if
live event delivery fails. `instruction.skill.updated` carries only ID,
revision, actor and deletion state, never the body.

Before runtime invocation the engine creates
`instructions/executions/{executionId}` exactly once (CAS revision zero).
The immutable snapshot contains graph/node/execution IDs and entries with
scope, source, revision for managed skills, SHA-256 and any omission reason.
Legacy snapshots also store included bodies. Oversized repository content is
not hashed from a truncated prefix.
Execution IDs link the snapshot to graph version, run, replica and trace in the
existing execution record. No storage adapter/schema migration was necessary.

`GET /api/v1/graphs/{id}/instructions?node=...&replica=0` resolves current saved
configuration; it is not historical evidence. `GET
/api/v1/executions/{eid}/instructions` returns the immutable snapshot. Legacy
executions or turns that failed before context preparation return 404, rather
than inventing a snapshot. The console offers both views. From **Activity**, open an execution and
choose **Guidance** to see the historical order, catalogue, actual reads
and omissions. The existing installation-wide Viewer role applies. For
progressive runs the snapshot and immutable read outcomes record validated
relative paths, console revisions, catalogue and file SHA-256 digests,
bounded descriptions, and delivery state. Prepared, confirmed, interrupted
and indeterminate states distinguish reading from dispatch. The record
persists for the lifetime of the execution. New entries omit body and
reference copies. Legacy snapshots remain readable with their original
contents. Failed audit persistence prevents unrecorded guidance from
reaching the model.

Runtime Protocol v1 carries optional `instructions` as inert manifest data;
`prompt` contains composed optional guidance followed by the original agent
prompt, and operator messages remain in their existing fields/system context.
The Python SDK exposes `TurnInput.instructions`. Models/runtimes do not receive
new capabilities, environment variables or credentials through this mechanism.
`execution.instructions.loaded` reports counts and remaining budget with graph,
node and execution correlation; normal execution failure events record load
failures. Content is not logged by the new instruction loader.

## Verification

Use local Go (`make test GO="go -C server"`) or the documented container
wrapper. Targeted packages are `internal/instructions`, `internal/graph`,
`internal/api`, `internal/engine` and `internal/runtime/native`; Python tests include an HTTP runtime round
trip. `SkillsView.test.tsx` tests administration, conflict retention, roles and
inspection. The Chromium journey is included in `make test-e2e`, using the disposable
controller and synthetic tools without the shared Docker daemon. To run only
this journey, use `cd web && pnpm exec playwright test --config playwright.instructions.config.ts`.
The shared configuration defaults to loopback port 18089; `WEFT_E2E_PORT`
selects another validated port for parallel sessions.
