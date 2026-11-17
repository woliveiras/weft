---
id: OPS-003
title: "Declarative Installation Configuration"
summary: "Plan, apply and export installation objects through administrator-only YAML configuration."
status: implemented
scope: [installation configuration, CLI, admin API, secret references, consent]
risk: high
risk_domains: [secrets, authorization, persistence, concurrency]
reversibility: "Object updates remain editable through the console; no implicit deletion."
change_surfaces: [server/internal/api, server/cmd/weft, docker-compose.yml]
contracts: [weft.io/v1, config plan/apply/export]
navigation: [specs/README.md, specs/installation-and-operations/declarative-installation-configuration/matrix.md]
documentation: [docs/declarative-configuration.md, docs/api.md, api/openapi.yaml]
dependencies: [OPS-001, INT-001, INT-004, INT-005, RUN-005, RUN-006, ENV-001, ENV-009]
authority:
  granted: [specification, implementation, fixture verification, local commits, standard Dockerfile rebuild, live export and dry-run]
  withheld: [live apply, live external writes, push, secret inspection, new image acquisition]
---

# Declarative Installation Configuration

An administrator can reproduce an installation from reviewed YAML, inspect a
field diff before writes and export the current configuration without secrets.
The console remains an equal configuration surface.

## Maintainer decisions

- **2026-11-17 — Complete scope.** Include inference plugin activation and model
  providers, price catalogue, cost policy, monthly budgets, installation network
  policy, MCP and SCM connections, URL-imported projects, graphs and issue
  triggers. This supersedes UI/API-only provider configuration in INT-001 and
  console-only routine setup in OPS-001. Existing graph YAML remains accepted.
- **2026-11-17 — Server-resolved secret references.** Accept dedicated environment
  variables and bounded files under an administrator-configured directory. Resolve
  on the server and seal through existing installation encryption. Omission keeps
  the stored secret. Never return values in YAML, plans, responses, logs or events.
- **2026-11-17 — Reviewed apply with editable console.** Plan before confirmation,
  preserve per-object revision knowledge, require object-specific force for
  conflicts and export deterministic configuration. No implicit removal.
- **2026-11-17 — Explicit trigger consent.** Console creation and scope edits
  record the authenticated administrator's consent. Declarative writes require
  explicit recordConsent; otherwise changed/new triggers are saved paused with
  reason `consent required`. The authorization mode must come from the document.
  This supersedes RUN-006's edit-then-resume consent flow only for reviewed admin
  edits; runner scope checks and authorization semantics remain unchanged.

## Format and interface

Each document has `apiVersion: weft.io/v1`, `kind`, `metadata.name`, optional
`metadata.revision` (opaque), and `spec`. The eleven kinds are InferencePlugin,
ModelProvider, PriceCatalog, CostPolicy, Budget, NetworkPolicy, MCPConnection,
SCMConnection, Project, Graph and IssueTrigger. Graph retains its existing
metadata (including id/name) and spec. Input properties otherwise follow the
existing API's camelCase names and units; money uses integer micro-USD.
Singleton names are `installation`. Budget identity is scope/id. Exports use stable trigger IDs and retain the display name in spec.name; hand-authored trigger names must resolve unambiguously. Project aliases resolve by URL/ref without introducing duplicates.

Credential input is replaced by `secretRef: {env: WEFT_SECRET_GEMINI}` or
`secretRef: {file: gemini.key}`. Export uses `secretRef: {preserve: true}` with a
comment explaining how to replace it; this sentinel never reads a secret and
requires an existing credential. It permits export/apply without credentials
being available in the operator's shell. Additional secret slots, where supported
by an existing connection, use named references rather than plaintext.

`POST /api/v1/config/plan` and `POST /api/v1/config/apply` accept the same JSON
body: locally read YAML documents, object revision knowledge, an explicit list of
forced `Kind/name` keys, and recordConsent. Apply additionally requires confirmation
and the revisions returned by the reviewed plan; stale plans are rejected before
writing the affected object. `GET /api/v1/config/export` returns ordered YAML.
All three routes require admin and use existing service validation and audit.

The CLI accepts multi-document files or recursively sorted .yaml/.yml files in a
directory. It prints the plan before prompting; `--yes` confirms, `--dry-run`
never writes, `--force Kind/name` is repeatable and `--record-consent` is separate
from confirmation. Successful object revisions are atomically saved in a sibling
`<input>.weft-state.json` file, including partial success; YAML is not rewritten.
An export's embedded revision is authoritative unless the sidecar records a later
successful apply of that input. No sidecar is written during dry-run.

## Behavior and invariants

- The server accepts only reference syntax for credential fields; raw credential
  updates, credential-bearing URLs and unknown fields fail with field paths.
  Parse errors never echo source snippets or values.
- Environment references match `WEFT_SECRET_[A-Z0-9_]+`, excluding installation
  key variables (`WEFT_SECRET_KEY`, `WEFT_SECRET_KEY_PREVIOUS`, key-file/generation
  controls and configured key environment names). No general environment lookup.
- File references are relative, contain no `..` component, and are opened under
  `secrets.dir` with root-constrained filesystem access (including symlink races).
  Reject escape, nonregular files, empty files and files larger than 16 KiB.
  Missing references produce a bounded `secret reference unresolved` diagnostic.
- Secret diffs contain only set/unchanged/changed. Decrypted bytes and secret
  digests are never part of public revisions or persisted declarative metadata.
- Plans have no writes, discovery calls, SCM tests, imports, builds or consent
  events. Availability of references may be checked server-side without storage.
- Revisions cover configuration and credential changes, not transient health,
  polling counters or budget spend. Budget definitions retain independent revision stamps in their existing settings record, including change-then-restore edits; legacy records are not rewritten on reads. Compare before writing; force overrides only
  the selected object's baseline conflict, never authorization or validation.
- Existing objects absent from the file remain untouched. A no-op performs no
  save, audit mutation, consent renewal, import, test or discovery call.
- Apply order: plugin, provider, prices/cost/budgets/network, MCP/SCM, project,
  graph, trigger. References within the batch resolve by stable identity. Errors
  stop subsequent writes; report applied/unchanged/failed/not-applied per object.
  Retry keeps completed work idempotent; no pretend transaction or rollback.
  Each server serializes administrator mutations during a batch; long imports can
  delay other administrator writes. Native store revisions remain the write
  preconditions across server instances; this is not a distributed transaction.
- Apply confirmation includes repository download. Imports use existing reviewed
  URL/ref validation, durable import progress and retry rules, and wait for a
  completed project before dependent graphs/triggers. No GitHub writes in plan.
- MCP saves discover tools using the existing service and expose ready/unavailable
  plus sanitized diagnostics. SCM apply reports its existing connection test.
- Manual graph environments remain accepted. No automatic build. Environment
  changes report that the definition needs a build before use.

## Acceptance criteria

### AC-001 — Typed YAML and compatibility

All eleven kinds accept valid existing API configuration; reject unknown kinds,
duplicate identities, invalid fields, credential values and malformed documents
with safe field paths. Multiple files/documents have deterministic ordering.
Existing graph-only `weft apply -f graph.yaml` remains supported.

### AC-002 — Secret confinement and preservation

Only dedicated allowed environment variables or confined bounded regular files
resolve. Reject forbidden variables, traversal and symlink escape. Omitted refs
retain stored credentials. Export preserve references round-trip. Credential
values never appear in plans, responses, export, captured logs or stored events.

### AC-003 — Review, diff and conflicts

Plan reports create/update/unchanged/conflict/error per object with field changes.
The CLI writes only after confirmation. Per-object baseline revisions detect
console edits; explicit object force is required. Concurrent changes after plan
invalidate its write precondition. Unrelated objects do not spuriously conflict.

### AC-004 — Ordered and recoverable application

Apply uses the same validation, authorization and audit as existing services,
resolves dependencies, stops after failure and reports partial success and skipped
objects. Reapplying is idempotent and never deletes unmanaged objects.

### AC-005 — Deterministic export

Export contains all supported configuration kinds, ordered deterministically,
with revisions and safe secret placeholders. Export then apply against the same
installation produces unchanged for every object and no runtime side effects.

### AC-006 — Explicit trigger consent

Authorization is mandatory in YAML. Plan describes consent scope and mode.
Without recordConsent, a new/changed trigger is paused with `consent required`
and no fabricated consent. With it, record actor, timestamp and file mode using
existing scope rules. Console admin scope edits record consent. No-op apply
preserves paused state and consent; authorization semantics remain unchanged.

### AC-007 — Operational integration

MCP discovery and SCM tests use existing managers after save and report sanitized
outcomes. URL/ref project imports require the reviewed apply confirmation and are
idempotent. Network policy precedes graph validation. Changed manual environments
report build-required without launching a build.

### AC-008 — Administrator API and CLI

Plan/apply/export deny unauthenticated, viewer and operator callers. CLI file,
directory, dry-run, confirmation, force, consent and export flows work against a
real fixture HTTP server. CLI never resolves or transmits credential values.

### AC-009 — Operator documentation and safe qualification

Document API, format, revisions, secrets, recovery and consent; include the full
Gemini/GitHub rehearsal example and backward-compatible Compose reference inputs.
Verify Go unit/integration, lint, builds and e2e with cached images. Rebuild weftd
from the standard Dockerfile, then only export and dry-run on the real stack.
Missing example secrets are reported safely; rehearsal resources remain unchanged.

## Explicit exclusions

Implicit deletion, YAML console editor, automatic environment builds, OAuth browser
consent automation, new authorization modes, live GitHub writes and production
qualification. Last-applied console badges are optional. Runtime task copies,
spend history, leases, credentials and ephemeral diagnostics are not configuration.

## Specification review

The three governing decisions precede implementation. Preserve placeholders and
CLI sidecars make round-trip and revision knowledge explicit without secret export.
Existing service validation remains authoritative; validation failures must not be
silently converted to success. Tests must cover both pure rules and real component
collaboration. No permission to mutate the live installation is inferred from YAML.

## Reconciliation

- [Behavior and oracle matrix](./matrix.md).
- INT-001, OPS-001, RUN-006 and ENV-009 record the bounded maintainer decisions.
