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
| Bitbucket Data Center access with an HTTP access token | **Proposed, not implemented** |

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

## Bitbucket Data Center (proposed)

Bitbucket Data Center has no official remote MCP server, and its credentials are
not interchangeable with Atlassian Cloud ones. The proposed setup is a dedicated
SCM connection, used by the planned `git.push` and `scm.pull_request.create`
gateway tools through an `SCMProvider` adapter.

### Proposed steps

1. Open **Connections → Source control → Bitbucket Data Center**.
2. Enter the base URL, for example `https://bitbucket.example.com`.
3. Paste an **HTTP access token**. Prefer a **repository** or **project** access
   token with the smallest permission that works:
   - *Read* to clone and fetch;
   - *Write* to push task branches and open pull requests.
   Personal tokens work too, but they carry everything the person can do.
4. Optionally provide a custom CA certificate when the server uses an internal
   certificate authority.
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
