# Weft HTTP API

The control plane (`weftd`) exposes one HTTP listener (default `:8080`) with four surfaces:

| Surface | Prefix | Auth | Purpose |
|---|---|---|---|
| Public API | `/api/v1` | bearer (token or OIDC JWT) + role | Graphs, lifecycle, messages, approvals, history |
| Event stream | `/api/v1/events/stream` | bearer or `?access_token=` | Server-Sent Events tail of the event log |
| Runtime callbacks | `/internal/v1/executions/{id}` | per-execution bearer token | Runtime Protocol v1 (see [runtime-protocol.md](runtime-protocol.md)) |
| Operations | `/healthz`, `/readyz`, `/metrics` | none | Probes and Prometheus scrape |
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
| `PUT /api/v1/triggers/{trigger}` | admin | Edit using the create body. Changing consent scope pauses the trigger. After consent and resumption, readiness-enabled polling releases holds that fall outside the updated scope. Holds stay in place while readiness is disabled until an operator resolves them. |
| `GET /api/v1/triggers/{trigger}/matches` | operator | Read-only preview returning `{next, waiting[], skipped[], busy}`. GitHub preview spends the shared trigger search budget and reports a deferral in `error` when the budget is full. Preview never evaluates readiness or writes to the tracker. |
| `POST /api/v1/triggers/{trigger}/resolve-hold` | operator | Resolve the hold identified by `{key, generation}`. Use `force: true` to explicitly abandon a pending or failed label or assignment update, or to abandon comment publication after the label was applied. The tracker keeps its current labels and assignment. The API audits the resolution and frees capacity. It returns 409 if polling is in progress, the claim was interrupted, the hold generation changed, or tracker write-back requires forced resolution. |
| `POST /api/v1/triggers/{trigger}/pause` | admin | Pause polling. |
| `POST /api/v1/triggers/{trigger}/resume` | admin | Record current consent and activate polling. |
| `DELETE /api/v1/triggers/{trigger}` | admin | Remove the trigger. |

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

## Differences from the early working reference (`docs/dev/contracts.md`)

| Topic | contracts.md | Implemented |
|---|---|---|
| `GET /graphs/{id}/events` without `after` | not specified | Returns the latest page, ascending |
| SSE without resume point | not specified | Starts at "now" |
| `GET /graphs/{id}/nodes/{node}/chat` | all messages | Latest 200, oldest first |
| `/info.identity` | not specified | `{"name","role"}` of the caller |
| Event `replica` | number | Always present; `-1` means the node as a whole |
| Error codes | listed | Also `rate_limited`, `llm_error`, `no_answer` (callbacks) |

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
