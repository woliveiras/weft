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

## Planned portable integrations

The [INT-009 specification](../specs/providers-and-integrations/portable-issue-and-source-control/spec.md)
records the next scope; this is not a setup procedure for available adapters.

| Product/edition | Current boundary | Planned work |
| --- | --- | --- |
| Jira Cloud | Issue reference, polling/readiness, comments and labels have implementation paths; live qualification remains separate | Qualify the full flow and add optional discovered workflow mappings |
| Jira Data Center | No implemented tracker adapter | Separate instance/authentication/API adapter and qualification |
| Bitbucket Cloud | Native delivery adapter has local HTTP/Git fixture coverage; console selection and portable review remain pending | Live qualification and portable PR review/merge capabilities |
| Bitbucket Data Center | Native REST 1.0 delivery adapter has local HTTP/Git fixture coverage | Console configuration, private-network/TLS and target-version qualification |

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
claimed. Console availability and review/merge operations are separate batches.
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
Custom CA configuration and console selection are separate work; the adapter
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
5. Select **Test connection**. Weft reads the token's identity and lists the
   repositories it can see.

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
