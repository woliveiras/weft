# Priority MCP integration contracts

Research date: 2026-10-17; implementation reconciliation: 2026-10-18. These are source-confirmed vendor contracts and planned
Weft deliveries. Implemented compatibility is identified below; this is not
live-service certification.
The current [generic client](mcp.md) supports public HTTPS Streamable HTTP,
protocol 2025-06-18 and optional Bearer credentials. Custom connections can explicitly select the bounded stateless 2026-07-28 profile; it does not imply complete warehouse compatibility. It rejects URL queries except the exact reviewed Atlassian v2 `tools=all` option,
redirects and private destinations. Generic pre-registered OAuth now supports the
browser lifecycle; see [implementation and limits](mcp.md#browser-oauth-pre-registered-clients).
GitHub, Atlassian, GitLab and custom setup profiles are available for pre-registered
clients, plus reviewed DCR/CIMD setup where advertised; [registration and evidence](mcp.md#service-setup-and-independent-connections)
distinguish fixture coverage from pending live/corporate qualification. Typed encrypted Atlassian Basic and standalone LangSmith credentials are also implemented; arbitrary credential headers are not supported.
See also the [additional template survey](mcp-templates.md).

## GitHub

The official remote endpoint is `https://api.githubcopilot.com/mcp/`, using HTTP
MCP with Bearer PAT or OAuth. A host implementing OAuth must configure its own
GitHub App or OAuth App. Enterprise Cloud with data residency has a different
host; GitHub Enterprise Server requires separately hosted server infrastructure
and is outside the initial github.com acceptance. [Official server](https://github.com/github/github-mcp-server)

Remote toolset paths and `/readonly` allow narrower catalogues. Prefer a reviewed
read-only profile before enabling write tools; toolsets do not replace token
permissions or gateway grants. [Remote configuration](https://github.com/github/github-mcp-server/blob/main/docs/remote-server.md)

There is no single minimum scope for every tool. For selected repositories, map
read operations to fine-grained Contents, Issues, Pull requests and Actions read
permissions as applicable; enable corresponding write permissions only for the
chosen mutations. Review each underlying API endpoint, including any additional
permission requirements. [GitHub permission reference](https://docs.github.com/en/rest/authentication/permissions-required-for-fine-grained-personal-access-tokens)
Classic PAT scopes such as `repo` and `read:org` are broader alternatives, not the
default minimum. Discovery cannot prove authority: fine-grained tokens do not
support the server's classic-scope filtering. [Scope filtering](https://github.com/github/github-mcp-server/blob/main/docs/scope-filtering.md)

Implemented OAuth compatibility: the GitHub.com profile handles omitted client
authentication metadata, confidential POST exchange/renewal, comma-separated scopes
and OAuth errors returned with HTTP 200. Unit, TLS API integration and browser
fixtures cover the contract. Follow the [manual app registration guide](mcp.md#registering-githubcom-oauth-for-a-local-weft); live consent remains pending.

The guided OAuth setup and PAT/read-only toolset profiles are implemented.
Remaining delivery: a live-qualified per-tool permission matrix and resource
selection with an honest distinction
between UI defaults and enforced token restrictions; discovery and read-only
smoke test; separately approved issue/PR write tests. Diagnose token expiry,
organization approval/SSO and policy denials without exposing credential values.

### Read-permission review worksheet

Reviewed against official endpoint documentation on 2026-10-18. This is a
starting worksheet for underlying API permissions, **not a live-tested MCP
permission matrix**. Check the discovered MCP schema and the server version's
implementation before assigning a permission to a specific tool/method.

| Intended operation | Fine-grained repository permission to review | Source |
| --- | --- | --- |
| Read repository file/directory | Contents: read | [Repository contents](https://docs.github.com/en/rest/repos/contents#get-repository-content) |
| Read an issue | Issues: read or Pull requests: read, depending on endpoint/resource | [Get an issue](https://docs.github.com/en/rest/issues/issues#get-an-issue) |
| Read a pull request | Pull requests: read | [Get a pull request](https://docs.github.com/en/rest/pulls/pulls#get-a-pull-request) |
| Read a workflow run | Actions: read | [Get a workflow run](https://docs.github.com/en/rest/actions/workflow-runs#get-a-workflow-run) |

Select the credential's resource owner and permitted repositories first. A
successful public read does not establish access to a private repository or
prove that an excluded repository is denied. Organization approval, SSO policy,
expiry and token type can independently restrict calls. A `404` is not evidence
that the resource does not exist. Do not expand scopes automatically to solve a
failure, and do not use a read-only toolset label as a credential boundary.

Before closing qualification, run each chosen MCP operation with a dedicated
test credential that permits only its reviewed resources, then repeat against
an excluded test resource. Record connection/template revision, exact tool and
method, expected permission and sanitized outcome. Keep write tests separate
and obtain approval for the exact external test target. No such live permission
matrix was executed by this documentation change.

## Shared Atlassian Cloud contract

Use `https://mcp.atlassian.com/v2/mcp` with remote HTTP MCP. Bitbucket Cloud is
supported when its workspace is linked to an organization. For a gateway, the
vendor offers `https://mcp.atlassian.com/v2/mcp?tools=all` for flat paginated
discovery. The legacy `/v1/sse` endpoint is no longer the onboarding target.
[Official server](https://atlassian.github.io/atlassian-mcp-server/)

Authentication options are OAuth 2.1, personal scoped API tokens via
`Authorization: Basic <base64(email:api_token)>`, or service-account API keys via
`Authorization: Bearer <api_key>`. Token authentication requires organization
admin enablement. API tokens are not bound to one cloud ID; IP restrictions still
apply. They are not interchangeable with an OAuth access token or an arbitrary
Bitbucket REST credential. [API-token contract](https://support.atlassian.com/atlassian-ai-gateway/docs/configure-authentication-via-api-token/)
OAuth grants product/site access through consent and sends a Bearer access token;
redirect/domain policy and renewed consent must be handled. Runtime authorization
metadata, registration support and refresh behavior must be validated during
implementation rather than assuming the generic Atlassian REST OAuth flow.
[OAuth contract](https://support.atlassian.com/atlassian-ai-gateway/docs/configure-oauth-2-1/)

The following are literal MCP permission scopes, not ordinary REST API scope
names. Request only those needed for the chosen profile:

| Integration | Read/search scopes | Optional mutation scopes |
| --- | --- | --- |
| Jira | `read:jira:agent-interface`, `search:jira:agent-interface` | `write:jira:agent-interface`; exclude `delete:jira:agent-interface` and `manage:jira:agent-interface` from initial defaults |
| Confluence | `read:confluence:agent-interface`, `search:confluence:agent-interface` | `write:confluence:agent-interface` |
| Bitbucket Cloud | `read:bitbucket:agent-interface` | `write:bitbucket:agent-interface` |

The default catalogue also exposes `discover` and dispatcher tools
`executeRead`, `executeWrite`, `executeDestructive`. Weft uses the flat catalogue for exact per-operation grants and rejects
the broad dispatchers during discovery and invocation, including legacy entries. Discover accessible resources and
resolve `cloudId` as required. [Supported tools and scopes](https://support.atlassian.com/atlassian-ai-gateway/docs/supported-tools/)

Implemented on 2026-10-18: the exact `tools=all` endpoint with bounded pagination,
unknown/secret query rejection and canonical/advertised OAuth resource binding.
TLS fixtures and browser tests cover this route; live acceptance remains pending.
Encrypted Basic credentials, dispatcher protection and an optional finite
Jira/Confluence read policy with explicit cloud UUIDs are implemented. The policy
filters the catalogue and checks the site argument before dispatch; it does not
enforce Jira projects or Confluence spaces. See the [bounded read policy](mcp.md#atlassian-read-boundaries).
Remaining work: qualify actual protocol version, schema/name compatibility and
catalogue size against current bounds. Do not relax limits silently. Show site,
project, workspace and space selections as defaults unless a verified policy
actually restricts arguments. User permissions and Weft grants both apply.
Jira/Confluence/Bitbucket Data Center are separate future hosting contracts.

## Per-product acceptance and deliveries

| Integration | Planned guided flow | Required product-specific evidence |
| --- | --- | --- |
| GitHub | Select github.com, credential mode, repository/tool profile and exact agent grants | Read selected repository/issue/PR; deny inaccessible repository; approved write in test repository; expiry, scope denial and disable during approval wait |
| Bitbucket Cloud | Verify organization-linked workspace, select workspace/repository and read or reviewed write profile | Repository/file/PR read; unlinked workspace failure; cross-workspace denial; approved test PR/comment; no pipeline execution or merge granted implicitly |
| Jira Cloud | Select accessible site/project; explain read/search versus write scopes | Issue/JQL read; project permission denial and missing scope; approved test issue/comment; field/transition validation and no delete/manage by default |
| Confluence Cloud | Select accessible site/space; explain read/search versus write scopes | Page/search read; restricted-page denial; approved test page update, version conflict and permission failure; returned attachment links remain unfetched |

Every row requires unit tests, real-boundary integration tests and browser E2E.
Fixtures cover authentication failures, protocol errors, pagination, cancellation,
redaction, stale revisions and approval rejection. Opt-in live tests require
operator-provided credentials and authorized test resources; capture sanitized
protocol/catalogue evidence and distinguish fixture coverage from live acceptance.
Never replay a possibly completed write automatically after a lost response.

## Shared completion criteria

Version templates and record the reviewed source/date; guide credential setup,
show granted access, discover tools, authorize each agent call and support
revocation. Deletion, per-connection quotas and a filterable call/approval/outcome
history are implemented without exposing secrets or sensitive payloads. OAuth state,
PKCE, callback/issuer validation, encrypted tokens, refresh concurrency and
reauthorization errors are implemented with fixture coverage. Do not forward credentials across hosts or use a template
to bypass the existing network policy. Additional-header support belongs to the
broader template backlog; it is not a prerequisite for the initial PAT GitHub path.

The survey is complete at documentation level. OAuth registration/refresh details,
exact tool-to-API permission mappings and live acceptance are explicit remaining
implementation qualifications. The additional candidates do not expand Release 1
acceptance automatically.


## GitLab MCP (instance-specific)

The profile derives `/api/v4/mcp` from a selected HTTPS instance origin and requests
`mcp`. The official MCP guide now documents pre-registered public applications,
including UI support from 19.3; legacy REST/PAT `api` permissions are not equivalent.
The scoped public-PKCE metadata compatibility rule and registration instructions
are described in [the setup guide](mcp.md#gitlab-instance-setup). Exact instance,
version, group/instance enablement, refresh/revocation and live tool authorization
remain external acceptance requirements. DCR is available after explicit setup review;
CIMD is selected only if the instance advertises it and the installation can host
a public HTTPS document. Manual registration remains available. See
[automatic setup and limits](mcp.md#automatic-client-setup-dcr-and-cimd).

[Official GitLab MCP contract](https://docs.gitlab.com/user/model_context_protocol/mcp_server/).
