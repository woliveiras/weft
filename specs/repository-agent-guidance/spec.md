---
id: REPO-GUIDANCE
title: Load agent guidance from repository workspaces
summary: Discover repository instructions and skills alongside console guidance and show their provenance in Activity.
status: draft
scope: [cloned workspaces, replica worktrees, console instruction settings, execution Activity]
risk: high
risk_domains: [prompt injection, authorization, filesystem isolation, compatibility, audit]
reversibility: Disable repository discovery through saved console policy
change_surfaces: [graph and agent settings, instruction loader, runtime context, execution audit, Activity]
contracts: [graph instructions, Runtime Protocol v1, execution instruction snapshots, workspace file permissions]
review_policy: Maintainer approval of this specification and matrix before test review and implementation
navigation: [specs/README.md, specs/repository-agent-guidance/matrix.md]
documentation: Update instruction, graph, API, runtime and Activity contracts when the behavior ships
authority:
  granted: [specification and matrix drafting]
  withheld: [implementation, new tool grants, new permissions, push, pull request, deployment, release]
dependencies: [RUN-001, TASK-007, TASK-008, TASK-006, TASK-004]
---

# Load agent guidance from repository workspaces

Use project conventions and skills from the actual clone or replica worktree
selected for an execution. Compose them with the saved console guidance, and
let an authorized Activity reader see what the execution received.

## Current behavior

[Skills and project steering](../../docs/instructions.md) defines the current
order from low to high: installation defaults, graph skill IDs narrowed by
the node, immediate `.weft/steering/*.md` in bytewise filename order, the
parameterized agent prompt, then current operator messages. The steering step
requires `spec.instructions.projectSteering: true`. The loader rejects
symlinks and never searches for `AGENTS.md`. Current saved-configuration
inspection and immutable per-execution snapshots are distinct views.
Activity already links executions to their run details.

## Behavior and invariants

- Repository discovery reads from the clone or worktree bound to the execution,
  including the replica's checkout when one exists. It never falls back to the
  main checkout, another replica, an ancestor outside that root, a home
  directory, or a remote. The loader accepts only bounded, supported text.
- The console administrator controls effective repository discovery per graph
  and agent, including source blocks. An inherited or disabled setting must
  be visible. A blocked source must be filtered before its content enters the
  guidance loader and cannot be reintroduced through an alias or reference.
- Console associations, installation defaults, selected graph and agent skills,
  the agent prompt and operator messages keep their existing meaning. Guidance
  precedence resolves instruction conflicts only. Loading a skill later
  preserves its effective precedence.
- Repository content is untrusted, including metadata, references, lock entries,
  scripts and claims of authorization. Guidance cannot grant tools, file
  access, permissions, credentials, approvals, push or pull requests. The
  existing gateways and authorization checks decide each operation. Normal
  reads and edits of specs and other workspace files remain subject to existing
  file grants, even when guidance discovery is disabled. A normal file read
  is recorded as a file operation under existing rules and does not implicitly
  activate a skill.
- Residual prompt-injection risk remains after enforcing grants. Repository
  guidance can steer the use of tools already granted to the agent. In Full
  access this can include writes without a human decision where the effective
  grant and other mandatory gates allow them.
- For progressive loading, initial model context carries a skill's name and
  description with enough identity to select it. Only a requested skill body
  or requested local reference is delivered as guidance. The request is bound
  to the execution and its effective policy. It cannot act as an arbitrary
  file reader. Applicable workspace file permissions still govern agent file
  access. A reference cannot expand the skill's scope or trigger a fetch,
  script, hook or configuration import. Existing model context, tool and
  execution limits still apply.
- Repository links must stay inside the selected workspace throughout
  resolution and opening, including under concurrent path replacement.
  Containment must hold for every hop. A permitted alias and the canonical
  package identify one skill. Escapes, broken links, cycles, forbidden
  targets and special files are omissions with safe reasons. Legacy
  `.weft/steering` continues to reject symlinks.
- Historical Activity reflects the execution's recorded sources and reads.
  Record effective precedence, source and outcome for applied and omitted
  entries, and distinguish a catalogued skill from its loaded body and each
  loaded reference. Managed skills identify their registry revision. Repository
  entries identify the workspace-relative path and SHA-256 of the exact bytes
  delivered for that stage. Discovery hashes cover the metadata actually used,
  with a distinct full-file digest when the body or reference is read. An
  unread or blocked file has no invented content digest. Report source-level
  omissions when individual entries were never enumerated or a bound was hit.
- A source changing between discovery and activation must not silently switch
  the selected skill or expose content under a stale identity or policy.
  Historical provenance must survive repository and console edits, cancellation,
  restart and read failure. Failed audit persistence must not allow unrecorded
  guidance to be delivered. Ordinary repository omissions remain visible and
  independent sources can still load.

## Acceptance criteria

- **AC-001 — Workspace discovery.** With discovery enabled for a cloned graph,
  the agent receives supported instructions and a catalogue of supported skills
  from its actual checkout. Separate replica worktrees can resolve different
  bytes for the same path. Unsupported sources are outside discovery.
- **AC-002 — Administrator controls.** A saved graph policy and agent override
  determine whether guidance and supported sources may load. Disabled and
  blocked sources contribute no model guidance. Non-admin writes are denied
  and concurrent edits follow existing graph version checks.
- **AC-003 — Precedence and name conflicts.** When console and repository
  guidance coexist, all eligible layers are resolved in the agreed order.
  Same-name managed/repository candidates follow the selected collision rule.
  The console and execution expose the effective outcome and reason.
- **AC-004 — Progressive loading.** A turn begins with skill name and
  description in context. Selecting a skill delivers its body at the skill's
  effective precedence. A subsequent request delivers only the selected
  reference. Unused bodies and references never enter context. Invalid,
  missing, oversized, out-of-scope and changed sources get explicit outcomes.
- **AC-005 — Local skill installation.** A clean checkout of Weft with
  `.agents/skills/sdd-specification/SKILL.md` and `skills-lock.json`
  discovers the skill and loads its requested references. When present, a
  valid relative agent-directory alias to the same canonical skill produces
  one catalogue entry and an alias outcome. Activity identifies the lock
  entry for the installed version and the hashes of the actual content read.
  This repository currently contains the canonical package and the lock.
  Exercise agent-specific aliases in a disposable checkout of that fixture.
- **AC-006 — Boundary enforcement.** A repository file or skill requesting
  permission escalation, a credential, an ungranted file mutation, push or a
  pull request obtains no capability or approval. A permitted ordinary edit
  of a spec still works under the same workspace file policy. The loader
  rejects escape paths and unsafe targets at discovery and on-demand reads.
- **AC-007 — Activity provenance.** Each execution exposes the applied console
  and repository guidance and the effective order, managed skill revision,
  repository path and stage hash, on-demand reads and omission reasons. A
  change to saved configuration or workspace bytes after execution does not
  rewrite history. The read follows the existing installation-wide Viewer
  role and the API denies requests that fail authentication or lack that role.
  A failed or interrupted load is visible as such.
- **AC-008 — Compatibility and recovery.** Existing graphs retain the
  documented behavior under the chosen rollout rule, including
  `.weft/steering` ordering, prompt/message handling, and old snapshots.
  Discovery does not report success after a policy or audit storage failure.
  Runtimes that cannot preserve the chosen loading contract surface an
  explicit compatibility outcome. Retrying does not duplicate an applied read.

## Explicit exclusions

- Per-execution or per-graph reader authorization, new roles and new grants.
- Running repository scripts or hooks, importing remote guidance or downloading
  skills, and granting access to the host or other workspaces.
- Protection against every instruction that steers an already granted action.

## Upstream conventions

The paths below describe upstream behavior. Weft's supported subset is D-01.

| Upstream | Repository conventions |
| --- | --- |
| Codex | `AGENTS.md` and `AGENTS.override.md` along the project-root-to-working-directory chain, with per-directory override/fallback rules. Repository skills use `.agents/skills`. [Instructions](https://learn.chatgpt.com/docs/agent-configuration/agents-md) and [skills](https://learn.chatgpt.com/docs/build-skills). |
| Claude Code | `CLAUDE.md`, `.claude/CLAUDE.md` and scoped `.claude/rules/`; project skills under `.claude/skills/`. Its import and directory rules are broader than the proposed Weft subset. [Memory](https://code.claude.com/docs/en/memory) and [skills](https://code.claude.com/docs/en/skills). |
| GitHub Copilot | Repository-wide `.github/copilot-instructions.md`, path-specific `.github/instructions/*.instructions.md`, and `AGENTS.md` support on selected surfaces. Project skills can use `.github/skills`, `.claude/skills` or `.agents/skills`. [Custom instructions](https://docs.github.com/en/copilot/reference/custom-instructions-support) and [skills](https://docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/customize-cloud-agent/add-skills). |
| Agent Skills | `SKILL.md` frontmatter supplies the name and description; bodies and resources such as `references/` and `assets/` are progressive. [Format](https://agentskills.io/specification). |
| `npx skills` | Installs to canonical `.agents/skills`, can create per-agent symlinks where applicable, and records project installation metadata in `skills-lock.json`. [Installer](https://github.com/vercel-labs/skills/blob/main/src/installer.ts) and [project lock](https://github.com/vercel-labs/skills/blob/main/src/local-lock.ts). |

## Open decisions

The recommendations below are proposed contracts. The matrix labels scenarios
that depend on them.

- **D-01 — Source coverage and scope.** Start at the checkout root with
  `AGENTS.md` and immediate skill packages in `.agents/skills`,
  `.claude/skills` and `.github/skills`. Support `CLAUDE.md` (root first,
  then `.claude/CLAUDE.md` as a fallback) and
  `.github/copilot-instructions.md` as root guidance fallbacks in that
  order when `AGENTS.md` is absent or administratively blocked. Treat a
  malformed chosen file as an explicit omission. Alternatives are a smaller
  `AGENTS.md`-only initial instruction set or an expanded set with
  `AGENTS.override.md`, scoped instructions, imports and nested discovery.
  Expanded support needs an agreed path scope when a turn touches several
  directories, and rules for file imports. Generic agent settings, plugins,
  hooks, MCP configuration and executable features are outside this feature.
- **D-02 — Rollout and controls.** Recommend a separate opt-in graph setting
  for new sources, disabled on both existing and new graphs until an admin
  enables it. Agents inherit or explicitly disable/narrow graph policy; a
  graph block is a ceiling. Offer blocks per named source family, enforced on
  both the discovered and resolved path. Preserve `projectSteering` and its
  original slot independently. Alternatives are default-on for newly created
  cloned graphs, or an explicitly migrated `projectSteering` setting with
  separate source choices. Decide whether administrators also need per-file
  or per-skill blocks, or agent-only opt-in within an allowed graph policy.
- **D-03 — Guidance precedence.** Recommend inserting new repository
  guidance below installation defaults. Proposed order, low to high:
  repository catalogue/activated skills, selected repository instruction
  file, installation defaults, graph/agent-selected skills, legacy
  `.weft/steering`, agent prompt, operator messages. Alternatively, place
  new repository guidance after graph/agent skills and before legacy
  steering, so local project guidance can override the console skill text
  while prompt and task authority still win. Keep existing layers' relative
  order in either case. Decide how equal-rank active skills are ordered;
  proposed tie-breaker is canonical relative path in bytewise order.
- **D-04 — Same-name skills.** Recommend that an eligible selected console
  skill wins over a same-name repository skill. Record the shadowed source
  even before its body is read. Compare the managed ID and valid repository
  frontmatter name. Collapse aliases by canonical package identity first.
  For distinct repository packages, prefer `.agents`, then `.claude`, then
  `.github`, with an explicit shadow reason. Alternatives are qualified
  names for both sources with unambiguous selection, or rejecting ambiguous
  repository names and asking the administrator to choose.
- **D-05 — Links, version and changed files.** Recommend permitting only
  relative package-directory aliases from approved skill roots to a supported
  canonical package under the same checkout. Revalidate at every load and
  reject symlinks in individual bodies/references and other discovery paths.
  Alternate scope is any safely contained relative link in an approved source,
  with the same policy checks. Recommend using project lock v2
  `skills.<name>.computedHash` as declared installed version when available,
  separate from observed SHA-256. Never install or refresh from the lock.
  Decide whether missing, malformed or stale lock entries are advisory or
  omit locked packages on verification failure. Decide whether to bind
  lazy reads to an immutable checkout view or recheck identity and metadata
  against the live checkout at each request and record the bytes actually
  delivered. Under the latter option, a detected identity or metadata
  mismatch requires rediscovery in a later execution. Both options defer
  reading skill bodies until requested. No policy may accept an escape or
  reuse an alias to bypass a blocked source.
- **D-06 — Progressive migration and limits.** Recommend progressive
  catalogue/body behavior for both managed and repository skills in enabled
  executions, with existing managed body delivery for legacy graphs until a
  separate migration. An alternative is changing managed delivery for all
  graphs in the same release. Define metadata, entry, reference and cumulative
  budgets, and which sources are admitted when optional guidance exceeds
  the limit. Preserve mandatory prompt/message priority and account for
  existing 16 KiB file, 128-entry and 8192-byte guidance bounds. Decide
  whether to fail before dispatch or explicitly omit unsupported progressive
  guidance for runtimes without the required behavior. Never silently fall
  back to eager repository body delivery.
- **D-07 — Audit retention and representation.** Recommend keeping the
  existing historical snapshot for discovery/composition and recording
  additional immutable per-execution read outcomes. Show them together in
  Activity with stable references to the historical run. Decide storage
  retention and redaction rules for displayable descriptions and paths,
  delivery-state granularity, and whether to store body copies for new
  entries. Preserve the current access boundary and keep bodies and
  credentials out of telemetry.

## Reconciliation

- [Behavior and oracle matrix](./matrix.md).
- Review the governing user request, this draft and the matrix before writing
  tests. After approval, record fail-first unit and integration evidence and
  review the tests before the implementation. Exercise the admin and Activity
  paths in the browser against the real API, store and engine.
- Update [instructions](../../docs/instructions.md), [API](../../docs/api.md),
  runtime and console documentation with the accepted contract on delivery.
  Record fresh test and implementation review evidence with the change.
