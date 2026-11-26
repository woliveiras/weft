# Weft HTTP API

The control plane (`weftd`) exposes one HTTP listener (default `:8080`) with these surfaces:

| Surface | Prefix | Auth | Purpose |
|---|---|---|---|
| Public API | `/api/v1` | bearer (token or OIDC JWT) + role | Graphs, lifecycle, messages, approvals, history |
| Event stream | `/api/v1/events/stream` | bearer or `?access_token=` | Server-Sent Events tail of the event log |
| Runtime callbacks | `/internal/v1/executions/{id}` | per-execution bearer token | Runtime Protocol v1 (see [runtime-protocol.md](runtime-protocol.md)) |
| Operations | `/healthz`, `/readyz`, `/metrics` | Probes: none; metrics: viewer when `server.protectMetrics` is enabled | Probes and Prometheus scrape |
| Console | `/` | (the SPA authenticates against the API) | Static web UI when `server.webDir` is set |

The machine-readable contract is [`api/openapi.yaml`](../api/openapi.yaml) (OpenAPI 3.1). JSON Schemas for
the shared documents live in [`schemas/`](../schemas). This page explains the conventions.

## Authentication and roles

```
Authorization: Bearer <token>
```

| Credential | Configuration | Notes |
|---|---|---|
| Static token | `auth.tokens[]` with `role` and one of `tokenEnv`, `tokenFile`, `token` (inline, dev only); `WEFT_ADMIN_TOKEN` adds an admin token | Minimum 12 characters. Compared by SHA-256 digest in constant time. |
| OIDC JWT | `auth.oidc: {issuer, audience, roleClaim}` | RS256 and ES256 (P-256). Keys from the issuer's discovery document / JWKS, cached 1 h. Checks `iss`, `exp`, `aud`. Role from `roleClaim` (default `weft_role`; string or array, highest wins; default `viewer`). Name from `preferred_username` or `sub`. |
| Disabled | `auth.disabled: true` / `WEFT_AUTH_DISABLED=true` | Every caller is `admin`. Local development only; a warning is logged. |

Roles are hierarchical: a higher role can do everything a lower one can.

| Role | Can |
|---|---|
| `viewer` | All `GET`s, `POST /graphs/validate`, event stream |
| `operator` | + start/stop/pause/resume/kill graphs and nodes, scale, inject messages, operator chat, decide approvals, test model providers, build project environments |
| `admin` | + create, update and delete graphs; manage model providers and their credentials; reset project environments |

`?access_token=<token>` is accepted **only on `GET`** requests, because `EventSource` cannot send headers.
Prefer the header everywhere else: query strings end up in proxy logs.

Startup fails if no credential is configured and auth is not explicitly disabled.

## Conventions

* **Content types.** JSON everywhere. Graph documents may also be sent as YAML
  (`Content-Type: application/yaml`; a body that does not start with `{` is parsed as YAML).
* **Field names.** camelCase (Go JSON tags). Message *metadata* keys are snake_case (`correlation_id`).
* **Errors.** Non-2xx responses carry:
  ```json
  {"error": "human readable", "code": "invalid|unauthorized|forbidden|not_found|conflict|rate_limited|internal", "details": {}}
  ```
  | HTTP | `code` | When |
  |---|---|---|
  | 400 | `invalid` | Malformed body/query, bad `If-Match`, invalid action argument |
  | 401 | `unauthorized` | Missing/invalid credential |
  | 403 | `forbidden` | Role too low; on callbacks: tool denied or rejected by a human |
  | 404 | `not_found` | Unknown graph/node/channel/approval/execution, unknown action segment |
  | 409 | `conflict` | `If-Match` mismatch, graph exists, delete while running, approval already decided, pause when not running, provider in use or default |
  | 409 | `budget_admission_blocked` | A reached monthly budget blocks a new task or its environment preparation. `details` contains the blocking `scope` and `id`. Raise the budget in Models → Costs and retry the saved draft. |
  | 409 | `budget_resume_blocked` | A monetary budget blocks graph start, graph resume, node resume, or a node start that would start a stopped graph. `details` contains the blocking `scope` and `id`. |
  | 409 | `secret_key_missing` | A credential was sent but the server has no installation key to encrypt it |
  | 422 | `invalid` | Graph validation failed, or a graph/node start whose agents cannot reach their model provider — `details` is the full `ValidationReport`; provider input invalid — `details.errors[]` with `path` and `message` |
  | 429 | `rate_limited` | Tool grant rate limit (callbacks) |
  | 500 | `internal` | Unexpected; details are logged server-side, never returned |
* **Optimistic concurrency.** `GET /graphs/{id}` returns `graph.metadata.version`. Send it back as
  `If-Match: 3` on `PUT`; a concurrent edit makes the save fail with 409. Without `If-Match` the save
  is unconditional (last writer wins). The whole-document `PUT` is the only graph write path: the web
  editor (canvas and YAML view) edits a local draft and saves it this way, so validation and version
  checks happen once, on the server.
* **Documents as written.** `GET /graphs/{id}`, versions and export return the document as it was
  saved: `spec.defaults` are applied when the graph is validated and run, not copied into each
  node, so changing a default reaches every node that does not set its own value. Only identity
  derived from names (graph, node and channel ids) is filled in on save. Graphs saved by earlier
  versions keep the per-node values they were stored with.
* **Strict graph documents.** Graph `POST`/`PUT` reject keys the schema does not define with `422`
  and one `details.errors[]` entry per key (`path` such as `spec.nodes[0].promt`, message
  `unknown field "promt"`); `POST /graphs/validate` reports them as errors too. Keys starting with
  `x-` on the graph, `metadata`, a node or a channel are extensions: they are stored and returned
  verbatim (JSON, YAML export) and never interpreted. `x-` keys anywhere else are rejected. Free-form
  maps (`node.config`, `env`, `labels`) are not inspected.
* **Pagination.** List endpoints take `limit` and return newest first (messages, executions,
  approvals). The event log pages by sequence number (`after=<seq>`) and returns ascending order.
  Defaults: messages 100, executions 50, events 200, approvals 200.
* **Asynchronous lifecycle.** Lifecycle actions record the *desired* state and return the current
  `GraphStatus`; replicas converge within one reconcile interval (default 2 s). Poll `/status` or follow
  the event stream. A budget-blocked action preserves the graph or node state and is audited with the
  requesting identity. An administrator can raise the blocking ledger in Models → Costs or through the
  budget raise endpoint. Graph summaries expose the graph lifecycle state and run ID.
  The budget resume receipt is internal to ledger reconciliation.
* **Security headers.** Every response carries `X-Content-Type-Options: nosniff`,
  `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`. CORS is closed unless `server.corsOrigins`
  lists the origin (or `*`).
* **Body size.** Bounded by `server.maxBodyBytes` (default 8 MiB).

## Model providers

Providers are instances of inference plugins managed through the API
(`source: managed`); only the development `mock` comes from server
configuration (`source: config`, read-only). Responses never contain a
credential or its ciphertext; `credential` only says whether one is
configured and where it comes from (`stored`, `none`).

| Method and path | Role | Purpose |
|---|---|---|
| `GET /api/v1/providers` | viewer | List providers (superset of the earlier `ProviderInfo`: adds `source`, `editable`, `credential`, `status`, `error`, `lastTest`). `ETag` = settings revision. |
| `GET /api/v1/providers/{name}` | viewer | One provider. |
| `POST /api/v1/providers` | admin | Create a managed provider. |
| `PUT /api/v1/providers/{name}` | admin | Update a managed provider (the name cannot change). |
| `DELETE /api/v1/providers/{name}` | admin | Remove a managed provider. `409` while a graph selects it explicitly or it is the default. |
| `POST /api/v1/providers/{name}/test` | operator | Connection test of a saved provider. |
| `POST /api/v1/providers/test` | admin | Connection test of unsaved settings (same body as create; a saved provider's stored credential is reused when the body has none). Saves nothing. |
| `GET /api/v1/providers/{name}/models` | viewer | Models reported by the provider. |
| `GET /api/v1/provider-plugins` | viewer | Installed inference plugins: manifest (local/hosted, key mode, capabilities, settings), `enabled`, `enabledBy`, `instances`. |
| `PUT /api/v1/provider-plugins/{id}` | admin | `{"enabled": true\|false}`. Plugins start disabled; providers of a disabled plugin report unavailable. `If-Match` = settings revision. |
| `GET /api/v1/provider-settings` | viewer | `{revision, default, configDefault, secrets: {enabled, keySource, keyId}, types[]}`. |
| `PUT /api/v1/provider-settings` | admin | `{"default": "<name>"}` selects the default provider; `""` returns to the configuration default. |

Create/update body:

```json
{"name": "team-openai", "type": "openai", "baseUrl": "https://api.openai.com/v1",
 "defaultModel": "gpt-4o-mini", "timeoutSec": 120, "maxRetries": 2, "concurrency": 8, "contextWindow": 65536,
 "modelGuidance": "", "apiKey": {"value": "sk-..."}}
```

`apiKey` is optional: omit it to keep the stored credential, send
`{"value": "..."}` to replace it, `{"clear": true}` to remove it. Unknown
fields are rejected (server-side `apiKeyEnv`/`apiKeyFile` cannot be set
through the API). Writes accept `If-Match: <revision>`; a stale revision
returns `409`. Without it, each write applies to the latest settings.

A connection test lists models (informational), sends one short chat
request through the same gateway agent turns use (`ok` reflects that
inference), then offers the model one tool and reports in `tools` whether it
called it in the structured form agents need (`{checked, ok, reply?, error?}`;
advisory — a model that writes the call as text fails it). Errors are returned with any echoed credential replaced by
`[redacted]`. Tests are audited as `provider.tested` events; changes as
`provider.changed`.

### Chat model selection

`GET /api/v1/chat/settings` returns the current Chat model, its revision and
one of `unconfigured`, `configured` or `unavailable`. A new installation has
revision zero and no selection. `configured` means the selected provider is
registered and available in Weft's registry. It does not establish whether an
inference call would succeed.

Administrators save an explicit provider and model ID using
`PUT /api/v1/chat/settings`, body `{"provider":"local","name":"qwen:7b"}` and
`If-Match` set to the revision just reviewed. A stale revision returns 409.
The selection applies to Chat independently of the installation's workflow
default. These routes do not contact the provider or take credentials.

### Chat sessions

A Chat message containing `/find <resource> [name]` searches addressable resources. A reply in state `discovery` presents numbered choices. The next user message in the same session can select one by name, ordinal or `/choose <number>`. Selection revalidates the target and resumes the original request. Mutations still use the confirmation endpoint.

Explicit `/inspect <resource>` requests read installation state through the same authenticated Chat session endpoint. Enter `/inspect` to list the supported resources. The server projects bounded, timestamped fields and links to the owning console page. This shortcut works without a configured inference provider.


`GET /api/v1/chat/sessions` lists up to 30 sessions per page for the current
principal, ordered by last relevant activity with the session ID breaking
ties. Pass the preceding page's `next` cursor as `after` to view older
conversations. Use `archived=true` for the archived view. Both lists report
`archivedUnread` for the current principal.
`GET /api/v1/chat/sessions/{id}`
returns the transcript and observed task states. An unavailable or
inaccessible session is reported as not found. An auth-disabled local
installation identifies its shared session list with `shared: true`.

Submit `{requestId, text}` to `POST /api/v1/chat/sessions` for a new session
or to `POST /api/v1/chat/sessions/{id}/messages` for a follow-up. The request ID
is 32 lowercase hexadecimal digits, stable across transport retries. Reuse
with different text returns a conflict. An unprocessed request can be retried
at `POST /api/v1/chat/sessions/{id}/messages/{requestId}/retry`.
An untitled session can retry its title once at
`POST /api/v1/chat/sessions/{id}/title/retry`. This runs only a bounded title
completion using the original request and leaves the transcript, operations
and recency unchanged.

Use `POST /api/v1/chat/sessions/{id}/archive` or `/restore` to move a session
between lists. These operations do not cancel its watches. Use
`POST /api/v1/chat/sessions/{id}/messages/{messageId}/read` to mark an individual
result read. Session writes use the same authenticated ownership rule as reads.
The user API has no conversation deletion operation.

An administrator's request to create a reusable catalog agent returns a
saved review with its exact agent definition and an expiry. Send the review's
`digest` to `POST /api/v1/chat/sessions/{id}/operations/{operationId}/confirm`
or `/decline`. The server binds the digest to the owner, session, operation,
definition, model revision and catalog create precondition. A repeated
decision retrieves its recorded state without creating another version.
The Chat message endpoint accepts the explicit confirmation or decline text
shown on the card when the conversation contains exactly one pending review.

A PR analysis review can expose its bounded diff at
`GET /api/v1/chat/sessions/{id}/operations/{operationId}/pr-diff`. The route
checks session ownership, operator role, the saved SCM read grant and the
reviewed head and base. Diff reads do not start an analysis and are not cached
or copied to the transcript.

For a completed GitHub analysis, `POST .../{operationId}/pr-comment-review`
accepts `{commentIds}` from the draft. It returns a separate `prPublication`
review with the exact comments and target. The decision endpoint rechecks the
saved `scm.pull_request.comment` grant, connection version and bounded diff.
Each `prReceipts` item corresponds to the comment in the same position. An
uncertain receipt can be checked with `POST .../{publicationId}/pr-comment-receipts`.
Recovery only looks up an existing write. It does not resend a comment.

Installation reviews use the same decision endpoint and require administrator access. The `configuration` on a review contains the proposed document, per-object changes, expected revisions and intake consent. A terminal review distinguishes `applied`, `incomplete`, `conflict` and `unknown`. Inspect `configReceipts` to identify each applied, unchanged, failed, untouched or saved-but-unverified object. A repeated confirmation returns the recorded outcome. A role change hides the administrative review on subsequent reads.

Chat can propose an existing `Project` by native ID with only `executionMode`
and/or `checks` in its spec. The server looks up the saved source and omitted
fields before planning and again at confirmation. The reviewed object revision
gates the update. Supplying `checks` replaces the list. The same native
installation service validates and saves the resulting project configuration.

Workflow creation uses the same decision endpoint. The workflow review contains
the planned graph, one pinned catalog agent version and task ingress defaults.
Confirmation checks that the workflow ID is still unused under the normal
workflow transition lock. A successful result records the saved graph version.
The workflow is stopped and no task is created. If an outcome cannot be
verified, the review stays unknown and repeats do not apply it again.

An operator can review and save a task draft against the current version of
a named workflow. The review seals the request, outcome, entry and result
agents, input and workflow fingerprint. The decision endpoint rechecks the
role for the specific review. A workflow edit after review requires a new
proposal. The response reports the authoritative task ID and initial draft
revision. Creating a draft neither grants repository access nor admits a run.

An administrator can confirm the start of a ready task that requires no
repository, environment preparation or delivery settings. The review seals
the task revision, workflow and current authorization. The native admission
path rechecks budgets and gates and records its operation ID with the task
claim. An `accepted` review proves admission was recorded. The conversation
tracks the actual task state and outcome separately.

An administrator can cancel an admitted running task through a separate
review of its revision, execution graph and run ID. The native task record
stores the cancellation request and exact run before the engine interrupts it.
Reconciliation resumes that recorded request if delivery was interrupted.
The operation can expire pending approvals for that task attempt. The result
appears as cancelled when the task is durably settled.

An administrator can review a task's authorization mode through Chat.
Confirmation binds the task revision, workflow scope digest, selected mode
and native authorization revision. It writes only the task's consent and
its operation receipt. Running tasks accept reductions under the native
policy. Project defaults, workflow grants and mandatory approvals use
their own controls.

An administrator can raise an existing task-attempt or current UTC monthly
budget through a `budgetRaise` review. Confirmation binds the absolute
amount in ledger currency, scope, actor, ledger revision and monthly
definition revision when applicable. The native ledger saves a receipt and
schedules eligible resumptions. The response exposes the saved revision
and the results observed for the affected runs in `budgetResult`. An
`accepted` review can still be waiting to observe a resume. A recorded
increase leaves recurring definitions and future-run defaults intact.

An operator can review the replica policy of one reusable workflow node.
`workflowScale` binds the node, policy, workflow version and run. Fixed
counts write a runtime override. Autoscale ceilings save a workflow version
and keep the current minimum. `scaleResult` reports the recorded version
and count observed from the runtime. `accepted` means the policy was saved
and the runtime is still settling. A stopped node can report a saved policy
with zero active replicas.

An administrator can review a pause or resumption of issue intake. The
`triggerControl` binds the source, trigger configuration, action, actor and
any already admitted task. Resumption also binds the fresh native consent
scope. Confirmation uses the native lifecycle and saves an operation receipt
on the trigger. `applied` confirms the intake control. Separately inspect the
trigger's current state or follow a task to see the result of admitted work.

An operator can review a saved reusable workflow environment. A
`workflowEnvironment` review binds the workflow ID, saved version and
definition hash. Confirmation uses the native non-forced build and watches
its recorded state. A compatible ready environment can be reused. `queued`
represents a build in progress, and `applied` means the reviewed definition
is ready. Inspect Environment settings for native build diagnostics.

An operator can review a start, pause, resume, graceful stop or immediate
interruption on a reusable workflow or one node. Chat seals the exact graph
version, target, run and desired control state. Confirmation takes the native
workflow transition lease and reuses its budget and execution checks. A saved
operation receipt proves the control was recorded. The response reports the
requested and observed state separately while the runtime settles.

An operator can confirm a retry draft for the exact saved revision of an
eligible failed manual task. The same source revision has one deterministic
retry draft. The result links the authoritative child ID. The original
task and its evidence remain available, and the retry has its own start
review and authorization.

An administrator can publish revised shared agent instructions through the
same review route. The review binds the current catalog version, storage
revision and prior definition. The publication appends one immutable version
with the operation receipt and preserves the other catalog fields. Workflow
pins and node-local instructions are governed by separate workflow changes.

Workflow adoption is a separate administrative review. It binds the current
workflow version and identity, node, old catalog pin and exact reviewed target
version. Chat can adopt versions that differ only in shared instructions.
The native save preserves local overrides and records the new workflow
version. An already admitted task uses its existing workflow copy.

An operator can review a decision for a specific pending message or turn
request, or answer a human question. Chat seals the exact approval record
and the intended decision. The effective expiry is the earlier of the
request's deadline and the confirmation deadline. The native approval
write checks the pending request and records the operation ID. A recorded
decision and any resulting task progress are separate observations.

## Native issue connections

Native Jira Data Center connections have their own credential store, independent
of MCP and source control. These endpoints and declarative `IssueConnection` documents configure the native REST 2 adapter. The scoped trigger lifecycle
is supported. The Connections console offers a dedicated native issue-tracker form and trigger selection. No agent permission is
created by saving or testing a connection.

| Method and path | Role | Purpose |
|---|---|---|
| `GET /api/v1/issue-connections` | viewer | Credential-free connection records. |
| `GET /api/v1/issue-connections/{name}` | viewer | Current record and revision ETag. |
| `POST /api/v1/issue-connections` | admin | Create `{name, kind: "jira-dc", siteUrl, projectId, enabled, projectRestricted?, token}`. HTTPS site context paths are supported; `projectId` is the numeric Jira ID. |
| `PUT /api/v1/issue-connections/{name}` | admin | Requires `If-Match: <revision>`. Omit `token` to retain it for the same site/project; changing scope requires a replacement native credential. |
| `POST /api/v1/issue-connections/{name}/test` | operator | Requires `If-Match`; tests authenticated identity only, returning `{ok, identity?, scope?, error?}`. This does not qualify project permissions. |
| `POST /api/v1/issue-connections/{name}/transitions` | operator | Read-only discovery for `{issueKey}` with a positive `If-Match` revision. Returns `{connection, revision, discovery}` for the issue's exact site/project/type/current status. No mapping or transition is saved. |

`projectRestricted` defaults to false. Set it only after reviewing that the
remote technical account has write permissions restricted to this project.
It is an administrator confirmation, not an automatic permission test. Native
clients opened for reads cannot write; write clients require this confirmation,
a matching current revision and an enabled connection. Agent grants and durable
write reconciliation still apply; saving a connection alone does not authorize
a trigger or an agent. Native trigger sources carry `kind: "jira-dc"`, the
connection ID and exact `siteUrl`/numeric `projectId`; consent fences its current
revision. Existing GitHub/Jira Cloud source fields are unchanged.

Stale or disabled connections cannot be tested. Secrets are encrypted with
site/project-bound associated data and never returned; records expose only
`credentialConfigured`. Malformed/unknown JSON fields are rejected. Changes
record a credential-free `tracker.connection_change_requested` audit intent;
the stored CAS revision is the authoritative mutation result. Live Data Center
version and project permission qualification remain separate requirements.

### Jira Data Center workflow mappings

Mappings are reviewed definitions; saving one does not attach it to a trigger or
execute a transition. Each definition binds a native connection/revision,
site/project, issue type, source status and transition/destination IDs. Sample
issue ID/key/update metadata must match a fresh discovery at save time. Names
are display text. The initial profile supports transitions without required
field updates; same-status actions and unknown metadata are rejected.

| Method and path | Role | Purpose |
|---|---|---|
| `GET /api/v1/issue-workflow-mappings?connection=<name>` | viewer | Credential-free definitions, optionally filtered by connection. |
| `GET /api/v1/issue-workflow-mappings/{name}` | viewer | Definition with an ETag revision. |
| `POST /api/v1/issue-workflow-mappings` | admin | Create after actual issue discovery and project-restricted account review. |
| `PUT /api/v1/issue-workflow-mappings/{name}` | admin | Replace with a positive If-Match revision and fresh discovery. Disabling the exact saved definition works without remote access. |

Input: `{name, connection, connectionRevision, sampleIssueKey, sampleIssueId,
issueTypeId, fromStatusId, transitionId, targetStatusId, issueUpdated, enabled}`.
A changed sample/connection returns conflict; required fields or invalid IDs
return validation errors; remote discovery failures return 502. Mapping CAS
atomically guards the reviewed connection revision. Arbitrary Jira field values,
credentials and board column names are not accepted by this schema. Lifecycle
adoption and operator synchronization recovery remain separate work.

The administrator-only installation plan/apply/export API also supports
`kind: IssueWorkflowMapping` with the same `MappingInput` fields (name in
`metadata.name`). Plan performs no discovery. Effective apply calls the same
reviewed-sample/CAS manager; it never executes a transition. Export excludes
presentation-only discovered names and retains explicit numeric identifiers and
connection revision. See [declarative mappings](./declarative-configuration.md#native-jira-workflow-mappings).

## Source control connections

Push and pull request tools act through server-held SCM connections; agents
never run an SCM CLI and never see a token.

| Method and path | Role | Purpose |
|---|---|---|
| `GET /api/v1/scm/connections` | viewer | Connections without credentials (`tokenConfigured` only). |
| `POST /api/v1/scm/connections` | admin | `{name, kind, repository, apiUrl?, gitUrl?, branchPrefix?, requirePullRequestApproval?, token}`; the token is encrypted with the installation key. |
| `PUT /api/v1/scm/connections/{name}` | admin | Same body without `name`; omit `token` to keep the stored one. |
| `DELETE /api/v1/scm/connections/{name}` | admin | Removes a connection. |
| `POST /api/v1/scm/connections/{name}/test` | operator | Reads the token's identity and repository access: `{ok, identity: {user, repository, defaultBranch, canPush}}` or `{ok: false, error}` with the token redacted. |

Kinds: `github` (REST API; `apiUrl` defaults to `https://api.github.com`,
`gitUrl` to `https://github.com/<repository>.git`; a fine-grained token with
*Contents: read and write* and *Pull requests: read and write* on the
repository, or an OAuth token). The test reports the default branch and
whether the token can push; 401/403/404 answers explain the likely cause.

Task drafts also support `DELETE /api/v1/tasks/{id}` for operators and
administrators. Send `If-Match` with the current task revision and the exact JSON
body `{"confirm":true}`. Success returns 204. Missing confirmation or revision
returns 400; a stale revision, non-draft lifecycle state, execution copy or
environment preparation reference returns 409. Deleted task reads return 404 and
both task listing orders omit them. A minimal deletion marker retains the task
ID, lifecycle dates and actors to fence stale saves, model results, repeated
submissions and competing admission; the discarded request is not retained in
that marker. Existing audit events remain. This endpoint never deletes a
workflow, execution history, repository, import or workspace. Tasks already
prepared or executed remain available for inspection.

Operators/admins can call `POST /api/v1/tasks/{id}/retry` with the failed
manual task's positive `If-Match` revision and strict `{"confirm":true}`.
It creates a new draft containing the original editable input and read-only
`retryOfTaskId`/`retryOfRevision` provenance. It copies no execution, approvals,
authorization or result. The response is 201 on creation or 200 on replay, with
the child's `ETag` and API `Location`. One child exists per source revision;
concurrent requests and lost responses return the same child, including later
edits/status. A deleted child returns 409 rather than being recreated.

The source is unchanged. Retry creates no workspace, build, model call or remote
action. Normal review, consent, approvals and start remain required. A stale
workflow version is retained for explicit repair. Nonfailed or trigger-owned
sources and stale revisions return 409, invalid confirmation/revision/JSON 400,
and missing source 404. Audit/storage failures surface as errors; repeating the
same request can recover a committed child. Issue-trigger recovery remains in
its separate trigger endpoint.

A task draft may name one connection in `scmConnection`. Preparing its run
grants the result agent (or the entry agent when none is set) `git.read`,
`git.write`, and `git.push`/`scm.pull_request.create` for that connection
only, and adds hand-off instructions; a missing connection blocks the start.
`GET /api/v1/tasks/{id}/progress` reports `delivery: {connection, branch,
repository, pullRequestUrl, pullRequestNumber, failures[]}`.

URLs may not carry credentials. `branchPrefix` (default `weft/`) is
mandatory on every pushed branch. Opening a pull request waits for a person
unless `requirePullRequestApproval` is explicitly `false`. Changes are audited
as `scm.connection_changed`. A push runs in Weft's own Git process with no
user or system Git configuration; the credential is an HTTP header in that
process's environment only, never in Git configuration, remote URLs, process
arguments, agent environments, events or logs.

## Issue triggers

An issue trigger starts one task at a time from GitHub or Jira under the
administrator's current consent.

| Method and path | Role | Purpose |
|---|---|---|
| `GET /api/v1/trigger-policy` | viewer | Read the installation readiness limits and catalogue currency. |
| `GET /api/v1/triggers?workflow=` | viewer | List trigger state, consent, current task, skips and readiness holds. |
| `POST /api/v1/triggers` | admin | Create and consent to `{name, workflow, nodeId, resultNodeId?, source: {kind, connectionId, repository?, cloudId?, query?}, label?, owner?, intervalMinutes?, scmConnection?, projectId?, executionMode?, authorizationMode?, readinessDisabled?}`. |
| `GET /api/v1/triggers/{trigger}` | viewer | Read one trigger. |
| `PUT /api/v1/triggers/{trigger}` | admin | Edit using the create body. Changing consent scope records replacement consent from the authenticated administrator and preserves the existing active/paused state. With current consent and active polling, readiness releases holds that fall outside the updated scope. Holds stay in place while readiness is disabled until an operator resolves them. |
| `GET /api/v1/triggers/{trigger}/matches` | operator | Read-only preview returning `{next, waiting[], skipped[], busy}`. GitHub preview spends the shared trigger search budget and reports a deferral in `error` when the budget is full. Preview never evaluates readiness or writes to the tracker. |
| `POST /api/v1/triggers/{trigger}/resolve-hold` | operator | Resolve the hold identified by `{key, generation}`. Use `force: true` to explicitly abandon a pending or failed label or assignment update, or to abandon comment publication after the label was applied. The tracker keeps its current labels and assignment. The API audits the resolution and frees capacity. It returns 409 if polling is in progress, the claim was interrupted, the hold generation changed, or tracker write-back requires forced resolution. |
| `POST /api/v1/triggers/{trigger}/pause` | admin | Pause polling. |
| `POST /api/v1/triggers/{trigger}/retry-failed` | admin | Confirm `{key, taskId, confirm:true}` for a paused readiness-enabled GitHub or Jira trigger and its original undispatched failed draft or stopped task without PR delivery. Portable origins must match the exact provider, connection, instance, issue and workflow (plus the Data Center project); historical GitHub-only origins remain supported. Cancels only an undispatched draft; retains finished tasks, graphs and prior admissions, and audits one new admission generation. Does not resume or modify the tracker. Requires a fresh verified maintainer `weft:todo` event after recording (GitHub Maintain/Admin or Jira Cloud project administrator). Jira Data Center cannot currently verify the label actor and remains blocked before dispatch. Running, ambiguous, unowned or delivered tasks return 409. |
| `POST /api/v1/triggers/{trigger}/resume` | admin | Record current consent and activate polling. |
| `DELETE /api/v1/triggers/{trigger}` | admin | Remove the trigger. |

An omitted `resultNodeId` means the entry agent. `lastFailedTask` links a retained
launch failure; `failedRetries` records explicit single-use recovery decisions.
Ask environment preparation uses administrator-only `task_environment` approvals
bound to the saved task scope. The task exposes `environmentApprovalId` and
`launchError`; an ordinary task preparation cannot bypass a pending decision.

The default selection label is `weft:todo`. The interval defaults to 15
minutes and accepts 5–1440 minutes. `readinessDisabled` defaults to
`false`. The tracker connection must support the provider API operations
described in [issue-trigger setup](task-workflows.md#issue-trigger-readiness-and-delivery).
Consent binds the source and connection revision, exact operation list,
workflow and agents, authorization and delivery settings, and the
readiness model and limits. A stale scope blocks polling until reviewed
again. A removed workflow, entry agent or explicitly selected result
agent on edit returns HTTP 422 with the `workflow`, `nodeId` or
`resultNodeId` field error and preserves the saved trigger. An unknown
trigger ID returns HTTP 404.

An eligible issue is open (Jira *To Do* category), selected by label and
query, unblocked, unassigned or assigned only to the configured owner,
and has no prior admission for that source and workflow. The server
checks triggers every 30 seconds. Polling leases coalesce missed intervals
and keep one runner in charge of a trigger. Readiness is checked before claim, task
creation, sandbox or workspace preparation, and execution budget
reservation. A hold is removed from the queue and leaves the active task
slot available. The trigger reports `readinessBlock` at ten holds. In
`holds[]`, `key`, `url`, `reason`, `message`, `round`, `state` and
`publication` describe the held issue and tracker write-back. The optional
`eligibility` field explains why the issue cannot currently be reopened. Activity
projects the same issue identity, reason, round and publication state in
`triggerHolds[]`. Tracker text is excluded from these diagnostics.

Claim applies `weft:wip`. A question or failure uses
`weft:needs-attention` with a comment describing the required action.
An open PR completes Weft's delivery and applies `weft:done` while human
review is pending. The gateway adds a GitHub closing reference using
the admitted issue identity. Jira uses comments and labels throughout.
Other labels are preserved. Tracker operations are audited under the
`trigger-<id>` principal. Poll outcomes are recorded as `trigger.run`.

## Model prices

| Method and path | Role | Purpose |
|---|---|---|
| `GET /api/v1/cost/prices` | viewer | Current price catalogue `{version, currency, unpriced, prices[], updatedAt, updatedBy}`. `ETag` = setting revision; version `0` means nothing was saved yet. |
| `PUT /api/v1/cost/prices` | admin | Saves a new catalogue version `{currency, unpriced: "allow"\|"refuse", prices[]}`; `If-Match` gives optimistic concurrency. Audited as `cost.prices_changed`. |

A price applies to a provider instance (`provider`) or a plugin (`type`) and a
model (`"*"` for any), in integer micro-units per million tokens:
`inputMicrosPerMTok`, `outputMicrosPerMTok` and optional
`cacheReadMicrosPerMTok`/`cacheWriteMicrosPerMTok` (default: the input rate).
The most specific entry wins; local plugins cost zero unless priced; anything
else is unpriced. Weft ships no vendor prices. See
[cost control](cost-control.md).

## Monetary budgets

| Method and path | Role | Purpose |
|---|---|---|
| `GET /api/v1/cost/policy` | viewer | `{taskRunBudgetMicros, warningPercent}`; `0` means task runs have no budget. |
| `PUT /api/v1/cost/policy` | admin | Sets the budget every new task run starts with and the warning level (default 80%). |
| `GET /api/v1/cost/budgets` | viewer | Monthly budget definitions `{budgets: [{scope, id, limitMicros, warningPercent?, interruptPercent?}], period}`. |
| `PUT /api/v1/cost/budgets` | admin | Replaces the definitions. When a higher limit leaves capacity in a held ledger, the controller applies the limit and attempts an audited resume in the background. Scopes: `installation` (id `"*"`), `user` (identity name), `project` (project id), `workflow` (source graph id). `interruptPercent` (101-1000) cancels the call that takes spend past it and pauses the run. |
| `GET /api/v1/cost/budgets/preview?workflow=&project=` | viewer | Budgets a new task by the caller would be held to: `{taskRunBudgetMicros, currency, budgets[]}` with what remains this month. |
| `GET /api/v1/cost/ledgers` | viewer | Monthly ledgers for the requested UTC period (current by default), plus task-run ledgers that reached their limit or hold a graph transition. Invalid rows for this selection remain visible with `invalid: true` and `revision`. |
| `GET /api/v1/cost/spend?period=YYYY-MM&graph=` | viewer | Spend of a month from `cost.recorded` events: `total` and the top 100 lines by provider/model, task run, user, project and workflow (`costMicros`, `estimatedMicros`, `unpricedCalls`, tokens). Follows the event history retention; `truncated` when more than 100 000 records exist. |
| `GET /api/v1/cost/budgets/{scope}/{budget}` | viewer | Ledger view `{limitMicros, chargedMicros, estimatedMicros, reservedMicros, remainingMicros, reached, paused, raisedBy, notResumed?, invalid?, revision?}`; scope `run` with the task run's execution graph id, or a monthly scope with `<id>@<YYYY-MM>` (UTC month). Historical monthly views suppress `paused` and `notResumed`. Only scope, ID and revision are meaningful when `invalid` is true. |
| `POST /api/v1/cost/budgets/{scope}/{budget}/raise` | admin | `{"limitMicros": n}` (must be higher). Audited as `budget.raised`. When charged plus reserved spend is at or above the new limit, the response keeps `reached` true and existing holds remain in place without a pending resume. Otherwise the controller attempts resumptions within a shared four-second window. `notResumed` lists graph IDs with pending resumptions and is also available from the ledger GET endpoints. The controller retries pending transitions in the background. The graph stays paused if any applicable ledger is reached, with the blocking scope identified in `budget.graph_transition`. Stopped, deleted and finished runs are cleared and audited. Closed periods return 422 and invalid ledgers return 409. |

[Budget enforcement](cost-control.md#enforcement) covers reservation, settlement,
pause and resume for native turns, external callbacks and delegation children.
The user budget is keyed to the task creator. A reached monthly budget blocks
new task admission with `409 budget_admission_blocked`. A blocked graph or node
control request returns `409 budget_resume_blocked`. Both responses identify the
blocking ledger in `details.scope` and `details.id`.

## Installation network policy

| Method + path | Role | Behavior |
|---|---|---|
| `GET /api/v1/network-policy` | viewer | Effective `network`, `revision`, `source`, update metadata and graph `usage` (requested hosts and admission status). |
| `POST /api/v1/network-policy/preview` | admin | `{revision, network: {setupNetworks, runNetworks, egressHosts}}`; validates the draft and returns affected graphs without persisting it. |
| `PUT /api/v1/network-policy` | admin | Saves the reviewed network settings with strict revision matching. Revision `0` requires no existing saved policy. Returns saved settings and new revision. |

Network arrays must be nonempty and contain unique `none`, `bridge` or `egress`
values. Hosts are exact lowercase DNS names, with the same 64-host ceiling as
other environment allowlists. Unknown fields, malformed input or invalid hosts
return 400; revision conflicts return 409. Bodies are limited to 32 KiB.
The settings override only network fields, preserve YAML feature restrictions,
and apply on new admission/resume without automatically stopping active work.
See [network policy management](environments.md#manage-installation-network-policy-in-the-console).

## Project environments

The environment definition is part of the graph (`spec.environment`) and is
saved with the graph. These endpoints manage its build; see
[environments.md](environments.md).

| Method and path | Role | Purpose |
|---|---|---|
| `GET /api/v1/graphs/{id}/environment/proxies` | viewer | Read-only `{proxies, lastPreparation?, truncated}`: managed proxy state and sanitized recent request samples. Maximum 32 containers, 100 log lines per proxy within 24h; unavailable items include a warning. Last preparation is a historical snapshot. |
| `GET /api/v1/graphs/{id}/environment` | viewer | Build state compared with the saved definition: `state` (`absent`, `building`, `ready`, `failed`), `defined`, `currentHash`, `upToDate`, `image`, `caches`, `error`, `log` (last 64 KiB). |
| `POST /api/v1/graphs/{id}/environment/build` | operator | Body `{"force": bool}` (optional). Starts a background build of the saved definition (`202`); an up-to-date environment without `force` is left alone (`200`). `400` without `spec.environment`; `409` while a build or reset holds the environment lease. The error message identifies the operation in progress. |
| `POST /api/v1/graphs/{id}/environment/reset` | admin | Deletes graph-owned images, writable cache volumes and build resources. [Shared images and immutable cache seeds remain](environments.md#4-reset-it). Returns `409` if the graph is running, another instance is building, another reset is active, or cleanup is incomplete. |

`DELETE /api/v1/graphs/{id}` (admin) requires the graph to be stopped. It
cancels a build owned by this controller and waits up to 30 seconds for
contention and cleanup. Graph and node starts are blocked during deletion
cleanup. A graph with no environment resource record can be deleted without
contacting Docker.

If Docker cleanup fails, deletion returns `409` with
`code: "environment_cleanup_incomplete"` and the reason. The graph remains
available for retry. Internal and storage failures return `500`.
An administrator can use `DELETE /api/v1/graphs/{id}?force=true` to defer
environment cleanup and delete a stopped graph. The optional JSON body
`{"reason":"..."}` records the operator's reason (up to 500 characters) in
the preview and audit. Avoid entering credentials or other secrets.
The shared environment cleanup preview includes `deferredGraphs`. Retry one
deferred graph by posting `{"dryRun":false,"deferredGraphs":["graph-id"]}`
to `/api/v1/environments/cleanup` (admin). A successful retry returns `204`.
The preview reports `graphExists`, `actor`, `since`, `reason` when available,
and `daemon` (SHA-256 of the configured Docker endpoint). `daemonMatches`
compares it with this controller's endpoint. `possibleResources` lists names
and label selectors, including when Docker is unavailable. An invalid record
appears with `invalid: true` and selectors derived from the reserved ID. An
administrator can release it after reviewing the possible resources.

A deleted graph's ID stays reserved until cleanup succeeds or an administrator
releases it. To release it, POST
`{"dryRun":false,"abandonDeferred":{"graph":"graph-id","reason":"verified on the original daemon"}}`
to `/api/v1/environments/cleanup`. The reason is required (1–500 characters).
A successful request returns `204` and audits the actor, reason, and possible
remaining resources. It makes no Docker calls. The endpoint returns `404`
when there is no reservation and `409` when a graph or environment lease is
busy. A missing or empty `graph`, an invalid reason, a dry run, or a request
that also selects generations or deferred graphs returns `400`. If the graph
still exists after an interrupted force deletion, the same request cancels the reservation
and preserves its environment. Alternatively, repeat the forced DELETE to
finish deleting it. A pending deletion blocks PUT, start, resume, and
environment builds and resets.

Graph create/update, deletion, graph and node lifecycle actions (start, stop,
pause, resume, kill), autoscale edits, budget pause/resume, and deferred
cleanup use the graph transition lease. Contention returns `409` with
`code: "conflict"`. Stop and kill (including task-run kill) wait up to three
seconds for the lease. Operations under the lease have a two-minute work
limit, and a crashed controller can leave a lease for up to three minutes.
A Docker cleanup timeout offers deferred deletion once the required leases
can be acquired. A timeout waiting for an active lease asks the caller to
retry after the active operation ends.

Graph deletion records pending broker groups before deleting the graph.
The controller retries removal in the background even if the ID is never
reused. Each failed graph backs off from two seconds up to one minute.
The controller logs a summary of failed attempts and records
`graph.broker_cleanup_completed` after removal. Recreating the same ID can
also retry cleanup and returns `409` while the broker cannot confirm removal.
With NATS, retained stream messages still expire according to the configured
retention period.

Starting a graph or node whose docker-isolated agents (without an image of
their own) need an environment that is not ready returns `422` with an issue
at `spec.environment`.

## Activity view

`GET /api/v1/activity` (viewer) returns one bounded summary for the console's
Activity page: what is running across all workflows and task runs, what is
waiting for a person, and the executions started in a recent window. It only
reads existing records (executions, approvals, human reviews, replica status and
the `agent.plan` and `cost.recorded` events) and never scans the full history.

| Query | Default | Notes |
|---|---|---|
| `window` | `24h` | Go duration or whole days (`90m`, `6h`, `7d`); 5m to 7d. |
| `limit` | 100 | History rows, 1 to 200. |
| `status` | all | History only: `active` (pending, running, awaiting approval), `succeeded`, `failed` (failed or interrupted), `cancelled`. |
| `workflow` | all | History only: a graph id; a task run matches its own id and its workflow (`weft.workflow` label). |
| `agent` | all | History only: a node id. |

Invalid values return `400 invalid`. Filters never change the counters, which
describe the whole window.

```json
{
  "window": "24h", "since": "…", "generatedAt": "…", "currency": "USD",
  "counters": {"running": 2, "waiting": 1, "failed": 0, "finished": 5, "cancelled": 0},
  "needsYou": [{"kind": "tool", "id": "apr_…", "title": "…", "graphId": "…", "graphName": "…", "task": "…",
                "nodeId": "coder", "replica": 0, "executionId": "exe_…", "createdAt": "…", "expiresAt": "…"}],
  "running": [{"graphId": "…", "graphName": "…", "task": "…", "workflow": "…", "state": "running",
               "replicas": [{"nodeId": "coder", "replica": 0, "state": "running", "phase": "running", "detail": "…",
                             "executionId": "exe_…", "startedAt": "…", "elapsedMs": 120000, "planStep": "Edit the handler",
                             "usage": {"inputTokens": 1200, "outputTokens": 300}, "toolCalls": 4, "costMicros": 12500}]}],
  "history": [{"id": "exe_…", "graphId": "…", "workflow": "…", "nodeId": "tester", "replica": 0, "status": "failed",
               "startedAt": "…", "completedAt": "…", "durationMs": 5000, "toolCalls": 3, "error": "…",
               "usage": {…}, "costMicros": 0}],
  "options": {"workflows": [{"id": "…", "name": "…"}], "agents": ["coder", "tester"]},
  "limits": {"history": 100, "needsYou": 100, "executions": 1000, "activeGraphs": 50, "replicas": 200, "costEvents": 5000},
  "truncated": {"needsYou": false, "running": false, "executions": false, "history": false, "cost": false},
  "errors": [{"section": "reviews", "code": "forbidden", "message": "…"}]
}
```

* **Counters.** `running`: replicas in a turn (`running`, including waiting for
  sandbox capacity, or `awaiting_approval`). `waiting`: pending approvals plus the
  pending human reviews this caller may decide. `failed` (failed or interrupted),
  `finished` (succeeded) and `cancelled`: executions *started* in the window.
* **Needs you.** Every pending approval (`kind` is the approval kind) and pending
  human review (`kind: review`), oldest first, up to 100. Decide them through the
  existing `/approvals` and `/reviews` endpoints.
* **Running now.** Live replicas of graphs that are running, paused or stopping,
  grouped per graph (a task run is its own group, labelled with `task` and its
  `workflow`). `phase` refines `state`: `waiting_for_capacity`,
  `awaiting_approval`, `awaiting_review`, `waiting_for_message`, or the state.
  Execution fields are present only while the replica's execution is in
  progress; `planStep` is the `in_progress` item of that execution's latest
  `agent.plan` event.
* **Cost** sums the `cost.recorded` events *in the window* per execution, in
  micro-units of the catalogue currency (records priced in another currency are
  left out). `costMicros` is `null` when cost could not be read.
* **Bounds.** At most 1000 executions (newest first), 1000 pending approvals, 50
  active graphs, 200 replicas, the newest 200 `agent.plan` events per active
  graph and 5000 cost records are read. Active graphs are found from the graph
  control records (one small row per graph, as `GET /graphs` reads), and human
  reviews come from the review list, which has no page size. A read that hits its bound sets the
  matching `truncated` flag; counts are then lower bounds.
* **Roles and failures.** The endpoint reuses the existing gates: executions,
  approvals and replica status are visible to every role; human reviews need
  `operator` and, when a review names reviewers, the caller among them. A viewer
  gets `errors[{section: "reviews", code: "forbidden"}]` instead of silently
  missing reviews; `unavailable` means reviews are not configured. A section that
  cannot be read is reported with `code: "failed"` and a generic message (the
  cause is logged server-side); its list and counters are `null`, never empty.
  `section` is one of `needsYou`, `reviews`, `running`, `history`, `cost`.
* **Observability.** Each section runs in its own span (`activity.<section>`);
  metrics `weft_activity_sections_total{section,outcome}` and
  `weft_activity_build_duration_seconds`.

### Activity health

`GET /api/v1/activity/health` (viewer) returns, per agent and per workflow, how
executions started in the window went: runs, failure rate, the most common
errors, median and p90 duration, tokens and cost, plus three rankings. It reads
the same records and bounds as the Activity view (the newest 1000 executions and
5000 cost records) and never the full history.

| Query | Default | Notes |
|---|---|---|
| `window` | `24h` | `24h` or `7d` only. |
| `ranking` | 10 | Rows per ranking, 1 to 50. |

Invalid values return `400 invalid`.

```json
{
  "window": "24h", "since": "…", "generatedAt": "…", "currency": "USD",
  "agents": [{"key": "wf/tester", "workflow": "wf", "workflowName": "Software factory", "nodeId": "tester",
              "runs": 4, "finished": 4, "succeeded": 1, "failed": 3, "cancelled": 0, "failureRate": 0.75,
              "p50Ms": 20000, "p90Ms": 40000, "usage": {"inputTokens": 400, "outputTokens": 40}, "costMicros": 100,
              "topErrors": [{"message": "read <path>: no such file", "count": 2, "lastAt": "…",
                             "lastRun": {"id": "exe_…", "graphId": "task-run-1", "status": "failed", "startedAt": "…"}}],
              "otherErrors": 0,
              "lastRun": {"id": "exe_…", "graphId": "wf", "status": "succeeded", "startedAt": "…"},
              "lastFailure": {"id": "exe_…", "graphId": "wf", "status": "interrupted", "startedAt": "…"}}],
  "workflows": [{"key": "wf", "workflow": "wf", "workflowName": "Software factory", "runs": 15, …}],
  "rankings": {"failsMost": {"agents": ["wf/tester"], "workflows": ["wf"]},
               "slowest": {"agents": ["wf/coder", "wf/tester"], "workflows": ["wf"]},
               "mostExpensive": {"agents": ["wf/coder"], "workflows": ["wf"]}},
  "limits": {"executions": 1000, "costEvents": 5000, "rows": 200, "ranking": 10, "errors": 5},
  "truncated": {"executions": false, "cost": false, "agents": false, "workflows": false},
  "errors": []
}
```

* **Rows.** An agent is a node of a workflow (`key` is `workflow/nodeId`, an
  opaque id); a task run counts under its workflow (`weft.workflow` label), as in
  the Activity history filter. Rows are busiest first. `finished` counts terminal
  runs; `failed` is failed or interrupted; `failureRate` is `failed / finished`
  (cancelled runs count as finished, not failed) and `null` with no finished run.
  `p50Ms`/`p90Ms` are nearest-rank percentiles of finished runs' durations,
  `null` without one. `costMicros` sums the window's `cost.recorded` events of
  the row's executions, as in the Activity view, and is `null` when cost could
  not be read.
* **Errors.** Errors of failed and interrupted runs are grouped after
  normalization: only the first line is kept; credentials (auth schemes,
  `key=value` secrets, JWTs and well-known token shapes) become `<redacted>`;
  long quoted text becomes `<text>`; URLs, e-mails, UUIDs, paths, hex and
  generated ids and numbers become `<url>`, `<email>`, `<id>`, `<path>` and
  `<n>`; the result is capped at 200 characters. A run with no error text groups
  as `(no error message)`. The top 5 groups per row are listed, most frequent
  first, each with its newest run; `otherErrors` counts the remaining failures
  (at most 100 distinct groups are tracked per row).
* **Rankings** list row keys, worst first, ties by key: `failsMost` (rows with a
  failure, by failure rate then failed count), `slowest` (by p90 then median) and
  `mostExpensive` (rows with cost, by cost; `null` when cost could not be read).
  The `agents` and `workflows` lists hold every ranked row plus the busiest
  others, up to 200 each (`truncated.agents`/`truncated.workflows`).
* **Links.** `lastRun`, `lastFailure` and each error's `lastRun` name an
  execution and its graph for the existing run detail view; the console links
  each row to the Activity history filtered by `workflow` and `agent`.
* **Bounds, roles and failures.** `truncated.executions` means more executions
  started in the window than were read; statistics then describe the newest 1000.
  Executions and cost are visible to every role, so no section is role-gated. A
  section that cannot be read is reported in `errors` with `code: "failed"` and a
  generic message (`section` is `executions` or `cost`); when executions fail,
  `agents`, `workflows` and `rankings` are `null`, never empty.
* **Observability.** Spans `activity.health.executions` and `activity.health.cost`;
  `weft_activity_sections_total{section="health_executions"|"health_cost",outcome}`
  and `weft_activity_health_build_duration_seconds`. Error text is never a metric
  label.

## Event stream (SSE)

```
GET /api/v1/events/stream?graph=software-factory&types=execution.completed,approval.requested
Accept: text/event-stream
```

```
retry: 3000
: connected

id: 4211
event: execution.completed
data: {"seq":4211,"id":"evt_…","type":"execution.completed","graphId":"software-factory",…}

: ping
```

| Behaviour | Detail |
|---|---|
| Start position | `?after=<seq>` or `Last-Event-ID: <seq>` resumes after that event; with neither the stream starts at the newest event ("now"). |
| Ordering | Matching persisted events are sent in increasing `seq` order. Filters can skip sequence numbers. |
| Delivery | The handler tails the shared event log and sends entries that match the subscription. Local appends wake it immediately. Otherwise it polls every second, including for events written by other instances. See [event persistence](events.md) for publication guarantees. |
| Heartbeat | A `: ping` comment after 15 s without events keeps proxies from closing the stream. |
| Filters | `graph`, `node`, `types` (comma separated). |
| Reconnect | Browsers reconnect after `retry` ms and send `Last-Event-ID` automatically. |

The event catalogue is in [events.md](events.md).

## Typical flows

```bash
export WEFT=http://localhost:8080
: "${WEFT_ADMIN_TOKEN:?Set WEFT_ADMIN_TOKEN from your local .env file}"
export TOKEN="$WEFT_ADMIN_TOKEN"
H="Authorization: Bearer $TOKEN"

# Create a graph from YAML (admin)
curl -sf -X POST -H "$H" -H 'Content-Type: application/yaml' \
  --data-binary @examples/graphs/hello-echo.yaml $WEFT/api/v1/graphs

# Validate a change before saving it
curl -s -X POST -H "$H" -H 'Content-Type: application/yaml' \
  --data-binary @my-graph.yaml $WEFT/api/v1/graphs/validate | jq

# Update with optimistic concurrency
curl -s -X PUT -H "$H" -H 'Content-Type: application/yaml' -H 'If-Match: 3' \
  --data-binary @my-graph.yaml $WEFT/api/v1/graphs/my-graph

# Start, inspect, stop
curl -s -X POST -H "$H" $WEFT/api/v1/graphs/software-factory/start | jq .state
curl -s -H "$H" $WEFT/api/v1/graphs/software-factory/status | jq '.nodes[] | {nodeId, state, pending}'
curl -s -X POST -H "$H" $WEFT/api/v1/graphs/software-factory/stop

# Put work into the factory (operator)
curl -s -X POST -H "$H" -H 'Content-Type: application/json' \
  $WEFT/api/v1/graphs/software-factory/channels/requests/messages \
  -d '{"type":"feature.request","payload":{"title":"slugify","description":"Add slugify(text)."}}' | jq .id

# Follow one workflow end to end by correlation id
curl -s -H "$H" "$WEFT/api/v1/graphs/software-factory/messages?correlationId=msg_…" | jq '.[] | {type: .message.type, channel, status}'

# Human in the loop
curl -s -H "$H" "$WEFT/api/v1/approvals?status=pending" | jq '.[] | {id, kind, title}'
curl -s -X POST -H "$H" -H 'Content-Type: application/json' \
  $WEFT/api/v1/approvals/apr_…/approve -d '{"comment":"ship it"}'

# Talk to a running agent, scale a cluster, kill a stuck node
curl -s -X POST -H "$H" -H 'Content-Type: application/json' \
  $WEFT/api/v1/graphs/software-factory/nodes/developer/chat -d '{"text":"prefer stdlib only"}'
curl -s -X PUT  -H "$H" -H 'Content-Type: application/json' \
  $WEFT/api/v1/graphs/hello-echo/nodes/workers/scale -d '{"replicas":5}'
curl -s -X POST -H "$H" $WEFT/api/v1/graphs/software-factory/nodes/developer/kill

# Live events
curl -N -H "$H" "$WEFT/api/v1/events/stream?graph=software-factory"
```

The `weft` CLI wraps these calls (`weft help`).

## History and identity details

| Surface | Behavior |
|---|---|
| `GET /graphs/{id}/events` without `after` | Returns the latest page, ascending |
| SSE without resume point | Starts at "now"; see [event stream](#event-stream-sse) |
| `GET /graphs/{id}/nodes/{node}/chat` | Latest 200 messages, oldest first |
| `/info.identity` | `{"name","role"}` of the caller |
| Event `replica` | Always present; `-1` means the node as a whole |
| Callback error codes | Also include `llm_error` and `no_answer`; see [Runtime Protocol](runtime-protocol.md) |

## Environment blocked-access history

`GET /api/v1/graphs/{id}/environment/blocked-history` (viewer+) returns
`requests` (newest first; safe proxy request fields plus `proxy` and `phase`),
`retentionDays` and `limit` (effective installation limits, default 7/1000), and optional `collectedAt` (last successful
collection). It reads shared storage independently of Docker and applies retention.
Unknown graphs return 404; storage/corruption failures return an error, not an
empty successful history. This sampled diagnostic history is not a complete audit.

`GET /api/v1/blocked-history-retention` (viewer+) returns `{policy: {days,
maxEvents}, revision, updatedAt?, updatedBy?}`. Revision 0 means defaults (7 days,
1,000 blocks). `PUT /api/v1/blocked-history-retention` (admin only) accepts exactly
`{policy: {days, maxEvents}, revision, confirm: true}`. Days must be an integer
1–90 and maxEvents an integer 1–10,000. Missing confirmation/revision, invalid
values, unknown fields or trailing JSON return 400; stale revisions return 409.
A successful save returns the acknowledged revision and policy. New limits apply
to subsequent reads/collection/retention sweeps, including histories of deleted
graphs; saving is not a synchronous purge of every history. Storage failures return
an error. Lower limits can permanently discard history; higher limits do not
restore discarded records. Publication: `installation.history_retention.updated`.

## Worktree checkpoints

`GET /api/v1/graphs/{id}/checkpoints` (viewer) lists the graph's
[worktree checkpoints](native-coding-runtime.md#checkpoints-nat-07), newest
first: `[{id, ref, commit, tree, nodeId, replica, sequence, executionId?,
reason: "before_edit"|"before_restore", tool?, createdAt}]`. A workspace that is
not a Git repository has none (`[]`).

`POST /api/v1/graphs/{id}/checkpoints/restore` (administrator) takes
`{"checkpoint": "<id>", "confirm": true}`, or `{"node", "replica", "confirm":
true}` for the newest checkpoint recorded before an edit of that agent. It
rewrites that agent workspace's files to the checkpoint after recording the
current state as a new checkpoint, and returns `{restored, saved, written,
removed}`. Ignored files, `.weft/`, the Git index, HEAD and branches are not
changed. Missing confirmation, unknown fields, trailing JSON, malformed ids or a
checkpoint that does not belong to the given node return 400; an unknown graph,
node, checkpoint or a workspace without Git returns 404; a replica running a
turn on this instance returns 409. Success and failure are published as
`checkpoint.restored` / `checkpoint.failed` with the caller as actor.

## Read-only workspace Git inspection

`GET /api/v1/graphs/{id}/git/{view}` accepts `view=status|diff|history`.
It uses the same viewer/read authorization as Files. Optional `node` selects
an existing worktree-mode agent and `replica` selects its nonnegative replica
(default 0). Unknown/non-worktree agents and malformed replicas return 400;
replica without node also returns 400. It never creates a workspace/worktree.
`path` is an optional literal workspace-relative file or directory; absolute
paths, traversal, NUL, `.git` segments and escaping symlinks return 400.

The JSON response contains `repository` (boolean), `directory` (resolved
workspace path), `branch` (or `(detached)`), `content` (plain text), and
`attribution: "inconclusive"`, `graphId`, and (for worktree selection)
`nodeId`/`replica`. These identify the selected workspace, not change authors. Non-repositories return `repository:false`.
Status includes untracked names; diff compares tracked working files with HEAD
(including staged changes); history contains at most 50 commit IDs, author
timestamps and subjects. No untracked content is synthesized into patches;
use Files to read it. Unborn repositories explicitly report no commits.
Submodule contents are excluded. Inspection is a live observation, not an
immutable snapshot or evidence of authorship.

Git output is capped at 4 MiB per command, with an explicit failure instead of
a truncated patch. The overall inspection has a 10-second deadline. Metadata
validation reads directory entries in batches, allows at most 100,000 entries,
caps gitdir/commondir pointer files at 4 KiB, and copies at most 64 MiB of
metadata into a private temporary directory. Copies use rooted reads and regular
files, with no symlinks or hardlinks. Git runs against that validated private
copy, which is removed after the request. This is an inspection implementation
detail, not a persisted review/preview snapshot. Symlinked metadata, alternate
object stores, redirected common directories, and unsupported Git configuration
fail closed. Only inert init/clone/branch/identity configuration keys are
supported; filters, includes, worktree configuration and executable extensions
are rejected. Hooks, fsmonitor, pagers, external diff and textconv are disabled;
Git environment overrides and global/system configuration are not inherited.
Git/configuration/storage failures use the existing 500 error contract and
request failure logging; no repository stderr is returned. Concurrent changes
to the live `.git/config`, object-store pointers or refs
after copying cannot replace the private metadata used by Git. Concurrent
writes during copying may produce inconsistent metadata and a visible failure.
Working file contents remain live; this is not an atomic snapshot of workspace
contents. Existing native/process isolation still does not sandbox hostile
processes with unrestricted access to the controller's host or temporary files.

Files remains read-only and capped at 4 MiB/2,000 directory entries. Reads use
an OS-rooted filesystem handle; `.git` and aliases into it are refused, and only
regular files can be displayed. Existing absolute paths within the workspace
remain supported by Files. No user upload, edit or delete routes are added.

The Files panel displays Git views for the selected file/directory and queries
existing execution and `tool.completed` event APIs for task context (graph-wide
for the shared workspace, filtered by node/replica for a worktree). Recorded
filesystem operations link paths with execution/node/replica/outcome when audit
arguments are structured. Truncated arguments and arbitrary shell effects are
not inferred. Recent execution results and message/event links remain context,
not proof that an execution authored a current diff. No new attribution records,
persisted snapshots, human review or preview contracts are introduced.

Browser regression: `cd web && WEFT_E2E_PORT=18103 pnpm exec playwright test e2e/git.spec.ts`.
The optional port overrides the normal 18089 disposable server. This journey
uses real SQLite, the native tool gateway, temporary Git repositories and an
artificial model response; no shared Docker daemon or external provider is used.

## Agent catalog

| Method and path | Role | Result |
| --- | --- | --- |
| `GET /api/v1/agents` | viewer | List agent summaries with their latest version number |
| `GET /api/v1/agents/{agent}` | viewer | Read one catalog entry and all its versions |
| `GET /api/v1/agents/source?workflow=…&version=…&node=…` | viewer | Get a definition from a saved inline workflow agent |
| `POST /api/v1/agents` | admin | Publish the first version from `{id, definition}` |
| `PUT /api/v1/agents/{agent}` | admin | Append a version from `{definition}` with the latest version number in `If-Match` |

A catalog entry has an `id` and a `versions` array. Entries registered from a
saved workflow also have an `origin` identifying its `workflow` and `node`.
The initial snapshot excludes workflow parameter values and local instructions.
Each version contains its
numeric `version`, `definition`, `createdAt`, and `createdBy`. A definition includes its name, instructions,
model and runtime settings, grants, isolation settings and declared parameters.

A workflow node selects a version with
`"agent": {"id":"pr-reviewer","version":1,"parameters":{"owner":"example"}}`.
Parameter values are strings. Omit optional values to use the definition defaults.
The server materializes the selected definition into the graph version. Saving a
new catalog version leaves existing graph versions unchanged. Use the node's
`localInstructions` to append instructions scoped to that workflow instance.
Local text can refer to graph parameters and values on the instance, which take
precedence over a graph parameter of the same name. A saved graph
can report validation errors if an authored binding references an unknown agent or version,
omits required values, or supplies undeclared parameters.

## Skills and instructions

See [Skills and steering](instructions.md) for registry CRUD, mandatory revision headers, graph/agent associations and inspection endpoints.

Graph instructions and node definitions accept `repositoryGuidance`. The
graph must opt in. A node can add family blocks or disable its inherited
policy. Writes use existing graph administration and version checks.

`GET /api/v1/graphs/{id}/instructions?node=...&replica=...` previews effective
saved policy and discovery. `GET /api/v1/executions/{eid}/instructions`
returns the historical manifest to authenticated installation Viewers.
Progressive snapshots expose `repositoryPolicy`, `blockedSources`, and
ordered `entries` with stages, delivery outcomes and omissions. `reads`
contains linked body and reference outcomes with their original order,
source, console revision or repository digest. New entries exclude body
copies. A missing snapshot returns 404. The console links this manifest
from Activity through the execution's **Guidance** section.

## Multi-repository manual tasks

`POST /api/v1/tasks` and revision-checked `PUT /api/v1/tasks/{tid}` accept
`repositories`. Send the complete list on edits, including `[]` to clear it.
The singular `projectId`, `baseCommit` and `scmConnection` fields continue to
represent the existing single-repository layout. Combining them with an explicit
list is a validation error. Workflow suggestions are stored as project IDs in
`spec.task.projectIds`. Drafts pin the projects independently.

| Member field | Meaning |
| --- | --- |
| `alias`, `projectId`, `baseCommit` | Unique portable directory name, registered project ID and full Git commit |
| `sourceRef`, `checks`, `requiredChecks` | Source ref, registered check definitions and the subset of check names required by this task |
| `scmConnection`, `connectionRevision`, `repository`, `targetBranch` | PR connection name, its returned `updatedAt`, repository identity and base branch. Configure every member when requesting PRs. |

`GET /api/v1/tasks/{tid}/review` returns the saved bindings and admission
blockers. `GET /api/v1/tasks/{tid}/progress` returns preparation, delivery and
confirmed PR information under `repositories`. Current workspace fingerprints
are available at `GET /api/v1/tasks/{tid}/repository-states`. Git inspection
and checkpoint listing take `?repository={alias}` for these tasks. Reviewers
can read the frozen patch for a changed member at
`GET /api/v1/reviews/{review}/repositories/{alias}/diff`.

An administrator can reconcile an outstanding delivery on a task in
`delivery_pending` with `POST /api/v1/tasks/{tid}/repositories/{alias}/deliver`,
the current task revision in `If-Match` and body `{"confirm":true}`. The
gateway checks authorization, the approved candidate, current check evidence
and the remote before applying either operation. Previous confirmed PRs
remain attached to the original task.

## Durable application reviews

These routes require an operator/admin and the review's optional reviewer allowlist.
They do not replace the existing `/approvals` contract. Creation is the agent tool
`review.request`, scoped to its own graph/node/replica and Docker workspace.

| Route | Request/result |
| --- | --- |
| `GET /api/v1/reviews` | Authorized review views, candidate digest, request evidence, state/session, generation, revision, expiry, actor/time/method and preview URL; no internal paths, graph snapshots or credential hashes. |
| `POST /api/v1/reviews/{id}/decide` | `{state, method, comment, revision}`. States: `approved`, `changes_requested`, `rejected`. Methods: `managed`, `external`, `local`, `without_execution`. Revision is the preview generation; terminal/repeated/stale decisions return 409. |
| `POST /api/v1/reviews/{id}/credential` | `{browser: false}` returns a private 15-minute client token and URL with `Cache-Control: no-store`; `true` issues a one-use browser exchange token valid for one minute. |
| `POST /api/v1/reviews/{id}/restart` | `{}`; restart failed/expired/stopped preview with a fresh generation and fresh admission. |
| `POST /api/v1/reviews/{id}/renew` | `{}`; extend a ready preview within the review deadline. |
| `POST /api/v1/reviews/{id}/revoke-access` | `{}`; invalidate all browser/client credentials and active streams. |
| `POST /api/v1/reviews/{id}/logs` | `{}`; bounded application logs for a managed preview. |

Invalid requests return 400, denied reviewers/policies 403, missing records 404,
and unavailable/stale sessions or conflicting decisions 409. Stop/kill of the
owning graph/node cancels pending reviews through the existing lifecycle routes.
Review states are `pending`, `approved`, `changes_requested`, `rejected`,
`cancelled`, `expired`, `superseded`. Session states are `none`, `queued`,
`starting`, `ready`, `failed`, `expired`, `stopped`. Preview expiry does not approve
or reject a still-pending review. `superseded` means the source changed between
approval and delivery of its continuation. Delivery uses a durable outbox and
insert-once operator message; it does not promise exactly-once external effects.

The separate gateway accepts `X-Weft-Preview-Token` or its own host-only
browser cookie, never a platform bearer token as preview authentication.
`/_weft/connect` and `/_weft/access` implement the console-origin-checked
browser handshake; application paths are otherwise preserved. See
[preview operations](sandbox-runtime.md#human-review-and-application-previews).

## MCP quotas and history

Connection create/update accepts `callsPerMinute` (integer 0–60,000, omitted/0
unlimited). Usage is a persisted fixed UTC-minute aggregate across agents and
controllers sharing storage. Exhausted admission is denied before remote dispatch.

Viewer-readable `GET /api/v1/mcp/history` accepts `connection`, `graph`, `node`,
`tool`, `type`, `outcome`, `since`, `until`, `before`, `limit`; see
[MCP history semantics](mcp.md#connection-quotas-and-call-history). RFC3339 time
bounds are inclusive; `before` is an exclusive event sequence. The response is
`{entries, hasMore, nextBefore?}`; sparse pages may be empty and still have more.
Entries contain only safe metadata/outcomes, never raw arguments/results/comments.
Invalid syntax, duplicates and unknown filters return 400; invalid ranges/enums
return 422. Storage failures are errors, not empty success.

## MCP registration and discovery

See [MCP scope](mcp.md) for the supported transport, authentication and limits.
Viewers can GET `/api/v1/mcp/connections`, `/api/v1/mcp/connections/{id}` and
`/api/v1/mcp/tools` (current selectable tool catalogue). Admins POST new connections and
PUT existing connections with their required `If-Match` revision. Operators can
POST `/{id}/test` or `/{id}/tools/refresh` under `/api/v1/mcp/connections`;
both discover a fresh catalogue and return its persisted safe connection view.
A completed discovery attempt returns 200 with `status: ready|unavailable`;
transport success is not inferred from HTTP 200 alone. Input errors are 422,
invalid bodies/missing revision 400, unauthorized roles 403 and stale revisions
409. Disable/credential/endpoint changes invalidate the catalogue. Admins can
DELETE `/api/v1/mcp/connections/{id}` with the current `If-Match` after disabling
the connection and revoking OAuth when configured (otherwise 422). Success is
204; removed IDs return 404 on reads and cannot be reused (409 on create).
Minimal ID reservations and audit history remain; remote registrations are not deleted. MCP execution uses the existing agent tool gateway/callback path; there
is no standalone endpoint that bypasses the execution principal.


Task outcome clarification (2026-11-16): native `finish.signal` also accepts
`blocked`. Its execution has `status: failed`, `signal: blocked` and the retained
summary; it ends without input retry or downstream emits. Task progress uses the
existing `blocked` state for agent blockers and missing required PR delivery.
New owned execution copies stop through existing engine reconciliation and their
task `status` persists the settled state. Historical admission records remain
unchanged; `observedState` / task progress describes the current evidence.
Task progress execution summaries are sanitized and limited to 201 Unicode
characters. Raw execution audit records are retained. `GET /graphs` continues to
include execution copies for diagnostic consumers; the console workflow list
and selector exclude them. Non-GET graph controls remain refused with 409 for
`task-run-*`; use the owning task's controls.

## Declarative installation configuration

All three endpoints require **admin**. The console remains editable and uses the
same underlying validation, encryption, authorization and audit services.

| Method and path | Behavior |
| --- | --- |
| `POST /api/v1/config/plan` | Read-only ordered field diff; no discovery, imports, tests or builds. |
| `POST /api/v1/config/apply` | Confirmed, revision-fenced application; stops at failure with a partial report. |
| `GET /api/v1/config/export` | Deterministic `application/yaml`, safe preserve references and object revisions. |

Plan and apply accept the same JSON schema:

```json
{
  "files": ["apiVersion: weft.io/v1\nkind: CostPolicy\nmetadata: {name: installation}\nspec: {taskRunBudgetMicros: 2000000, warningPercent: 80}\n"],
  "revisions": {},
  "force": [],
  "recordConsent": false,
  "expected": {},
  "confirmed": false
}
```

For apply, set `confirmed: true` and fill `expected` with each `objects[].key`
and `objects[].revision` from the reviewed plan. `revisions` represents the
input's prior successful apply; otherwise `metadata.revision` is used.
`force` contains explicit `Kind/name` identities. It does not override a stale
reviewed plan. Secret values and ciphertext are never returned.

Reports contain `ok` and `objects`: `key`, `action`, `revision`, `changes`
(`path`, `before`, `after`), optional `secret`, `status`, `consent`, `warning`,
`error` and, after apply, `result`. Actions are create/update/unchanged/conflict/error;
results are applied/unchanged/failed/not-applied. Inspect `ok` even on HTTP 200.
Malformed/oversized bodies use HTTP 400; authentication and role failures use
401/403. Maximum YAML content is 4 MiB/500 documents.

No object is implicitly deleted. Trigger consent needs `recordConsent`; without
it, new/changed triggers are paused with `consent required`. Configuration apply
does not start tasks or build environments. MCP discovery reports sanitized
status; SCM reports its existing connection test. MCP views include optional
`configurationUpdatedAt`, which excludes discovery-only updates.

See the [operator guide](declarative-configuration.md) for kinds, reference
confinement, import confirmation, recovery and CLI examples.

### SCM edition capabilities

`GET /api/v1/scm/kinds` is available to authenticated viewers, operators and
administrators. It returns the registered built-in delivery adapters, with
`id`, `name`, `repositoryHint`, `apiUrlHint`, `apiUrlRequired`,
`credentialHelp`, `push`, `createPullRequest`, `readPullRequest` and `reviewSummary`. This is implementation metadata;
it performs no external request and does not assert that an account is reachable
or authorized. Connection testing remains an explicit operator action.

The console uses this catalogue for source control creation and clears the
unsaved token and API/Git overrides when changing editions. Data Center requires
a site base URL; Cloud uses its own endpoint and credential contract. Both
editions use the existing `SCMConnection` YAML kind with their explicit adapter
IDs; no implicit edition or credential conversion occurs.

### Portable pull request reading

The gateway tool `scm.pull_request.read` requires an explicit tool grant and a
`repositories` scope containing the SCM connection name. Input is
`{connection, number, diff?}`. It returns the connection, provider, repository and
`pullRequest` snapshot (`number`, `url`, `title`, `body`, `state`, `head`, `base`,
`headSha`, `baseSha`, `draft`). `diff: true` adds a bounded diff. Repository text
is untrusted content; it cannot authorize tools or a merge.

GitHub uses the immutable `base...head` comparison; Bitbucket Cloud uses
`head..base?topic=true` directly on its repository diff endpoint (the PR patch
endpoint redirects). Data Center uses explicit `sinceId`/`untilId` and returns
a structured diff as JSON text, rejecting a missing/mismatched hash or a
truncation flag at any depth. Responses larger than 2 MiB and redirects are
rejected. All editions re-read the PR after fetching a diff and refuse a changed
head/base/state. Only same-repository branches are currently supported.

This read operation does not publish a summary, approve a PR, check CI or merge.
A separate grant and a separately verified mutation contract are required for
those operations. Existing workflows receive no new grants automatically.

### Portable review summary publication

`scm.pull_request.review` requires an explicit grant scoped to the SCM connection.
Input: `{connection, number, headSha, baseSha, verdict, summary}`. Verdict is
`approved` or `changes_requested`; a nonempty explanation is always required,
including when no findings exist. This publishes a **summary comment**, not a
provider-native approval. It does not merge or grant merge permission.

The PR must be open, non-draft and at both exact reviewed revisions. A durable
CAS reservation under `scm/reviews/` precedes the only POST. A server-generated
marker and exact comment body are reconciled with a bounded comment search.
Cloud pending/deleted comments and Data Center pending comments cannot confirm
publication. A confirmed receipt emits `scm.review_summary_published` and returns
`status: published`. Connection changes invalidate receipt identity.

Repeated identical requests reconcile the receipt without posting again; a
different summary/verdict for the same reserved revision pair is refused. If a
reserved attempt cannot be confirmed, it remains pending: the operator must
investigate the remote outcome. A retry never silently resets the reservation or
blindly reposts. A deleted/changed summary is no longer confirmed on retry.
No persistent receipt is automatically deleted. Publication records never store
the summary text or credential; URLs are redacted before persistence or events.

Native Jira transition discovery reads the issue, its available transitions and
required-field metadata, then rereads the issue. Connection revision changes or
disabling during discovery return 409 without publishing obsolete metadata;
remote failures return 502 with no partial result. Discovery is independent of
the project-restricted write attestation because it performs no remote mutation.
`fields: null` means unavailable metadata; `{}` means known-empty requirements.
Names are display text; transition and destination IDs remain separate.


### Optional Jira lifecycle synchronization

Jira DC trigger input, or Cloud input with an explicit reviewed workflow profile,
optionally accepts:

```json
{"lifecycleMappings":{"workStarted":{"name":"start-work","revision":1},"prDelivered":{"name":"send-to-review","revision":2}}}
```

Each pin must refer to an enabled definition for the exact reviewed connection,
revision, site and project. Empty pins preserve comments and labels. Editing
pins while a task is admitted is rejected by console/API and declarative apply.
Mapping edits do not retarget an admitted task or turn optional synchronization
into a prerequisite for PR delivery. A confirmed-merge stage is unavailable.

| Route | Minimum role | Contract |
| --- | --- | --- |
| `GET /api/v1/lifecycle-syncs?after=<sync-id>&connection=<name>` | viewer | `{items, next}`; at most 100 records, key cursor, no global history ceiling. Follow `next` for later pages; this is not chronological pagination. Optional connection filtering applies within each scanned page; an empty page can still have `next`. |
| `GET /api/v1/lifecycle-syncs/{id}` | viewer | `{sync, transition?}` retains task/stage, evidence, numeric issue identity, source, mapping revision and independent outcome. Optional transition is the exact historical adapter intent, including source/target status and transition IDs; missing intent is omitted, storage failures propagate and scope discrepancies conflict. Current mapping edits never replace historical intent. |
| `POST /api/v1/lifecycle-syncs/{id}/reconcile` | operator | Positive `If-Match` plus exactly `{"confirm":true}`. Read the retained transition intent; never POST a transition. Unknown outcomes remain visibly unknown. |
| `POST /api/v1/lifecycle-syncs/{id}/retry` | admin | Same revision/confirmation contract. Queue only a known pre-dispatch failure under its original unchanged consent and mapping. Unknown/pending writes are ineligible. |

The runtime milestone uses durable admission after gates/preparation, not task
creation or an approval-waiting execution. PR delivery uses a persisted
`scm.pull_request_opened` event, never a URL in agent text. Milestones are reserved
independently before current-task state is cleared. A separate controller uses
bounded storage pages and serializes stages per task; a pending/unknown start
prevents its delivery synchronization from overtaking it. Task/PR delivery itself
continues independently. Errors remain in the journal with operator recovery.
Retry of a start after delivery synchronization has run is refused. Seeing the
target status proves observation, not that Weft caused a transition.

### Portable internal pull request review routes

These installation routes consume only persisted Weft `scm.pull_request_opened`
facts with complete immutable identity. They do not register an external webhook.
Input pins `name`, `connection`, its exact RFC3339Nano `connectionRevision`
(SCM `updatedAt`), `provider`, `instance`, `repository`, `graphId`, positive
`graphVersion`, `channelId`, `nodeId` and `enabled`. The saved destination must
already have explicit read/summary grants scoped to that connection and an
exclusive enabled event consumer. No tool grant, model, connection or graph is
created as a side effect.

| Route | Minimum role | Contract |
| --- | --- | --- |
| `GET /api/v1/review-routes?after=<name>` | viewer | `{items,next}`; at most 100 key-ordered records per page. |
| `GET /api/v1/review-routes/{name}` | viewer | Saved route and ETag revision. |
| `POST /api/v1/review-routes/preview` | admin | `{route:input}`; local validation only, no writes or vendor calls. |
| `POST /api/v1/review-routes` | admin | `{route:input,confirm:true}`; create with CAS zero. |
| `PUT /api/v1/review-routes/{name}` | admin | Same body plus positive `If-Match`; name must match the URL. |
| `GET /api/v1/review-route-receipts?after=<id>` | viewer | `{items,next}`; at most 100 key-ordered receipts per page, including failed/unknown outcomes. |
| `GET /api/v1/review-route-receipts/{id}` | viewer | Historical source identity, route snapshot, destination run, message ID and independent outcome. |
| `POST /api/v1/review-route-receipts/{id}/reconcile` | operator | Positive `If-Match` plus exactly `{"confirm":true}`; confirms only exact retained engine-authored message evidence, without publishing again. Missing/expired evidence stays unknown. |
| `POST /api/v1/review-route-receipts/{id}/retry` | admin | Same revision/confirmation; requeues only known failures before dispatch, with unchanged route/SCM pins and a running compatible destination. |

Effective activation or editing records an event-sequence barrier: older events
are not replayed. An unchanged save retains its barrier; exact disabling works
without reachable dependencies. Duplicate events and routes reserve one receipt
per connection/revision, provider/instance, repository, PR and head/base revision.
The controller checks both durable graph state and the actual loaded run/version;
it never starts or resumes a graph. In-flight dispatch already accepted before
disabling is not retroactively cancelled.

`published` means engine dispatch acceptance, possibly held for channel approval,
not review completion. Track the execution and
`scm.review_summary_published` fact for the summary. A lost response or interrupted
dispatch is `unknown` and cannot be blindly retried. These routes grant no merge
capability. See the [separate adoption example](../examples/portable-code-review/README.md)
and [review evidence limits](./portable-review-evidence.md).

### Jira Cloud workflow profiles

A Cloud workflow profile is a credential-free, explicitly reviewed project scope
for optional automatic transitions. It references an existing first-party MCP
connection and its numeric revision. Configure independent REST access through
MCP connection input `jiraRest: {cloudId}` and write-only
`jiraRestCredential: {username, value}`. The email is limited to 320 bytes and
the token to 16 KiB; the complete connection JSON request has a 64 KiB envelope
limit (including JSON escaping and other fields). The public view returns only
`jiraRest: {cloudId, credentialConfigured}`. Omission preserves the stored pair;
`jiraRestCredential: {clear: true}` clears it without enabling fallback. A Cloud ID
change requires replacement or clear. Each change advances the connection
revision, requiring reviewed profile/trigger adoption. Independent REST dispatch
uses Basic authentication only to that Cloud ID's fixed api.atlassian.com Jira
REST3 path. MCP OAuth may coexist, but is never reused for REST. Custom endpoints
and conflicting policies remain unsupported. Legacy static connections retain
existing behavior until independent REST is selected.

```json
{"name":"cloud-project","mcpConnection":"jira","mcpRevision":1,"cloudId":"11111111-2222-3333-4444-555555555555","siteUrl":"https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555","projectId":"10001","enabled":true,"projectRestricted":true}
```

`siteUrl` is the exact Cloud gateway identity for the selected `cloudId`;
`projectId` is numeric. `projectRestricted` records the administrator's review
that the technical account's remote write permissions are limited to this
project. A local preflight cannot enforce that account configuration remotely or
atomically prevent a concurrent project move. Without that prerequisite, writes
remain unavailable. Profiles and mappings have separate Cloud/DC namespaces.

| Route | Minimum role | Contract |
| --- | --- | --- |
| `GET /api/v1/jira-cloud-profiles` | viewer | Bounded credential-free catalogue. |
| `GET /api/v1/jira-cloud-profiles/{name}` | viewer | Saved scope with ETag revision. |
| `POST /api/v1/jira-cloud-profiles/preview` | admin | Local input/REST-compatibility/revision validation; no save or remote request. |
| `POST /api/v1/jira-cloud-profiles` | admin | Save reviewed scope with profile/MCP CAS; no issue mutation. |
| `PUT /api/v1/jira-cloud-profiles/{name}` | admin | Positive `If-Match`; input name matches URL. Exact disabling remains offline. |
| `POST /api/v1/jira-cloud-profiles/{name}/transitions` | operator | Positive `If-Match` and `{"issueKey":"APP-7"}`; issue-specific GET discovery with profile/MCP revision recheck. |
| `GET /api/v1/jira-cloud-workflow-mappings?connection=<profile>` | viewer | Cloud mappings only, optional profile filter. |
| `GET /api/v1/jira-cloud-workflow-mappings/{name}` | viewer | Cloud mapping with ETag revision. |
| `POST /api/v1/jira-cloud-workflow-mappings` | admin | Existing mapping input contract; `connection` names the Cloud profile and `connectionRevision` pins its revision. Saving rechecks the sample issue and available transition IDs. |
| `PUT /api/v1/jira-cloud-workflow-mappings/{name}` | admin | Positive `If-Match`; same sample/discovery rules, exact disable offline. |

Transition and status names are display text. A column named `Review`,
`In Review` or a localized name is never guessed. Required transition fields
remain unsupported and visibly refused. Existing Cloud triggers without profiles
or mappings retain their comments/labels behavior. Optional profile adoption
pins its revision separately from the MCP credential connection revision.

For Cloud adoption, source kind remains `jira` and connectionId remains the MCP
connection. Add explicit `profileId`, positive `profileRevision`, and the profile's
exact `cloudId`, `siteUrl` and numeric `projectId`. MCP consent revision and profile
revision are independent. Changing these pins requires reviewed trigger consent
and cannot retarget an already admitted task. A mapping's `connection` names the
profile, while tracker reads/comments/labels continue using the MCP connection.

`GET /api/v1/lifecycle-syncs?kind=jira&connection=<profile>` filters Cloud history
by profile; `kind=jira-dc` filters by native connection. Without kind, the previous
connectionId (MCP/native) filter is preserved. Filters apply to each bounded page;
follow `next` even if a filtered page is empty. An invalid kind returns 400.
