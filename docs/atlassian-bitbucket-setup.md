# Connecting Atlassian Cloud and Bitbucket Data Center

This guide is the short path: configure each service once, then every task can
use it under your agents' grants. It covers the two setups most teams combine:
Jira and Confluence on **Atlassian Cloud**, and source code on a self-hosted
**Bitbucket Data Center**. Details and limits live in [MCP connections](mcp.md)
and [MCP integration contracts](mcp-integrations.md).

Examples use `example.atlassian.net` and `bitbucket.example.com`. Replace them
with your own hosts.

| Part | Status |
|---|---|
| Atlassian Cloud through the official MCP server | Implemented; live qualification pending ([evidence](mcp.md#qualification-and-remaining-external-acceptance)) |
| Bitbucket Data Center access with an HTTP access token | Native REST/Git adapter implemented with local fixtures; live/version qualification pending |

## Portable integration delivery status

The [INT-009 specification](../specs/providers-and-integrations/portable-issue-and-source-control/spec.md)
records the implementation batches and remaining qualification. The table
distinguishes available building blocks from unfinished operational flows.

| Product/edition | Current local boundary | Remaining qualification |
| --- | --- | --- |
| Jira Cloud | Polling/readiness and comment/label lifecycle; explicit credential-free project profiles, discovery, mappings and optional runtime/PR synchronization with API/UI/YAML and local fixtures | Real-account/project permission and selected tracker/SCM combination qualification; additional authentication modes remain separate |
| Jira Data Center | Native REST 2 client and project-restricted connection; discovery, mapping API/UI/YAML and optional lifecycle/recovery with local fixtures | Actual Data Center version, project/workflow/account permissions and selected combination qualification |
| Bitbucket Cloud | Native delivery and console/YAML selection; immutable PR evidence, durable summaries and explicit review-route API/YAML with local fixtures | Personal Cloud repository/authentication/push-permission REST check passed; real Git/PR delivery and combination qualification pending; required merge rules and atomic source/destination merge protection remain unavailable |
| Bitbucket Data Center | Native REST 1.0 delivery and console/YAML selection; immutable PR evidence, durable summaries and explicit review-route API/YAML with local fixtures | Target version, corporate network/CA, account and combination qualification; complete file inventory and automatic merge remain unavailable |

Issue origin and Git destination are independent: Jira can drive work delivered
to GitHub or either Bitbucket edition; GitHub Issues can drive Bitbucket work.
Tasks must preserve both reviewed identities and their separate connections.
Existing GitHub behavior remains effective until a workflow explicitly adopts
the portable tools/configuration. No installed pilot is migrated by these local
implementation batches.

Tracker workflows are configurable. Names such as `Review`, `In Review` or a
localized equivalent are display text, not required vocabulary. A board column
may contain several statuses and is not a transition. The edition-specific
inspectors and mapping editors discover actual available transitions and store
explicitly selected instance/project/issue-type/source-status identifiers. Each
actual issue is validated again at dispatch; a sample is not a project-wide
workflow guarantee. Without a mapping, no automatic transition is attempted. Missing or unavailable transitions must expose a recoverable sync
failure without erasing task/PR delivery; whether a transition can be mandatory
is a recorded open decision, not an implicit blocking policy.

Portable `scm.pull_request.read` and `scm.pull_request.review` now provide bounded
revision reads and durable summary comments for all three native SCM adapters.
`scm.pull_request.merge` is explicitly unavailable: expected-head protection alone
does not atomically guard the approved destination against PR retargeting. A safe
provider guarantee must be qualified before automatic merge can be enabled.

The existing code-review pilot is not migrated automatically. A
[separate portable reviewer example](../examples/portable-code-review/README.md)
pins an agent/workflow version, reviews strict security, scalability, performance,
accessibility and architecture criteria, and publishes an explicit summary even
when no findings are present. Its internet research uses explicit HTTP grants.
Bounded [provider evidence](portable-review-evidence.md) distinguishes reported
checks from unavailable required merge rules. The review-route API/YAML connects
new internal `scm.pull_request_opened` facts to an already running event workflow
under reviewed scope; it never starts the workflow or grants tools. External
authenticated webhooks remain separate future work.

Local delivery now includes adapters, independent configuration, portable
review routes/example and optional Jira mappings. Follow the
[portable provider rehearsal](portable-provider-rehearsal.md) for separately
authorized real-account/target-version qualification and evidence. Fixture
coverage does not qualify a live model or provider combination. Pilot resources,
supported Data Center versions, additional authentication modes and mandatory
transition policy remain open. Both editions remain in product scope.

## Native Jira Data Center write prerequisite

Native Jira Data Center configuration uses a separate PAT, HTTPS site URL and
numeric project ID (`IssueConnection` YAML or `/api/v1/issue-connections`).
Use a technical account whose remote write permissions are restricted to that
project. Only after reviewing that restriction may the administrator record
`projectRestricted: true`; the default is false. This flag records the review,
it does not change or automatically prove Jira permissions.

Read clients cannot write. Opening a native write client additionally requires
an enabled connection and the current revision. A project preflight cannot make
a broad PAT safe: an issue can move between projects before the mutation. There
is no fallback to an MCP credential or an insecure TLS connection. Identity tests
alone do not qualify project access. Native triggers can now poll, assess
readiness and update labels/comments through the trigger-only gateway. Configure the
connection in Connections → Issue trackers, then explicitly select it in a trigger.
Changing the site, project or PAT clears the account restriction confirmation in
the form. Saving a new connection revision requires reviewing trigger consent
again. Identity checks confirm the authenticated account, not project-only
permissions. Optional status transitions are implemented through explicitly
pinned mappings; live/version qualification remains pending.

A native trigger source uses the explicit snapshot below. Its site and numeric
project must exactly match the selected enabled, confirmed native connection;
connection revision changes require reviewing trigger consent again.

```yaml
source:
  kind: jira-dc
  connectionId: jira-team
  siteUrl: https://jira.example.com/context
  projectId: '10001'
  query: 'issuetype = 10002' # Optional JQL restriction
```

Selection uses the configured label and the issue's `new` status-category key,
not the status display name. The `blocked` label and standard inward `is blocked by` links to unfinished
issues exclude an issue. Unknown link metadata fails closed; custom link-type
mappings are not provided. The owner
login controls eligibility and readiness replies; DC claims preserve existing
assignees. Labels are updated relatively, leaving unrelated labels in place.
Site/project identity fences admissions, including across credential rotation.

Jira DC's current-user permission endpoint cannot verify the privileges of the
person who relabelled an issue. A relabel alone therefore cannot reset a
readiness hold: review and resolve it in the console, then select the issue
again. An active reporter/owner's clarification comment can still drive the
bounded second readiness round. Failed-admission automatic relabel retries are
unavailable for this edition until actor-specific authorization can be proven;
retain the stopped task and use a separately reviewed new task when appropriate.

### Inspect the actual Jira workflow

In Connections → Issue trackers, choose **Inspect transitions**, enter an issue
key in the configured project and choose **Discover transitions**. Operators and
administrators can inspect; viewers cannot dispatch discovery. Results show the
issue type, current status and available transition/destination IDs, including
required fields and unavailable metadata. Equal display names are not collapsed.
Discovery is read-only and does not save mappings or change the Jira issue.
Reload after a connection revision conflict. Available transitions are specific
to the actual issue, state and technical account, not to a board column.

## Jira Cloud optional workflow profiles

Existing Jira Cloud triggers without profiles/mappings retain their comment/label
behavior. Optional transitions require an explicitly reviewed Cloud workflow
profile referencing an existing first-party MCP connection with explicitly configured
Jira REST access (or a legacy static REST-compatible credential), and its exact
revision. Configure the profile in **Connections → Jira Cloud
workflow profiles**; it stores no credential. OAuth MCP credentials are never
reused for REST. An independent REST credential can coexist with MCP OAuth;
custom endpoints and incompatible connection policies remain unsupported.

The profile's `siteUrl` is the exact gateway identity
`https://api.atlassian.com/ex/jira/<cloudId>`, not a tenant browser URL or a Data
Center endpoint. It also binds the numeric project and records the administrator's
`projectRestricted` confirmation. Automatic transitions require a technical account
whose remote write permissions are restricted to that project. The confirmation
does not change Jira permissions or make local preflight an atomic project fence.
Read-only discovery does not require that write confirmation.

Inspect a real sample issue, save a Cloud mapping, then explicitly adopt the saved
profile and mapping revisions in a trigger. The MCP connection revision, profile
revision and mapping revision are distinct. Trigger source fields
`profileId`/`profileRevision` must match the selected profile, together with its
MCP `connectionId`, `cloudId`, gateway `siteUrl` and `projectId`. Cloud opt-in
selection uses numeric project scope and stable category keys, preserving the
legacy unmapped search path. Equal local mapping names in Cloud and Data Center
remain separate authority.

See [Cloud API contracts](api.md#jira-cloud-workflow-profiles) and
[declarative Cloud configuration](declarative-configuration.md#jira-cloud-workflow-configuration).
Save dependencies before adopting their revisions; dry-run does not discover
transitions or execute writes. Saving a mapping does not attach it to a trigger.

## The model: configure once, enforce on every call

- **You connect a service once.** Weft stores the credential encrypted and
  never returns it to the browser.
- **Agents never receive the credential.** Weft adds it server-side to each MCP
  call, Git operation or API request.
- **Connecting grants nothing by itself.** Each agent still needs explicit grants
  for the exact tools it may use; task authorization modes and approvals still
  apply. See [tool permissions](tool-permissions.md) and
  [task authorization](task-authorization.md).

## Atlassian Cloud (Jira and Confluence)

Jira issue triggers use comments and label changes through the gateway.
They require a scoped API token and a reviewed source connection.
[Issue-trigger setup and permissions](task-workflows.md#issue-trigger-readiness-and-delivery)

Weft uses the official remote MCP server (`https://mcp.atlassian.com/v2/mcp`).
Choose one credential type:

| Option | Best for | What you need |
|---|---|---|
| **Personal API token** | Fastest setup for a local installation | The token owner's email and an Atlassian API token. Your organization administrator must have enabled API-token access for the MCP server. |
| **OAuth with automatic client setup (DCR)** | Interactive consent, tools not available to API tokens | Weft registers a client after you review it. Your site administrator may need to allow Weft's callback URL. |
| **OAuth with a pre-registered client** | Organizations that issue their own clients | A client from your administrator with the exact callback shown by Weft, Authorization Code + PKCE and refresh tokens. |

### Steps

The Atlassian plugin in a chat and a connection saved in Weft are independent.
Logging into the plugin does not install a credential in the local Weft server.
For a personal Cloud rehearsal using an API token:

1. Open [Atlassian Administration](https://admin.atlassian.com), select the
   organization containing your test Jira site, then open **Connectors → Atlassian
   MCP server → Authentication**. Older navigation/documentation may call this
   **Rovo MCP server**. Enable **Allow API token authentication** for that organization if
   you want this authentication method. This is an operator administration step;
   Weft does not change Atlassian authentication settings.
   The toggle saves immediately; wait until it is enabled again and visibly on.
   Do not enable enterprise managed authentication or add a domain just for this
   token path. The domain list applies to OAuth redirects, not API-token access;
   API tokens remain subject to applicable IP allowlists and their scopes.
2. Open [your account's API tokens](https://id.atlassian.com/manage-profile/security/api-tokens),
   choose **Create API token with scopes**, name it for Weft and set an expiry.
   Select **Atlassian MCP V2** (the app name observed in the account UI), then
   keep only the Jira read/search/write scopes:
   `read:jira:agent-interface`, `search:jira:agent-interface` and
   `write:jira:agent-interface`. Do not add delete/manage scopes for this rehearsal.
   The scope picker may initially select all scopes. Set **Results per page** to
   50 to review the entire list and deselect unrelated apps and permissions.
   The final review must show exactly the three Jira scopes above. For a disposable
   rehearsal, use a short expiry; the observed default was seven days.
   Atlassian may first request an emailed verification code. Complete that account
   verification before creating the token. A short-lived code is not the API
   token. Keep the generated API token out of screenshots and documentation.
3. In Weft **Connections → MCP connections → Add connection**, enter a stable
   ID/name, select **Atlassian**, and use `https://mcp.atlassian.com/v2/mcp`.
   Leave browser OAuth off for this token path. Select **Atlassian personal API
   token (Basic)**, enter **API token owner email** and **Personal API token**,
   and save. Weft constructs the authentication header; no manual base64 or
   terminal command is needed.
4. Test the connection and discover its tools. **MCP Ready** confirms MCP
   discovery only. For typed issue reads, comments, labels and optional transitions,
   configure the independent **Jira Cloud REST access** section below before
   activating a trigger.

### Independent Jira Cloud REST access

The Atlassian MCP V2 and Jira REST audiences use separate credentials. Keep the
MCP token in the connection's normal Authentication fields.

1. In the same [API-token account page](https://id.atlassian.com/manage-profile/security/api-tokens),
   choose **Create API token with scopes**, a descriptive REST-specific name and
   a short expiry. Choose the **Jira** app, not **Atlassian MCP V2**.
2. Select `read:jira-work` and, only for an approved write rehearsal,
   `write:jira-work`. These classic scopes cover the required issue operations;
   `write:jira-work` also permits other writes, including deletion. The token
   inherits the account's permissions. Scope selection is not project isolation.
   Use a project-restricted technical account for automatic transitions.
3. Edit the saved Weft MCP connection and open **Jira Cloud REST access**.
   Enable independent REST access and enter the **Jira Cloud ID**, the token
   owner's email and the new REST token. The Cloud ID is the site's UUID, not
   the project key or browser hostname. It can be obtained from the site's
   `/_tenant_info` response (`cloudId`) or the existing Atlassian resource
   listing. The resulting REST destination is fixed to
   `https://api.atlassian.com/ex/jira/<cloudId>/rest/api/3/`.
4. Save. Weft encrypts the REST email/token separately and shows only whether a
   REST credential is stored. That status does not prove remote authorization.
   Inspect transitions from a real sample issue in a Cloud workflow profile to
   exercise the REST boundary without modifying an issue.
5. Saving credentials or refreshing MCP discovery advances the connection
   revision. Review and update dependent profile pins and trigger consent before
   resuming. Do not assume an MCP tool test also tested REST.

Editing without entering replacement credentials preserves the stored pair.
Replacing it requires both email and token. Clearing it leaves the selected
Cloud ID visible but makes REST unavailable; Weft never falls back to the MCP
credential. Changing Cloud ID requires replacement or explicit clearing. Existing
connections that never selected independent REST retain their legacy static
behavior. Independent REST is supported only for first-party Jira/Atlassian MCP
endpoints without conflicting process, header or read-boundary policies.

The [Jira issue REST reference](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issues/)
and [comment REST reference](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-comments/)
describe scopes and remote permission requirements.

See [Atlassian API-token authentication](https://developer.atlassian.com/cloud/rovo-mcp/guides/configuring-authentication-via-api-token/)
for scope/administration requirements. Token values are entered only in Weft's
credential fields, never in chat, issue descriptions or repository files.

### Live Cloud token qualification: November 20, 2026

The personal rehearsal confirmed the current administration path above. Enabling
**Allow API token authentication** and saving a Basic MCP connection with only
the three Jira `agent-interface` scopes produced **Ready** and discovered 23
tools. This qualifies authentication/tool discovery, not execution of every tool.

A separate read-only request using the same credential to the Jira Cloud gateway
`/ex/jira/<cloudId>/rest/api/3/issue/<test-issue>` returned **HTTP 401** with a scope
error. The account wizard selected **Atlassian MCP V2** as one app and exposed its
agent-interface scopes separately from the **Jira** app. Do not treat the MCP
Ready state as qualification of the trigger's typed REST calls.

This exposed a trigger credential-reuse blocker. INT-009 AC-010 separates the
REST credential from MCP authentication, with independent encrypted storage and
Cloud-bound dispatch. Local fixture qualification is recorded in the specification
evidence; live REST qualification must be recorded separately. No issue mutation
or task execution was performed during the initial MCP/scope qualification.

For the other supported connection choices:

1. Open **Connections**, select **New connection** and choose the Atlassian
   service (or its template, when offered).
2. Under **Credential type**, choose the option:
   - **Personal API token:** enter the email and token together and save.
   - **OAuth:** review the scopes, then select **Connect** and approve in the
     Atlassian page. Finish in the same browser.
3. Let Weft discover the tools. It lists individual operations and hides the generic
   `executeRead`, `executeWrite` and `executeDestructive` dispatchers, which could
   route one grant to many operations ([why](mcp.md#atlassian-dispatcher-restrictions)).
4. Optionally restrict reads to Jira or Confluence and to specific sites
   ([read boundaries](mcp.md#atlassian-read-boundaries)).
5. In the graph editor, assign each agent only the operations it needs, for
   example issue read and search for a planner, and a comment write for the agent
   that reports progress.

### Scopes to start with

Request read and search first; add writes only for the operations you grant.

| Product | Start with | Add only when needed |
|---|---|---|
| Jira | `read:jira:agent-interface`, `search:jira:agent-interface` | `write:jira:agent-interface` |
| Confluence | `read:confluence:agent-interface`, `search:confluence:agent-interface` | `write:confluence:agent-interface` |

Leave delete and manage scopes out. The full table is in
[MCP integration contracts](mcp-integrations.md#shared-atlassian-cloud-contract).

### What your administrator may need to do

Company Atlassian sites often restrict integrations. Depending on the option, ask
your site or organization administrator to:

- enable API-token access for the Atlassian MCP server, or
- allow Weft's OAuth callback URL (shown on the connection page), and
- allow the outbound IP of the machine running Weft, if IP allowlisting is on.

Weft never changes these settings. Until they are approved, the connection
stays pending and the error explains what was refused.

### Try it without company approval

Use an operator-controlled Atlassian Cloud test site, review its API-token
policy, and connect it as above. A configured connection alone does not qualify
the issue-to-PR/review/synchronization flow. Exercise the selected operations and
record the observed results using the [rehearsal guide](portable-provider-rehearsal.md).

## Bitbucket Cloud delivery (local adapter)

The SCM API/YAML kind is `bitbucket-cloud`, with repository `workspace/slug`.
The default REST base is `https://api.bitbucket.org/2.0`; the default Git remote
is `https://bitbucket.org/workspace/slug.git`. New Cloud connections can use an
Atlassian email and scoped API token (`authType: api-token`, `username: <email>`).
REST uses Basic authentication with the email; Git HTTPS uses the documented
static username `x-bitbucket-api-token-auth`. Both credentials stay server-held.
Omitting `authType`, or selecting `bearer`, preserves existing Bearer behavior.
There is no automatic fallback between authentication modes or automatic OAuth
token acquisition/refresh. Repository/workspace access tokens remain separately
unqualified.

### Create and enter a scoped API token

1. Open [Atlassian account API tokens](https://id.atlassian.com/manage-profile/security/api-tokens),
   select **Create API token with scopes**, name it for Weft, and choose an expiry.
2. Select **Bitbucket** and grant `read:repository:bitbucket`,
   `write:repository:bitbucket`, `read:pullrequest:bitbucket` and
   `write:pullrequest:bitbucket`. These cover the repository/permission checks,
   Git push and PR read/create/comment operations used by this rehearsal. Scopes
   do not give the account permissions it lacks, and write scopes do not imply
   read scopes. No repository administration or deletion scope is needed.
3. In Weft, open **Connections → Source control → Add connection**, select
   **Bitbucket Cloud**, and enter `workspace/repository-slug`.
4. Select **API token**, enter the token owner's Atlassian email and paste the
   token into the password field. Keep **Opening a pull request needs a person's
   approval** selected, then save and use **Test connection**.
5. A new repository also needs an initial commit and a default branch before
   it can be used for a task. A successful connection check is read-only; the
   rehearsal separately verifies actual Git push and PR/comment permissions.

Never put the token in a repository URL, prompt, issue, command line or chat.
Re-enter a replacement token when changing API-token mode/account/repository/API
or Git destination. Unchanged settings can retain the encrypted token; legacy
Bearer-to-Bearer destination-update behavior is retained. Installation YAML uses
the same `authType`/`username` fields and a `secretRef`, never a literal token.
See [Atlassian token creation](https://support.atlassian.com/bitbucket-cloud/docs/create-an-api-token/),
[REST and Git authentication](https://support.atlassian.com/bitbucket-cloud/docs/using-api-tokens/)
and [repository permission scopes](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-repositories/#api-user-workspaces-workspace-permissions-repositories-get).

Equivalent declarative input (review its dry-run before applying):

```yaml
apiVersion: weft.io/v1
kind: SCMConnection
metadata:
  name: team-bitbucket-cloud
spec:
  kind: bitbucket-cloud
  repository: workspace/repository-slug
  authType: api-token
  username: operator@example.com
  branchPrefix: weft/
  requirePullRequestApproval: true
  secretRef: {env: WEFT_SECRET_BITBUCKET_CLOUD}
```

The environment reference is an operator-supplied secret source, not a request to
write a token into `.env`. An unchanged existing connection can instead use
`secretRef: {preserve: true}`. That cannot authorize reuse after changing the
API-token credential scope; supply a reviewed replacement secret for that change.

Connection testing reads the configured repository and the caller's effective
workspace-scoped repository permission. It does not attempt a write. PR lookup
checks both branches and repositories so a fork with the same branch name cannot
be returned as the task PR. Queries are bounded to ten pages and 2 MiB per
response; incomplete/malformed results stop creation. Redirects and pagination
outside the reviewed resource collection are refused. A failed create is
reconciled by lookup and is never blindly replayed by the adapter.

HTTPS and normal certificate verification are required for remote API addresses;
HTTP is accepted only for loopback contract fixtures. No real-account result is
claimed. The console exposes this adapter with its edition-specific credential
guidance. Portable review evidence and summary operations have local fixture
coverage; automatic merge remains unavailable pending its independent atomic
source-and-destination concurrency guarantee.
Upstream contracts: [repository and workspace permissions](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-repositories/),
[pull requests](https://developer.atlassian.com/cloud/bitbucket/rest/api-group-pullrequests/).

## Bitbucket Data Center delivery (local adapter)

The native `bitbucket-dc` SCM adapter uses REST 1.0 and an HTTP access token
sent as Bearer for API and Git calls. Enter the site base URL (including its
context path if any), not `/rest/api/1.0`. Repository format is `PROJECT/slug`;
personal projects (`~user/slug`) are not yet supported. The default Git URL is
`<site>/scm/<project>/slug.git`; an explicitly reviewed Git URL can override it.
Permission checks search the exact repository identity under `REPO_READ` and
`REPO_WRITE`; branch restrictions remain enforced by the remote on push.

Local fixtures cover the documented REST 1.0 shapes, context paths, nonsequential
pagination, forks, denied/malformed responses and conflict reconciliation. No
installed version or real account is qualified. The
[REST contract reference](https://docs.atlassian.com/bitbucket-server/rest/7.15.3/bitbucket-rest.html)
is fixture provenance, not a supported-version or production recommendation.
A per-connection custom CA field is not implemented. The adapter uses the
server's normal verified TLS trust and never disables verification.

### Connection steps

1. Open **Connections → Source control → Bitbucket Data Center**.
2. Enter the base URL, for example `https://bitbucket.example.com`.
3. Paste an **HTTP access token**. Prefer a **repository** or **project** access
   token with the smallest permission that works:
   - *Read* to clone and fetch;
   - *Write* to push task branches and open pull requests.
   Personal tokens work too, but they carry everything the person can do.
4. There is no per-connection custom CA field. The Weft server's API and Git
   processes must already trust the reviewed internal certificate authority;
   certificate verification stays on.
5. Select **Test connection**. Weft checks read/write access to the configured repository
   and reads its default branch. This does not push or open a pull request.

### How the token is used

- Clone, fetch and push run over **HTTPS** with the token added by Weft in
  memory, never in Git configuration, remote URLs, command arguments, environment
  variables visible to agents, or logs. SSH keys are not needed.
- Pull requests are opened through the Bitbucket Data Center REST API by the
  gateway tool, not by a CLI inside the sandbox.
- Pushes are limited to the task's own branch prefix; the base branch and force
  pushes are refused. Opening a pull request requires approval by default.

### Network

Bitbucket Data Center usually sits on a private network. A local Weft reaches
it only when the machine does (for example through a VPN). Git operations run
in Weft's own Git process and do not require granting network access to the agent
sandbox. Sandbox network access remains governed by the reviewed installation
and workflow policy.

### Try it without company access

Use an operator-approved disposable Bitbucket Data Center test instance. Record
its actual version and reviewed account/token contract, create an approved test
repository, and exercise the selected operations. Follow the
[rehearsal guide](portable-provider-rehearsal.md); this does not establish a
supported version range or qualify another company instance.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Atlassian OAuth fails right after consent | The callback URL is not allowed for your site, or the browser changed between Connect and callback. |
| Atlassian API token rejected | API-token access is not enabled for the organization, or the email and token do not belong together. |
| Discovery works but a call is denied | The agent has no grant for that exact operation, or the scope was not requested. |
| A Jira or Confluence call fails with a site error | The operation needs a `cloudId` and the site is outside the configured read boundary. |
| Bitbucket test connection fails with a certificate error | Review the Weft server's TLS trust for the private CA and the certificate chain. Git operations also need their normal verified trust configuration. Per-connection custom CA entry is not implemented; never disable verification. |

### Portable automatic merge availability

`scm.pull_request.read` and `scm.pull_request.review` support the registered SCM
adapters. Automatic merge is currently unavailable in the portable gateway
(`automaticMerge: false`). An expected-head-only mutation leaves a PR retarget
race; a preflight or readback cannot prevent a merge into a changed destination.
The merge tool returns an explicit error without calling the provider. Use the
provider's separately authorized review/merge process until atomic head and
destination protection is qualified. This does not change existing workflow
versions, MCP grants or the GitHub pilot.

### Routing a delivered PR to portable review

The `scm.pull_request_opened` event includes provider, API instance, connection
name and revision, repository, PR number/URL and head/base commit IDs. Route only
events with `reviewRevisionAvailable: true` to revision-bound review. This flag
indicates identity completeness, not passing checks or PR eligibility.
Pass `connectionRevision` to `scm.pull_request.read` and
`scm.pull_request.review`; refuse events whose connection has been reconfigured.
The read result also includes the revision for subsequent summary publication.
Legacy delivery events/records remain readable; missing revision identity must
be resolved explicitly before automatic review. Nothing grants tools or changes
a saved workflow automatically. Repeated summary publication reconciles the
same connection/repository/PR/revision receipt without another comment POST.

Configure routes through the [review-route API](api.md#portable-internal-pull-request-review-routes)
or [PullRequestReviewRoute YAML](declarative-configuration.md#portable-pull-request-review-routes).
Activation creates a durable sequence boundary; earlier events are excluded.
Published route receipts mean engine acceptance, possibly held for approval,
not a completed review or confirmed summary. Unknown dispatches are never
automatically repeated. The paginated receipt API supports read-only operator
reconciliation and admin retry only for a known pre-dispatch failure. Follow the
[portable example](../examples/portable-code-review/README.md) for separate
adoption and explicitly retire an old bridge before routing the same scope.

### Optional Jira lifecycle mappings and recovery

For Data Center use the native connection; for Cloud use its reviewed profile.
Use its **Inspect transitions** page with an actual sample issue,
then create a **Workflow mapping** for its numeric issue type/source status,
transition and destination. Names such as Review or In Review are presentation;
no board column or alias is guessed. The current profile accepts transitions
without required field updates. Missing field metadata and required fields,
even those reporting defaults, need manual handling.

Pin the desired mapping name/revision to workStarted or prDelivered in the
trigger's **Optional tracker transitions** fields (or lifecycleMappings configuration). Saving a mapping does not attach it
to a trigger. Each actual issue is checked again before dispatch. No mapping
means no transition, preserving existing comment/label behavior. Automatic
confirmed-merge synchronization is unavailable while portable merge is disabled.

Runtime admission after approval/preparation and the persisted PR-opened event
are separate milestones. Optional synchronization has its own durable history;
failed Jira writes do not erase task/PR success. Reconcile unknown results by
reading before considering any further action. Explicit retry is supported only
for a known failure before dispatch, with the original reviewed scope unchanged.
Changed mapping/connection revisions require review; they never authorize a
blind re-send. A person can finish handling the issue manually when its workflow
no longer matches the pinned definition. Live version/workflow qualification and
the project-restricted technical account remain operator prerequisites.

From the connection's **Workflow mappings** page, **Tracker synchronization**
shows separate task/stage outcomes. Expand **Review recovery** to inspect the
source issue and retained source/target status IDs before acting. Operators can
reconcile uncertain outcomes by reading; admins can retry confirmed preflight
failures under the original authority. No intent means the destination is not
inferred from today's mapping. Page controls bound history; search/status filters
apply only to the current page. A revision conflict requires refresh and review.


### Atlassian OAuth client authentication

For a fresh DCR registration, the server-side Weft client prefers advertised
`client_secret_basic`, then `client_secret_post`, only for the selected Atlassian
service and canonical Atlassian MCP/issuer/authorization/token URLs. Setup preview
shows the chosen method. Returned credentials must satisfy the same strict
method, callback, scope, size and expiry checks; the client secret is encrypted
and stays on the server. Existing manual registrations and public CIMD are not
changed. This follows the client metadata and confidential-client distinction in
[RFC 7591 sections 2 and 3.2.1](https://www.rfc-editor.org/rfc/rfc7591.html).

An HTTP 201 with `registrationDiagnostic.fields: ["client_secret"]` means only
that credential validation failed; it does not expose or establish the returned
value. Use the separately confirmed uncertain-registration recovery flow before
another reviewed attempt. Do not bypass the response checks or automatically
repeat registration. Real consent and a scoped Jira tool call still need live
qualification after registration succeeds.
