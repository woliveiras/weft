# Declarative installation configuration

An administrator can configure Weft from YAML, review a field diff, apply the
reviewed changes and export the installation. The console remains editable.
This is application configuration, not bootstrap authentication or deployment.

## Review and apply

```sh
weft apply -f examples/installation/ --dry-run
weft apply -f examples/installation/                 # review, then answer yes
weft apply -f examples/installation/ --yes            # non-interactive confirmation
weft export -o installation.yaml                     # refuses to overwrite an existing file
weft apply -f installation.yaml --dry-run
```

`WEFT_URL` selects the server (default `http://localhost:8080`); `WEFT_TOKEN`
provides an existing admin credential. Viewer and operator credentials cannot
plan, apply or export installation configuration. The CLI reads YAML locally and
sends its text to the server. It never opens referenced secret files or resolves
secret environment variables on the client.

A file may contain several `---`-separated documents. Directories are traversed
recursively, sorted by path, and include `.yaml` and `.yml` files. Symlinks,
YAML aliases, duplicate identities and unknown fields are rejected. The request
is bounded to 4 MiB and 500 objects. Errors identify fields without echoing YAML
snippets. Existing graph documents remain supported by `weft apply -f graph.yaml`;
apply now reviews and confirms writes. Graph `x-` extensions are preserved.

```yaml
apiVersion: weft.io/v1
kind: ModelProvider
metadata:
  name: gemini
spec:
  type: gemini
  defaultModel: gemini-3.6-flash
  concurrency: 2
  contextWindow: 1048576
  secretRef: {env: WEFT_SECRET_GEMINI}
```

The plan reports `create`, `update`, `unchanged`, `conflict` or `error`, with
before/after field differences. Secret changes are only `set`, `unchanged` or
`changed`. Missing references are errors (`secret reference unresolved`);
planning never discovers tools, tests connections, downloads repositories,
builds environments or records consent. It also never writes a revision sidecar.
A plan containing errors or conflicts cannot be applied.

Administrators can also request an installation change from Chat. The review uses the same planner, revision checks, consent rules and per-object apply results. Chat accepts up to eight objects per review and can preserve an existing credential. Use the CLI or connection editor to supply or rotate credentials. After a partial outcome, obtain a new review against current revisions. [Chat operator guide](features/chat/README.md#review-installation-configuration)

## Object kinds

All documents use `apiVersion: weft.io/v1`, `kind`, `metadata.name`, optional
`metadata.revision`, and `spec`. Spec properties use existing API field names.

| Kind | Identity and configuration |
| --- | --- |
| `InferencePlugin` | Plugin ID as name; `enabled` boolean. Built-in plugins only. |
| `ModelProvider` | Provider instance name; provider input fields (`type`, `baseUrl`, `defaultModel`, `timeoutSec`, `maxRetries`, `concurrency`, `contextWindow`, `modelGuidance`, `project`, `region`), optional explicit `default` selection (`false` relinquishes an explicit selection; automatic fallback still applies), and `secretRef` instead of `apiKey`. |
| `PriceCatalog` | Name `installation`; `currency`, `unpriced`, `prices` using the existing catalogue input. |
| `CostPolicy` | Name `installation`; `taskRunBudgetMicros`, `warningPercent`. |
| `Budget` | Name `scope/id`, e.g. `installation/*`; `scope`, `id`, `limitMicros`, `warningPercent`, `interruptPercent`. Project scopes use registered project IDs. Unmentioned budgets are preserved. |
| `NetworkPolicy` | Name `installation`; `setupNetworks`, `runNetworks`, `egressHosts`. Existing installation restrictions remain in force. |
| `MCPConnection` | Connection ID as name; connection input including display `name`, `service`, `endpoint`, `enabled`, quota, transport, template, argument policy and OAuth configuration. Use references for credentials. |
| `IssueConnection` | Native tracker connection name; `kind: jira-dc`, HTTPS `siteUrl`, numeric `projectId`, `enabled`, optional `projectRestricted` confirmation (default false), and `secretRef`. Configuration only: apply makes no provider call and does not grant agent access or enable a trigger. |
| `IssueWorkflowMapping` | Native Jira Data Center mapping name; exact `connection` and positive `connectionRevision`, discovered `sampleIssueKey`, numeric `sampleIssueId`, `issueTypeId`, `fromStatusId`, `transitionId`, `targetStatusId`, exact `issueUpdated`, and `enabled`. Names/board columns cannot identify transitions. Uses the same reviewed mapping manager as the API. |
| `JiraCloudProfile` | Credential-free workflow profile: saved `mcpConnection` and native numeric `mcpRevision`, `cloudId`, exact Cloud gateway `siteUrl`, numeric `projectId`, `enabled`, and `projectRestricted` administrator confirmation. Never supplies, copies or converts an MCP/DC credential. |
| `JiraCloudWorkflowMapping` | Cloud mapping using the same reviewed sample fields as the DC kind, but `connection` names a saved `JiraCloudProfile` and `connectionRevision` is that profile's native numeric revision. Cloud and DC namespaces remain separate even for equal local names. |
| `SCMConnection` | Connection name; `kind`, `repository`, `apiUrl`, `gitUrl`, `branchPrefix`, `requirePullRequestApproval`, `authType`, `username`, `secretRef`. Bitbucket Cloud `api-token` uses the Atlassian email as `username`; omission/`bearer` preserves existing authentication. Other kinds do not accept API-token mode/usernames. |
| `Project` | An alias or exported registered project ID; `url`, optional `ref` (default `HEAD`), `checks`, `executionMode`. Existing local registrations export `path` and `displayName`. |
| `Graph` | Existing graph metadata and spec, including manual environments. `metadata.id` remains the graph identity; absent IDs derive from the display name. |
| `PullRequestReviewRoute` | Explicit saved SCM connection name and RFC3339Nano `connectionRevision`, provider/API instance/repository, and saved `graphId`, positive `graphVersion`, `channelId`, `nodeId`, `enabled`. Activation requires recorded administrator consent and never starts the workflow or grants tools. |
| `IssueTrigger` | Exported trigger ID, or an unambiguous display name for a hand-authored document; existing trigger input including optional display `name`, with mandatory `authorizationMode`. `projectId` can reference a Project alias in the same request. |

Money uses integer micro-units: USD 2 is `2000000`, USD 10 is `10000000`.
Prices use micro-units per **million tokens**. Weft does not fetch or update
vendor prices automatically. Review example rates against your own agreement.

Provider activation precedes instances. Cost and network policy, connections and
projects precede graphs; native issue connections precede workflow mappings; graphs and mappings precede triggers. Within a kind, names are sorted; explicit default providers follow other providers.
A failure stops remaining writes and reports each object as `applied`,
`unchanged`, `failed` or `not-applied`. Earlier writes remain; this is not a
transaction across services. Administrator writes on the same server wait while
a batch is applying, including during imports; viewer reads and operator runtime
controls remain available. Native store revision checks also fence writes across
server instances. Retry uses the same identities and retains completed
work. Omitted installation objects are never deleted.

## Native Jira workflow mappings

Discover transitions for the actual sample issue through the connection inspector
or `POST /api/v1/issue-connections/{name}/transitions` with the reviewed connection
revision. Copy the returned numeric identifiers and exact issue update timestamp;
display names and board columns are never substitutes. See the illustrative
[mapping document](../examples/installation/jira-dc-workflow-mapping.yaml).

Planning validates only the document shape and saved/planned connection scope.
It never calls Jira. An effective mapping apply uses the existing manager to
read the sample again, confirm its identity/type/source status/update timestamp,
validate the selected transition and destination, and reject unsupported required
fields. This is read-only discovery: saving a mapping never executes a transition.
Provider permission loss, stale discovery and concurrent scope changes fail the
mapping save. Earlier objects in a batch remain applied and later objects are
reported as not applied. Refresh discovery before retrying a changed sample.

`connectionRevision` is the native positive numeric revision shown by the
connection API, distinct from the opaque YAML `metadata.revision` of the mapping.
Its value is always explicit. When creating a connection in the same batch, its
revision is `1`; an effective update advances the saved revision by one. A pin
must match that reviewed result; the planner never rewrites it. A connection
must be enabled and reviewed as project restricted. A new/changed mapping still
requires sample validation, even when created disabled. Disabling exactly the
saved definition succeeds without Jira access. An unchanged export/apply performs
no discovery, save or audit event, even if the provider is unavailable.

Saving a definition does not enable lifecycle synchronization or grant a tool.
Apply the definition first, read its saved numeric revision, then explicitly
select that name/revision in the trigger's optional lifecycle mapping fields and
review trigger consent. A changed definition requires explicit trigger adoption;
YAML does not infer or rewrite lifecycle pins. Existing task history and grants
remain intact. Automatic confirmed-merge synchronization remains unavailable
until its independent delivery event and concurrency guarantees are qualified.

## Jira Cloud workflow configuration

Cloud optional transitions use separate `JiraCloudProfile` and
`JiraCloudWorkflowMapping` kinds. Save the compatible static first-party MCP
connection first. Then save a credential-free profile using its exact native
numeric MCP revision, Cloud UUID, numeric project ID and API scope identity:

```yaml
apiVersion: weft.io/v1
kind: JiraCloudProfile
metadata:
  name: cloud-project
spec:
  mcpConnection: atlassian-api
  mcpRevision: 1
  cloudId: 11111111-2222-3333-4444-555555555555
  siteUrl: https://api.atlassian.com/ex/jira/11111111-2222-3333-4444-555555555555
  projectId: '10001'
  enabled: true
  projectRestricted: true
```

The identifiers above are illustrative. `projectRestricted` records an
administrator's confirmation that the technical account's remote write
permissions are restricted to that project. It is not a remote permission
change or an automatic proof. `siteUrl` is the exact Cloud REST gateway scope,
not the tenant browser URL or a Data Center site. OAuth MCP credentials and
custom audiences are not converted to REST credentials.

Profile plan/preview/save perform local reads only. The profile must reference
an already saved MCP connection; a bundle cannot create or change that
connection and adopt its calculated future revision. After saving the profile,
discover the sample issue's transitions through its inspector and save a
`JiraCloudWorkflowMapping` with the sample IDs/update timestamp. Its `connection`
is the profile name and its `connectionRevision` is the saved profile revision,
not the MCP revision. The profile must already exist; profile changes and
mapping adoption require separate reviewed applies. Mapping plans never contact
Jira; effective saves validate the sample through read-only Cloud discovery.

Export retains only configuration and opaque per-object baselines. Native MCP,
profile and mapping revision numbers remain distinct from those baselines.
Unchanged applies and exact disabling of retained profiles/mappings work without
provider access. Disabling does not rewrite the original pins. Failed or stale
discovery leaves the mapping unsaved and does not execute a transition.

Finally, adopt the saved Cloud profile through the trigger's `source.profileId`
and `source.profileRevision`, retaining the matching MCP `connectionId`, Cloud
UUID, gateway site and project. Select saved lifecycle mapping name/revision
pairs and review trigger consent. A consent apply cannot simultaneously replace
the MCP connection, profile or selected mapping. Legacy Jira Cloud triggers
without profiles/mappings retain their existing comment/label behavior. The
existing `IssueWorkflowMapping` kind continues to mean Data Center.

## Portable pull request review routes

`PullRequestReviewRoute` routes new internal `scm.pull_request_opened` facts to
an explicitly selected review workflow. Save the SCM connection and workflow
first, inspect their saved revisions, then review a separate route apply. A
bundle that creates or changes either referenced dependency cannot activate or
modify the route. Unchanged dependencies may be included. The planner never
calculates or substitutes a connection or workflow revision.

```yaml
apiVersion: weft.io/v1
kind: PullRequestReviewRoute
metadata:
  name: portable-review
spec:
  connection: team-scm
  connectionRevision: '2026-11-20T12:00:00Z'
  provider: github
  instance: https://api.github.com
  repository: example/project
  graphId: code-review-portable
  graphVersion: 1
  channelId: requests
  nodeId: reviewer
  enabled: true
```

These values are illustrative. `connectionRevision` is the SCM connection's
exact `updatedAt` timestamp, while `graphVersion` is the existing immutable
workflow version. Neither is the route's opaque YAML `metadata.revision` or its
numeric API revision. Provider, API instance and repository must match the same
stored SCM connection. The selected workflow must already have its portable
read/review grants and an exclusive event-agent ingress. No route grants tools
or starts/resumes a stopped or paused workflow.

Plan/preview performs only local reads: no activation marker, queue publication
or SCM request. An effective enabled create/update requires `--record-consent`
in addition to the normal reviewed apply confirmation; object-specific force
does not replace consent. Apply delegates to the route manager, which records
a fresh activation boundary and CAS-guards the definition. Earlier events are
not replayed. Failed persistence leaves any change-request audit marker
inoperative. Uncertain review dispatches are retained and never automatically
sent again.

Export contains only the reviewed route input and an opaque baseline. An
unchanged export/apply performs no validation, save or activation, including
when source/destination configuration is no longer available. Disabling exactly
the saved definition remains local and does not require new activation consent;
changing its pins at the same time requires normal validation. Re-enabling or
changing an enabled route requires a new reviewed activation. Routing, publishing
a review summary and merging remain separate outcomes; this capability does not
enable automatic merge or change the existing GitHub MCP pilot.

## Revisions and console edits

Export embeds an opaque per-object revision. Successful apply writes revisions
atomically to a sibling `<input>.weft-state.json`, including objects completed
before a later failure. Keep this file with the local operational input. It holds
server identity and revisions only, never credentials. It is ignored by Git.

The sidecar records the last apply known to that input. Without it, an existing
object with differing desired configuration requires an exported revision or
explicit adoption. A console edit after the baseline produces a conflict.
If a known object was removed or renamed, apply conflicts instead of silently creating a replacement. Export again to use a trigger's stable ID after a rename.
Discovery health, polling counters and spend do not change declarative revisions. Budget revisions advance on edits even when a later edit restores the old amount; changing another budget does not invalidate this one.
The server checks the reviewed plan again before writing, including when forced.

```sh
weft apply -f installation.yaml --dry-run
weft apply -f installation.yaml --force ModelProvider/gemini --yes
```

`--force Kind/name` is repeatable and overrides only that object's baseline
conflict. It cannot bypass admin authorization, validation, confinement or a
change made after the reviewed plan. Export again when the source of a revision
is uncertain. No-op apply does not save, renew consent or run remote checks.

## Server-side secret references

Never place `apiKey`, `token`, `credential`, `oauthClientSecret`,
`processEnvironment` or inline import authentication in `spec`. They are rejected
locally and on the server. Use one of:

```yaml
secretRef: {env: WEFT_SECRET_GEMINI}
# or
secretRef: {file: gemini.key}
# generated by export, to retain an already stored credential:
secretRef: {preserve: true}
```

Environment references must match `WEFT_SECRET_[A-Z0-9_]+`. Installation encryption
key variables, key controls and the configured key environment names are always
forbidden, even if they match the prefix. `WEFT_ADMIN_TOKEN`,
`WEFT_RUNTIME_SECRET` and `POSTGRES_PASSWORD` are never resolvable references.

File references are relative to `secrets.dir` (or `WEFT_SECRETS_DIR`). Empty
configuration disables file references. Absolute paths, `..` components,
nonregular files and symlink escapes are refused. Root-constrained opens prevent
symlink traversal races. Maximum size is 16 KiB; component credential limits may
be smaller. Empty values fail. References are re-resolved at apply, and existing
managers encrypt the value using the installation key. Omission preserves the
stored credential; it never means clear. A preserve placeholder requires an
existing credential, so fill the commented references before using an export on
a new installation.

MCP also accepts `oauthClientSecretRef` and `processEnvironmentRefs`, a map from
process environment key to a reference. Omitted process keys remain stored.
Basic MCP credentials use `credentialUsernameRef` with `secretRef` for the
password. Browser OAuth authorization remains interactive. Export preserves
existing credentials without exporting browser access or refresh tokens.
An enabled OAuth connection saved without an access credential in
`authorization_required` state is applied successfully with that status. Apply
does not register an OAuth client, start consent or discover tools before
authorization. Authorize the connection in the console, then refresh its tools.
Changing OAuth scopes also requires authorization again. The saved configuration
and independent credential slots are retained while authorization is pending.
Independent Jira Cloud REST credentials on a first-party MCP connection use
public `jiraRest: {cloudId: <site UUID>}`, `jiraRestUsernameRef` for the owner's
email and `jiraRestSecretRef` for the token. Replacement requires both references;
inline `jiraRestCredential` is rejected. The REST slot is independent of MCP
`secretRef` and `oauthClientSecretRef`. Export includes only the Cloud ID and
`jiraRestSecretRef: {preserve: true}`, never the email or encrypted credential.
Omission preserves the selected REST mode. A changed Cloud ID requires a new
credential pair or explicit clearing through the native connection API/UI.

Private project imports may use `username` and `secretRef` for an explicit token
attempt; import authentication remains transient under the existing import
contract and is not exported or stored as a project credential.

### Compose

The default Compose file forwards these optional variables without requiring
new values for existing installations:

```dotenv
# Populate locally; never commit actual values.
WEFT_SECRET_GEMINI=<provider key>
WEFT_SECRET_GITHUB_MCP=<scoped MCP token>
WEFT_SECRET_GITHUB_SCM=<scoped SCM token>
```

Put them in your existing local `.env` and recreate `weftd` when ready; Compose
does not forward arbitrary variables automatically. Add an explicit `environment`
entry for any additional dedicated reference. Preserve bootstrap credentials and
the installation encryption key.

Alternatively configure `secrets: {dir: /run/weft-secrets}` in the server YAML
(or `WEFT_SECRETS_DIR=/run/weft-secrets`) and add a read-only Compose mount:

```yaml
services:
  weftd:
    environment:
      WEFT_SECRETS_DIR: /run/weft-secrets
    volumes:
      - ./installation-secrets:/run/weft-secrets:ro
```

Create that directory yourself, keep it outside version control, and make only
the intended files readable by container UID 10001. Do not mount the installation
key directory as the reference directory. No default secret-directory mount is
added, so an existing stack starts without extra files or credentials.

## Triggers, imports and environments

Trigger authorization is always read from YAML: `ask`, `workspace` or `full`.
The plan describes the scope, polling interval, selected mode and hand-off.
`--yes` confirms writes and repository downloads; it **does not grant trigger
consent**.

```sh
weft apply -f examples/installation/ --yes --record-consent
```

Without `--record-consent`, a new or changed trigger is saved atomically paused
with `consent required`, without invented consent. With the flag, the existing
scope validator records actor, timestamp and the requested mode. The flag can
activate an otherwise unchanged trigger waiting specifically for consent.
No-op exports preserve paused state and existing consent. Console creation and
scope edits record the reviewing admin's consent; editing a paused trigger keeps
it paused. Runner checks and tool grants still apply in every authorization mode.

A Project downloads through the same reviewed import manager as the console.
The CLI's confirmation covers this download. URL/ref pairs reuse the pinned
snapshot, including `HEAD`; they do not silently refresh branches. Existing project source URL/ref/path is immutable; use a new identity for another source. Apply waits
for the import before dependent objects. Failed imports keep durable progress and
use the existing revision-fenced retry. Configuration does not start workflows,
run tasks, push branches or write tracker issues.

Manual graph environments are saved without building. The report warns when a
changed environment needs a build. Use the existing environment build action
after review. An unavailable MCP endpoint is retained and reported with sanitized
discovery errors; dependent grants still require successfully discovered tools.
SCM saves report the result of the existing read-only connection test.

## HTTP API

See [API documentation](api.md#declarative-installation-configuration) and
[OpenAPI](../api/openapi.yaml). Plan and apply share `files`, `revisions`, `force`,
`recordConsent`, `expected` and `confirmed`; plan ignores write confirmation.
Apply requires `confirmed: true` and `expected` keyed by object identity with the
revisions returned by the immediately reviewed plan. Reports use HTTP 200 with
`ok: false` for object errors/partial results; malformed requests use HTTP 400 and
role failures use 401/403. Export returns `application/yaml`.

Implicit deletion, a console YAML editor, automatic builds and last-applied
console badges are not included. Runtime task copies, spend history and transient
runtime state are excluded from export.
