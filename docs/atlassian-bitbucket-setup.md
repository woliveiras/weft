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

| Product/edition | Current boundary | Planned work |
| --- | --- | --- |
| Jira Cloud | Issue reference, polling/readiness, comments and labels have implementation paths; live qualification remains separate | Qualify the full flow and add optional discovered workflow mappings |
| Jira Data Center | Native REST 2 client, connection API/YAML, read-only transition discovery and scoped issue-trigger lifecycle and console configuration have local fixture coverage | Optional mappings, transition reconciliation and live qualification |
| Bitbucket Cloud | Native delivery, console/YAML selection, immutable PR reads and durable summary comments have local fixture coverage | Event/workflow adoption and live qualification; automatic merge remains unavailable |
| Bitbucket Data Center | Native REST 1.0 delivery, console/YAML selection, immutable PR reads and durable summary comments have local fixture coverage | Event/workflow adoption, corporate network/CA and target-version qualification; automatic merge remains unavailable |

Issue origin and Git destination are independent: Jira can drive work delivered
to GitHub or either Bitbucket edition; GitHub Issues can drive Bitbucket work.
Tasks must preserve both reviewed identities and their separate connections.
Existing GitHub behavior remains effective until a workflow explicitly adopts
the planned tools/configuration.

Tracker workflows are configurable. Names such as `Review`, `In Review` or a
localized equivalent are display text, not required vocabulary. A board column
may contain several statuses and is not a transition. The planned editor must
discover actual statuses/available transitions and store explicitly selected,
instance/workflow-scoped identifiers. Without a mapping, no automatic transition
is attempted. Missing or unavailable transitions must expose a recoverable sync
failure without erasing task/PR delivery; whether a transition can be mandatory
is a recorded open decision, not an implicit blocking policy.

Portable `scm.pull_request.read` and `scm.pull_request.review` now provide bounded
revision reads and durable summary comments for all three native SCM adapters.
`scm.pull_request.merge` is explicitly unavailable: expected-head protection alone
does not atomically guard the approved destination against PR retargeting. A safe
provider guarantee must be qualified before automatic merge can be enabled.

The code-review pilot still uses GitHub MCP tools. Planned Weft review operations
must read PRs/diffs/checks, post summaries/findings and guard merges through the
selected SCM adapter. A clean review still publishes a summary; exact reviewed
head protection and remote repository rules are required for automatic merge.
The existing internal `scm.pull_request_opened` event can drive Weft-created PR
reviews; external authenticated webhooks remain separate future work.

Delivery proceeds through adapter contracts/fixtures, edition-specific adapters,
independent configuration and portable review, optional Jira mappings, then
authorized end-to-end rehearsals. Pilot order, supported Data Center versions,
authentication modes, test resources and mandatory-transition policy remain
open. Both editions remain in product scope regardless of pilot order.

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
permissions. Optional status transitions and live/version qualification remain
pending.

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

Create a free Atlassian Cloud site, enable API-token access there, and connect it
as above. This qualifies the whole flow on a site you control before asking for
company access.

## Bitbucket Cloud delivery (local adapter)

The SCM API/YAML kind is `bitbucket-cloud`, with repository `workspace/slug`.
The default REST base is `https://api.bitbucket.org/2.0`; the default Git remote
is `https://bitbucket.org/workspace/slug.git`. The initial credential contract is
an existing user OAuth Bearer access token with account/repository read access
for identity/permission checks and the appropriate repository/PR write scopes
for delivery. Token acquisition/refresh, email-plus-API-token Basic authentication
and repository/workspace access-token qualification are not supplied by this
adapter. Tokens are still entered through the reviewed secret mechanisms.

Connection testing reads the configured repository and the caller's effective
workspace-scoped repository permission. It does not attempt a write. PR lookup
checks both branches and repositories so a fork with the same branch name cannot
be returned as the task PR. Queries are bounded to ten pages and 2 MiB per
response; incomplete/malformed results stop creation. Redirects and pagination
outside the reviewed resource collection are refused. A failed create is
reconciled by lookup and is never blindly replayed by the adapter.

HTTPS and normal certificate verification are required for remote API addresses;
HTTP is accepted only for loopback contract fixtures. No real-account result is
claimed. The console exposes this adapter with its edition-specific credential guidance. Review/merge operations remain a separate batch.
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
Custom CA configuration remains separate work; the adapter
uses the server's normal verified TLS trust and never disables verification.

### Proposed steps

1. Open **Connections → Source control → Bitbucket Data Center**.
2. Enter the base URL, for example `https://bitbucket.example.com`.
3. Paste an **HTTP access token**. Prefer a **repository** or **project** access
   token with the smallest permission that works:
   - *Read* to clone and fetch;
   - *Write* to push task branches and open pull requests.
   Personal tokens work too, but they carry everything the person can do.
4. Custom CA entry is planned. Until available, the Weft server must already
   trust the internal certificate authority; certificate verification stays on.
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
it only when the machine does (for example through a VPN). Sandboxes stay without
network access: Git operations run in Weft's own Git process, not inside the
agent's sandbox.

### Try it without company access

Run Bitbucket Data Center locally with an evaluation licence in a container,
create a test repository and a repository access token, and connect it as above.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Atlassian OAuth fails right after consent | The callback URL is not allowed for your site, or the browser changed between Connect and callback. |
| Atlassian API token rejected | API-token access is not enabled for the organization, or the email and token do not belong together. |
| Discovery works but a call is denied | The agent has no grant for that exact operation, or the scope was not requested. |
| A Jira or Confluence call fails with a site error | The operation needs a `cloudId` and the site is outside the configured read boundary. |
| Bitbucket test connection fails with a certificate error (proposed) | The server uses an internal CA; add it to the connection. |

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


### Optional native Jira DC lifecycle mappings

Use the connection's **Inspect transitions** page with an actual sample issue,
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
