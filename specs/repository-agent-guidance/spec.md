---
id: REPO-GUIDANCE
title: Load agent guidance from repository workspaces
summary: Discover repository instructions and skills alongside console guidance and show their provenance in Activity.
status: in-progress
scope: [cloned workspaces, replica worktrees, console instruction settings, execution Activity]
risk: high
risk_domains: [prompt injection, authorization, filesystem isolation, compatibility, audit]
reversibility: Disable repository discovery through saved console policy
change_surfaces: [graph and agent settings, instruction loader, runtime context, execution audit, Activity]
contracts: [graph instructions, Runtime Protocol v1, execution instruction snapshots, workspace file permissions]
review_policy: Maintainer-approved behavior and matrix recorded before tests and implementation
navigation: [specs/README.md, specs/repository-agent-guidance/matrix.md]
documentation: Update instruction, graph, API, runtime and Activity contracts when the behavior ships
authority:
  granted: [specification, local commit, implementation, local verification]
  withheld: [new tool grants, new permissions, push, pull request, deployment, release]
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

## Approved decisions and oracle provenance

On 2026-11-11 the maintainer approved option A for D-01.1 through D-07.4
in the decision review. The following contracts are spec-derived. The
acceptance criteria above and matrix use these decisions as their oracles.

### D-01 — Source coverage (AC-001, AC-005)

- Discover one root instruction. Use the first present, unblocked candidate
  in this order: `AGENTS.md`, `CLAUDE.md`, `.claude/CLAUDE.md`,
  `.github/copilot-instructions.md`. An invalid chosen file is an omission
  and does not advance the fallback chain.
- Discover immediate package directories with `SKILL.md` under
  `.agents/skills`, `.claude/skills` and `.github/skills`. Discovery does
  not recurse or follow imports. Nested rules, `AGENTS.override.md`, agent
  configuration and executable features are outside discovery.

### D-02 — Rollout and source controls (AC-002, AC-008)

- The new saved graph policy is opt-in for all graphs. An agent inherits,
  disables or narrows it. A graph block is a ceiling. Administrative blocks
  apply per source family to both the discovered and canonical paths before
  content is loaded. Preview and execution expose inheritance and omissions.
- The existing `projectSteering` setting, discovery rules and precedence
  remain independent.

### D-03 — Precedence (AC-003, AC-004, AC-008)

- Low to high: repository skills, selected root instruction, installation
  defaults, graph/agent-selected console skills, legacy `.weft/steering`,
  agent prompt, operator messages. A catalogue entry takes its skill's rank.
  Activating a body or reference preserves that rank. Ties without an
  existing declared order use the canonical relative path in bytewise order.
  In progressive mode, an installation default also explicitly selected by
  the graph or agent occupies the selected skill's higher rank once. Record
  the suppressed installation occurrence. Legacy composition is unchanged.

### D-04 — Identity and collisions (AC-003, AC-005)

- Collapse aliases by canonical package identity first. A console skill ID
  selected by the graph or agent shadows a matching valid repository skill
  name when the console catalogue entry is admitted. Installation defaults
  alone and console entries omitted for budget leave repository skills
  eligible. If an admitted console skill's body exceeds the remaining budget
  on activation, release its active catalogue slot and offer the matching
  repository entry if its framed catalogue fits the remaining budget. Charge
  the replacement without refunding previously delivered content. Record the
  console omission and the replacement outcome separately. The repository
  body still requires an explicit selection and its own budget check.
  Distinct repository packages with the same name prefer
  `.agents`, then `.claude`, then `.github`. Within a family use the
  canonical relative path tie-breaker. Report the suppressed candidates.

### D-05 — Links, lock and changes (AC-004, AC-005, AC-006)

- The sole discovery symlinks allowed are relative directory aliases from
  an approved skill root to a supported canonical package in the same
  checkout. Every hop must be contained and policy-eligible. Reject symlinks
  in bodies, references, other discovery paths and legacy steering.
- A valid v2 `skills.<name>.computedHash` is an advisory declared version,
  distinct from observed SHA-256 values. Record the observed lock status.
  Missing, malformed, unsupported or detected stale locks do not block the
  package or trigger installation, network reads or writes.
- Revalidate policy, identity and metadata against the live checkout on
  every request. A detected mismatch is an omission that requires a later
  execution to rediscover the skill. Record the digest of actual delivered
  bytes. Body changes that preserve identity and metadata can be observed at
  activation.

### D-06 — Progressive loading and limits (AC-004, AC-008)

- Enabled executions progressively load both console and repository skills.
  Legacy executions retain current console body delivery. Initial catalogue
  items expose a name, description and selection identity. An
  execution-bound selection opens only the chosen body. A later request can
  open one chosen textual reference in that repository package, subject to
  its policy and workspace permissions. Discovery and Activity identify files
  from the checkout root. Reference authorization confines the target to the
  agent's working directory and evaluates filesystem path grants relative to
  that directory, including when `workspace.subdir` is configured. An
  unrestricted path grant still applies only within the working directory.
  No implicit activation, recursion or import occurs.
- Identify repository scopes in the progressive prompt preamble. Frame each
  optional source so its text cannot introduce a source header, closing marker
  or task outside its frame. Stage digests identify the payload before framing.
- Cap each body, root instruction or reference at 16 KiB, package metadata
  at 2 KiB, enumeration at 128 candidates per source family, the catalogue
  at 128 active items per execution and reference reads at 16 distinct references
  per execution. Legacy steering and registry limits still apply.
- Keep the 8,192-byte conservative guidance budget for opted-in executions.
  Reserve mandatory prompt and operator messages first, including the
  current failure behavior for oversized mandatory text. Charge framing,
  metadata, body and reference bytes against the same cumulative budget.
  Admit optional initial entries whole from higher to lower precedence with
  deterministic ties and charge subsequent activation against the balance.
  Omit a family as a whole when enumeration exceeds its cap. The model
  gateway continues to bound the complete request.
- An incompatible runtime fails explicitly before dispatch. Retrying an
  already applied read does not duplicate context or its budget charge.

### D-07 — Activity and retention (AC-007, AC-008)

- Preserve the existing snapshot and retain immutable per-execution read
  outcomes for the lifetime of the historical execution. Show them together
  in Activity to the existing installation-wide Viewer role. Keep old
  snapshots readable. For new entries retain provenance and digests without
  body or reference copies.
- Display validated workspace-relative paths and bounded descriptions as
  inert text. Exclude absolute paths and raw error detail. Keep bodies and
  credentials out of telemetry.
- Distinguish discovery, read, confirmed dispatch and indeterminate
  delivery, separately for catalogue entries, bodies and individual
  references. Record omissions, failures and interruptions. Persist read
  evidence before handing off guidance. When delivery cannot be confirmed,
  never infer it from a successful read alone.

## Reconciliation

- [Behavior and oracle matrix](./matrix.md).
- Review the governing user request, this approved spec and the matrix before
  writing tests. Record fail-first unit and integration evidence and review
  the tests before implementation. Exercise the admin and Activity
  paths in the browser against the real API, store and engine.
- Update [instructions](../../docs/instructions.md), [API](../../docs/api.md),
  runtime and console documentation with the accepted contract on delivery.
  Record fresh test and implementation review evidence with the change.
