# MCP connections and agent permissions

The connection list opens dedicated Overview, Tools, Usage & history and Settings
pages. Use Settings to edit an existing connection and Overview for OAuth actions.
See [Console navigation](interface-navigation.md) for routes and legacy compatibility.

MCP supports registration, discovery and remote tool invocation through the gateway.
Native agents and external runtime callbacks use the same authorization boundary.
Models receive only the currently available tools explicitly granted to their agent.

In **Connections → Add connection**, an administrator can create or edit multiple
connections, set a bearer credential, and disable/re-enable a connection. IDs are
stable; edits require the current revision. A blank credential preserves the
stored value; explicit clear removes it. Changing the endpoint requires replacing
or clearing an existing credential so it cannot be forwarded accidentally.
Credentials use the installation's existing AES-GCM secret box and are bound to
the connection ID. Read responses expose only `credentialConfigured`.

### Removing a connection

Disable the connection first. For OAuth, revoke access and wait for the local
revocation result before deleting. The admin-only **Delete** action reviews the
consequences and requires typing the connection ID. The API uses
`DELETE /api/v1/mcp/connections/{id}` with the current `If-Match` revision;
stale requests fail with 409, missing revisions with 400, and unmet prerequisites
with 422. Reload and review again after a conflict.

Deletion removes the local configuration, catalogue and encrypted credentials.
A minimal tombstone reserves the ID permanently, preventing old graph grants
from authorizing a replacement connection. Use a new ID for a new connection.
Existing graph definitions and audit history remain; obsolete grants are visible
as unavailable and can be removed in the editor. Pending OAuth transactions are
invalidated and stale writes cannot restore deleted settings. Already dispatched
operations cannot be rolled back. Deletion emits `mcp.connection.deleted` with
safe actor/revision metadata. It does not delete remote OAuth apps/registrations,
guarantee upstream revocation, or erase historical copies in installation backups.

### Session recovery

A request carrying a session ID that receives HTTP 404 triggers one fresh
initialization without the old ID, within the original 15-second operation
budget. Discovery discards partial pages and starts again. Initialization failure
without a session, authentication failures, other HTTP failures and ambiguous
network/SSE disconnections are not retried. If `tools/call` was already sent,
Weft can reinitialize but never resends that call; the gateway reports unknown
remote outcome and records a failed invocation. A subsequent explicit invocation
uses a new session and checks current agent grants/connection state again.
Persistent SSE event cursor resumption is not implemented.

Operators and administrators can test or refresh a connection. In this increment,
both actions initialize a new MCP session and refresh the tool catalogue. The
server negotiates protocol `2025-06-18`, sends `notifications/initialized`, and
reads every supported `tools/list` page. JSON and SSE POST responses are supported.
Session identifiers stay in memory and are terminated after discovery where the
remote server supports DELETE. Server-requested client operations are not executed.

The supported transport is remote **Streamable HTTP over HTTPS**, following the
[MCP transport specification](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports)
and [tool discovery contract](https://modelcontextprotocol.io/specification/2025-06-18/server/tools).
TLS verification is enabled. Redirects and environment HTTP proxies are disabled;
DNS answers are validated at connection time and the validated IP is dialled
directly. Private, loopback, link-local and reserved addresses are denied. No
local server processes or arbitrary commands are launched. Endpoint URLs cannot
contain user information or fragments. Queries are rejected except for the exact
reviewed Atlassian v2 `tools=all` option described below.

Discovery has a 15-second deadline, a 2 MiB response/aggregate catalogue limit,
at most 20 pages and 500 tools. Tool names currently accept only ASCII letters,
digits, underscores and hyphens, up to 128 characters. Schemas must be JSON objects
with `type: object`; descriptions are bounded to 8 KiB. Duplicate names, repeated
cursors, unsupported protocol versions and malformed responses fail discovery.
Errors exclude upstream payloads; catalogues containing the supplied bearer value
are rejected. A failed refresh clears the selectable catalogue and reports
`unavailable`; existing grants remain visible in the graph and can be removed.
Changing endpoint, credential or enabled state also invalidates discovery.

In an agent's **MCP permissions** section, select exact
`mcp.<connectionId>.<toolName>` grants and save the graph. Validation rejects
unknown, wildcard and disabled MCP grants. Existing non-MCP grants and their
restrictions are preserved. The selector reads `/api/v1/mcp/tools`; this catalogue
is separate from `/api/v1/tools`, which describes the static built-in registry.
The runtime combines both catalogues when preparing model schemas.

Registry changes and discovery produce `mcp.connection.changed`,
`mcp.connection.tested` and `mcp.tools.refreshed` events with actor, connection ID,
revision/status and discovery latency, excluding credentials, endpoint payloads
and tool arguments. Concurrent edits and refreshes use storage compare-and-swap;
a delayed refresh cannot restore a disabled connection.

Every call requires an exact grant; `*`, capability grants and `mcp.*` do not grant
MCP access. Authorization uses the execution's agent principal, not model-supplied
identity. Graph edits apply to subsequent graph runs under the existing execution
snapshot contract. Disabling a connection, changing its endpoint/credential or
refreshing its catalogue is observed on subsequent calls, even in existing runs.
The connection revision is checked again after any approval or preparation wait.
Already dispatched remote operations are not rolled back by disabling a connection.

The existing `requiresApproval` and `rateLimit` grant fields apply to MCP and can
be set in the graph definition. The MCP checkbox selector preserves these fields. Approval
requests show arguments for review; ordinary tool audit events omit MCP arguments
and results, retaining the qualified tool name, agent/execution, outcome and latency.
Metric tool labels aggregate MCP calls under `mcp`. Host constraints restrict the
registered endpoint; filesystem path/command constraints are rejected for MCP.

Each call initializes a fresh session and sends one `tools/call` with the unqualified
remote name and a JSON object (maximum 64 KiB). Calls share the discovery network
policy, a 15-second operation timeout and 2 MiB response bound; session cleanup has
an independent 3-second bound. Dispatched tool calls are never retried: after a timeout or
lost response, a remote mutation may already have succeeded. Sessions are closed
where supported. JSON/SSE, protocol errors and `isError` tool results are handled.
Tool content blocks and structured content reach the runtime as JSON text, capped
by the gateway (32 KiB by default, with a truncation marker); resource links are not
fetched, and media is not rendered as native model attachments. The remote server
validates tool-specific input schemas; Weft validates the object envelope and
size. Credentials and upstream error payloads are not exposed as transport errors.

Pending increments include live automatic-registration qualification,
private-network deployment policy, persistent SSE cursor resumption,
vendor-specific setup/scopes and live GitHub/Bitbucket/Jira/Confluence acceptance. The service selector is
metadata, not a vendor adapter or proof that a vendor supports this transport.

Validation: unit tests cover input/network rules, credential handling, catalogue
limits and concurrent updates; API integration tests use SQLite and a real TLS
MCP fixture; Chromium covers registration, discovery, graph grant persistence,
disable/removal, authentication failure, successful agent invocation and denied calls. Fixture credentials are synthetic;
no live vendor credentials are used by these tests.

Vendor setup is being specified in [priority integration contracts](mcp-integrations.md).
The [template candidate catalogue](mcp-templates.md) distinguishes proposed
configurations from verified compatibility. Both are planning documents; the
current runtime limits described above remain unchanged.

## Versioned templates and browser links

The authenticated `GET /api/v1/mcp/templates` catalogue provides version 1 of
DeepWiki, GitHub read-only, Context7, Hugging Face and W&B, plus GitHub v2 and
GitLab v1. Each template includes
public defaults, authentication guidance, source/review date and validation status.
These presets do not certify vendor compatibility; test each connection before use.

Open `#/integrations/connect?template=deepwiki&version=1` on your installation
(for example `http://localhost:8080/#/integrations/connect?template=deepwiki&version=1`).
The same hash route works on a hosted installation and survives the login reload.
Catalogue links can be copied from the browser. Links accept only one template ID
and one positive version; unknown versions and extra parameters are rejected.
Never include credentials in a link.

The review screen displays the endpoint and authentication guidance. An administrator
must choose **Use template defaults**, review/edit the connection ID and name, enter
any credential in the protected field, and explicitly save. Opening a link performs
no discovery, writes or grants. The saved connection records `template: {id, version}`
and initially starts untested with no tools. The console then automatically
requests discovery for enabled connections with available authentication; pending
OAuth requires consent first. Discovery failure preserves the saved connection and
can be retried with Test or Refresh tools. Direct API clients retain the separate
save/refresh contract. Discovery never grants tools to agents. Existing discovery and per-agent authorization
remain separate actions. Duplicate connection IDs fail rather than overwrite.

Template versions are immutable source-controlled defaults. The API rejects an
unknown reference, unsupported parameters/authentication modes, or a service/endpoint
that differs from the pinned version resolved with those parameters.
**Customize without template** detaches that reference before manual configuration;
existing endpoint-change credential protections still apply. Editing a pinned
connection in the console preserves its version. **Review template update** shows the
proposed endpoint and authentication guidance; **Apply update to draft** changes only
the form. Cancel leaves the draft intact. Saving still checks the connection revision,
requires credential review for endpoint changes, and never modifies agent grants.
There is no automatic upgrade.

GitHub v2 supports Bearer PAT or browser OAuth and the documented read-only default,
repository, issue, pull-request and Actions toolset endpoints. Select a **Read-only
toolset**, then apply it to the form. A toolset selects offered operations, not the
repositories a credential can access: restrict the PAT itself and review agent grants.
The per-tool fine-grained permission matrix and live toolset qualification remain pending.

GitLab v1 uses OAuth and accepts an HTTPS instance origin through **GitLab instance
URL**. Applying another instance updates its MCP endpoint/issuer and clears the draft
client ID; register or provision a client for that instance before authorizing.
Parameters persist as non-secret `templateParameters` beside the pinned reference.
Credentials must never be placed in parameters or browser links. Live instance,
licensing and corporate-policy qualification remain separate.

Endpoint sources: [GitHub remote toolsets](https://github.com/github/github-mcp-server/blob/main/docs/remote-server.md)
and [GitLab MCP server](https://docs.gitlab.com/user/gitlab_duo/model_context_protocol/mcp_server/).

## Browser OAuth (pre-registered clients)

Administrators can select **Use browser OAuth** when saving a connection and
configure its issuer, client ID, requested scopes and optional client secret.
Public clients use token endpoint authentication `none`; confidential clients use
`client_secret_basic` or advertised `client_secret_post`. Missing authentication
metadata defaults to `client_secret_basic` (RFC 8414); an explicitly empty list
is rejected. GitHub.com and public GitLab MCP clients have narrowly scoped compatibility
profiles below.
DCR and public-client CIMD are available through explicit setup review; see
[automatic client setup](#automatic-client-setup-dcr-and-cimd). Live-provider qualification remains pending.
Historical v1 template guidance describes their original Bearer/no-auth presets;
OAuth settings are configured separately and do not come from a shared link.

Set `server.publicUrl` or `WEFT_PUBLIC_URL` to the browser-facing installation
origin. The default is `http://localhost:8080`. Hosted installations require HTTPS;
HTTP is allowed only for localhost/loopback. Paths, credentials, query strings and
fragments are rejected. The OAuth form displays the configured callback via authenticated
`GET /api/v1/mcp/oauth/config`. Register that exact backend callback with the provider:

```text
https://your-weft.example/api/v1/mcp/oauth/callback
```

The callback is derived from this configuration, never the request Host or forwarded
headers. The browser must use that same origin so its binding cookie returns.
This delivery does not change any running installation's configuration.

After saving, review the issuer/scopes and choose **Authorize**. Weft probes the
MCP resource without credentials, reads `resource_metadata` from its authentication
challenge (or the path-specific well-known location), and verifies that the
configured issuer is advertised for the exact MCP endpoint. Issuer metadata must
match exactly and advertise PKCE S256. Client authentication follows the
advertised methods, the RFC default, or the explicit GitHub.com profile below. Metadata/token/revocation requests use the existing public-only HTTPS
transport, no redirects, a 15-second bound and 64 KiB response limits. Other resource
URI canonicalization/metadata fallback variants need separate compatibility work.
These mechanisms follow the [MCP authorization contract](https://modelcontextprotocol.io/specification/2025-06-18/basic/authorization).

The browser goes to provider consent and returns to the backend callback. A
10-minute, single-use transaction binds the connection revision, issuer, callback,
initiating Weft identity/session and an HttpOnly SameSite=Lax cookie (Secure
on HTTPS). The callback holds the code in encrypted, bounded server-side storage and redirects to
a frontend route containing only an opaque transaction handle. **Complete OAuth
connection** requires the original authenticated admin session plus the binding
cookie before exchanging the code with PKCE and a resource indicator. Denial,
expiry, replay, identity mismatch and stale connection revisions fail explicitly.
No access/refresh token or client secret is sent to the frontend. Discovery follows
the exchange; failure remains visible and does not mark the connection ready.
OAuth consent never grants an agent tools.

Transactions share an installation-wide limit of 128 entries and expire after ten
minutes. State, browser/session bindings, PKCE verifiers and callback codes are
stored in one encrypted, revision-checked document. Pending authorizations survive
a backend restart and can continue on another controller sharing the same storage,
encryption keys, authentication configuration and public URL; sticky routing is
not required for this transaction. Filesystem storage remains single-process;
multi-controller installations use a shared database. Keep controller clocks
synchronized. Expired entries are rejected immediately and reclaimed on the next
successful transaction update; the bounded encrypted document may retain expired
entries while authorization is idle. Capture and consumption each have one winner.
Consumption precedes the token exchange: a crash or uncertain exchange after that
point requires fresh authorization and never replays the code automatically.
Storage/key failures fail closed without disclosing transaction contents. Completed
credentials and issuer endpoints are encrypted/persisted as appropriate and survive
restart: access tokens, refresh tokens and client secrets are encrypted separately
and bound to the connection; issuer endpoint metadata is public configuration.

Before discovery or invocation, a token within five seconds of expiry is renewed
when a refresh token is available. A persisted compare-and-swap claim serializes
renewal; rotated credentials are saved atomically. Waiters have a 15-second bound;
a refresh left interrupted for 45 seconds requires reauthorization. Missing expiry
means the provider did not supply a schedule; an HTTP 401 requires authorization
again. Renewal failure also requires reauthorization, with no replay of a possibly
completed mutation. Requested scopes cannot expand through a token response, and
renewal cannot expand the previously granted scopes. Read the new connection
revision after a renewal; requests already waiting on older revisions can be denied.

**Revoke OAuth** requires confirmation and clears local access/refresh tokens and
the tool catalogue before contacting the provider. A late callback/refresh cannot
restore revoked credentials. The client registration/secret remains for deliberate
reauthorization; changing issuer/client clears the previous client secret. Temporary
disable retains credentials and is distinct from revocation or deletion. Provider
revocation is reported separately as pending, requested, unsupported or failed;
requested means the token revocation endpoint accepted the request, not proof that
an entire provider consent grant was removed. Use the provider's account settings
when necessary, including an ambiguous network failure. In-flight remote actions
cannot be undone. Concurrent discarded token responses trigger bounded upstream
cleanup and an audit outcome; they never restore local access.

OAuth lifecycle events (`mcp.oauth.exchanging`, `authorized`, `refreshing`, `renewed`,
`reauthorization_required`, `revoked`, `revocation_result`, and `cleanup`, all under
`mcp.oauth.`) contain state/outcome metadata, never codes or tokens. API latency and
outcomes use existing route metrics. Any external reverse proxy must also redact
callback query strings; Weft logs/traces record route paths, not callback queries.

Validation includes unit tests for metadata/configuration and callback boundaries,
TLS/API/persistence integration, concurrent refresh/revocation under Go's race
detector, and Chromium consent/return/renewal/revocation and failure journeys.
Live-provider credentials, provider-specific scopes and production activation are
not covered by these fixture tests.


## Registering GitHub.com OAuth for a local Weft

The GitHub.com profile requires the exact issuer `https://github.com/login/oauth`
and its exact `/authorize` and `/access_token` endpoints. GitHub currently omits
`token_endpoint_auth_methods_supported`; Weft uses GitHub's documented
`client_secret_post` with an encrypted registered client secret. Authorization
and renewal use the same method. Comma-separated response scopes are normalized
before scope-expansion checks. HTTP 200 responses carrying OAuth errors are
rejected without displaying upstream descriptions or credentials. PKCE S256,
resource/issuer/session checks, TLS and public-network restrictions still apply.
This profile does not automatically qualify GitHub Enterprise hosts.

For an initial OAuth App connection (no private repository access):

1. In GitHub, open **Settings → Developer settings → OAuth Apps → New OAuth App**
   ([registration](https://github.com/settings/applications/new)). Use a dedicated
   app, for example `Weft local`; homepage `http://localhost:8080`; authorization
   callback `http://localhost:8080/api/v1/mcp/oauth/callback`. Use the callback shown
   by your Weft form if the installation origin differs. Device flow is not needed.
2. Register the app yourself, copy its **Client ID**, and generate a client secret.
   Enter the secret directly into Weft's protected field, never into chat,
   connection URLs, source files or screenshots. The browser's existing GitHub
   session is used for login; it does not replace this application registration.
3. In **Connections → Add connection**, create a connection with service `github`,
   endpoint `https://api.githubcopilot.com/mcp/`, **Use browser OAuth** checked,
   issuer `https://github.com/login/oauth`, and that client ID/secret. For this
   initial check, request `read:user offline_access`. The latter requests expiring
   tokens and renewal; it is not repository access. If starting from the v1 PAT
   template, use **Customize without template** to configure this OAuth connection.
4. Save, select **Authorize**, review GitHub's consent and authorize the dedicated
   app. On return select **Complete OAuth connection** in the same Weft session.
   Check the authorized status and discovered tools, then explicitly grant only
   the desired read tools to an agent. OAuth consent alone grants no agent tools.
5. Use a read operation against your account or a public test repository for initial
   acceptance. Private repository access is a separate decision: OAuth App `repo`
   scope is broad and includes writes. For repository-specific permissions, prefer
   a GitHub App installed only on selected repositories; its live acceptance and
   permission matrix remain separate work. Organizations may require administrator
   approval/SSO authorization.
6. **Revoke OAuth** clears local access. GitHub's discovery metadata currently has
   no standard revocation endpoint, so Weft reports `unsupported` for provider
   revocation. Remove the grant in GitHub **Settings → Applications → Authorized
   OAuth Apps** as well when revoking upstream access. A refresh failure requires
   reauthorization; do not assume every token has a refresh token.

Evidence: unit contract/error tests, API integration with encrypted storage and
real TLS OAuth/MCP fixtures, and a Chromium consent/renewal/revocation journey
against the GitHub wire-contract fixture. This is not a successful live GitHub
account authorization; that remains pending a user-registered app and consent.

Sources: [GitHub MCP host integration](https://github.com/github/github-mcp-server/blob/main/docs/host-integration.md),
[OAuth authorization and refresh](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps),
[RFC 8414 metadata defaults](https://www.rfc-editor.org/rfc/rfc8414.html#section-2).


## Service setup and independent connections

In **Connections → Add connection**, choose **GitHub**, **Atlassian**, **GitLab** or **Custom**.
Existing Jira, Confluence and Bitbucket service labels remain readable/editable.
Choose **Use OAuth defaults** to prepare the fields; review them before saving.
Applying defaults clears client-registration fields in the form. It does not
register an external application or authorize access. A pinned v1 template must
be detached with **Customize without template** first. GitLab's instance field
accepts an HTTPS origin and derives `/api/v4/mcp`; private networks remain blocked
by the existing server-side transport. Custom settings require the server's own
MCP contract, not just an OAuth-enabled REST API.

Use unique IDs such as `github-personal`, `atlassian-work` and `atlassian-other`,
with display names such as “Personal GitHub” and “Work Atlassian”. The same
service can have multiple connections. These remain installation-owned settings,
managed by Weft administrators; they are not personal user accounts or social
login. Every ID has its own encrypted credentials, scopes, revision, catalogue,
authorization transactions and revocation state. Agent grants name the connection:
`mcp.github-personal.search` cannot authorize `mcp.atlassian-work.search`.
Changing a service invalidates that connection's OAuth authorization/catalogue.

The form reports **Configuration pending** until a registered client ID is entered.
Saved OAuth connections show **Authorization required**, **Authorized**, **Error**,
**Revoked** or an authorization-in-progress state. **Reauthorize** starts a new
bound transaction. Discovery errors remain distinct from successful token exchange.
Revocation affects that connection's local credentials and catalogue; no other
connection's persisted state or grants are edited. External providers can share
or revoke application-wide grants, so separate Weft connections do not promise
isolation from provider-wide account or app revocation.

OAuth errors expose only authored guidance from recognized error codes. Upstream
error descriptions and URLs are never echoed. Check client registration/callback
for invalid-client/grant errors, MCP scopes for scope errors, and corporate
site/project/IP/domain/SSO policies for access denials. A `403` alone does not prove
which policy failed. Missing refresh credentials or failed renewal requires fresh
consent. If upstream revocation is unsupported or fails, local access remains
revoked and the administrator must review provider account settings.

### Atlassian Cloud MCP v2

Public metadata checked on 2026-10-17 advertises:

- Resource: `https://mcp.atlassian.com/v2/mcp`.
- Issuer: `https://auth.atlassian.com/VCeDsk8ZHncYF1g234fKtc4lNipbBhu3`.
- Authorization: `https://auth.atlassian.com/authorize`.
- Token: `https://auth.atlassian.com/oauth/token`.
- Revocation: `https://auth.atlassian.com/oauth/revoke`.
- Client methods: `none`, `client_secret_post`, `client_secret_basic`, and
  `private_key_jwt`; Weft supports the first three, not signed client assertions.

Weft's suggested scopes are `read:me offline_access read:jira:agent-interface
read:confluence:agent-interface`. Review the tools/sites actually needed before
consent. Search and write tools may need additional **MCP** scopes; the defaults
are not permission to use every discovered tool. The issuer is checked against
resource metadata at every authorization start, so a changed provider contract
fails visibly rather than silently redirecting credentials.

For manual setup, a compatible pre-registered MCP client must be supplied by your administrator or
Atlassian. Request a dedicated client with the exact callback displayed by Weft,
Authorization Code/PKCE S256, supported client authentication, reviewed MCP scopes
and refresh-token access. No verified self-service manual registration portal is
provided here. Alternatively, select automatic setup: current authorization metadata
advertises CIMD and DCR. Weft calls DCR only after explicit review and confirmation. An ordinary
`auth.atlassian.com` REST 3LO app is not assumed compatible. Without a matching
registration, the real connection remains pending. Corporate admins may also need
to allow the callback domain, site and outbound IP. These approvals must happen
outside this implementation; no corporate settings are changed automatically.

Sources: [MCP resource metadata](https://mcp.atlassian.com/.well-known/oauth-protected-resource/v2/mcp),
[issuer metadata](https://auth.atlassian.com/.well-known/oauth-authorization-server/VCeDsk8ZHncYF1g234fKtc4lNipbBhu3),
[official Atlassian MCP setup](https://github.com/atlassian/atlassian-mcp-server).
The legacy `mcp.atlassian.com` issuer metadata is not interchangeable with the
issuer advertised for this v2 resource.

### GitLab instance setup

Choose the exact public HTTPS instance origin, for example `https://gitlab.com`
or `https://gitlab.example.org`. The endpoint is `<origin>/api/v4/mcp`, issuer
`<origin>`, and requested scope `mcp`. A REST `api` scope is rejected as a substitute.
GitLab documents pre-registration through user/group/admin UI starting in 19.3:
create an OAuth application, set the exact Weft callback, select `mcp`, clear
**Confidential**, and enter its application ID in Weft. Instance/group MCP
access must be enabled. Older versions require separate qualification; an older
REST OAuth application is not assumed sufficient.

GitLab.com's current metadata lists only confidential client methods even though
the MCP registration guide prescribes a public client. Weft allows public
PKCE only for service `gitlab`, its exact origin/resource pairing, canonical
`/oauth/authorize` and `/oauth/token` endpoints, and metadata advertising basic or
post authentication. Custom services and unrelated issuers do not inherit this
exception. Public clients send no client secret; confidential clients still must
use a supported advertised method. TLS/public-network checks and S256 remain
mandatory. Refresh and `/oauth/revoke` use the same connection-specific client.

Sources: [GitLab MCP pre-registration](https://docs.gitlab.com/user/model_context_protocol/mcp_server/#reuse-a-single-oauth-application),
[GitLab.com resource metadata](https://gitlab.com/.well-known/oauth-protected-resource/api/v4/mcp),
[GitLab.com issuer metadata](https://gitlab.com/.well-known/oauth-authorization-server).
An instance can differ from GitLab.com: review its version, metadata, registration
and corporate controls before authorizing. No application is created by Weft.

### Qualification and remaining external acceptance

Contract fixtures exercise real TLS OAuth exchanges and MCP discovery/calls,
encrypted filesystem persistence, overlapping pending authorizations, refresh,
revocation and exact gateway grants. Chromium covers the setup defaults, four
simultaneous connections (including two of one service), failed/denied consent,
re-authorization, independent revocation and absence of fixture secrets in browser
storage and API views. Generic issuer mismatch, network policy and credential
redaction tests remain applicable. This is fixture evidence, not live vendor
certification.

Pending for each real connection: accepted automatic or administrator-supplied registration and consent;
actual discovery and a permitted read tool; expiry/renewal; provider revocation;
insufficient-scope and corporate policy denial. GitLab also needs qualification of
the selected instance/version. Installation login and per-user credential ownership
are unchanged. No real provider registration was performed during implementation.


## Automatic client setup (DCR and CIMD)

In **Client registration method**, choose **Manual**, **Automatic**, **DCR** or
**CIMD**. Existing connections remain manual unless explicitly edited. Automatic
setup first reuses its persisted registration; otherwise it selects advertised
public-client CIMD when the configured installation URL can host it, then advertised
DCR. Manual registration is always an alternative. Saving a connection or opening
a link performs no registration. Use **Review OAuth setup**, inspect the selected
mechanism, then confirm to register/publish metadata and open provider consent.
No fallback occurs silently after a failed authorization.

The admin-only preview endpoint is
`GET /api/v1/mcp/connections/{id}/oauth/preview`. Automatic start requires the
reviewed mechanism in `POST .../oauth/start?registrationMode=dcr` (or `cimd`);
manual start retains its existing API. Discovery validates resource/issuer binding
and public-only HTTPS destinations before registration. DCR sends a bounded JSON
POST to the advertised endpoint with the exact callback and reviewed scopes.
Supported responses must agree on callback and authentication (`none`,
`client_secret_basic` or `client_secret_post`). Expiring registration secrets,
initial registration access tokens, private-key JWT and remote registration
management are not supported; use a compatible manual client when required.

Registration uses persistent compare-and-swap claims before sending the POST.
Concurrent starts cannot register the same saved connection twice. Completed
registrations survive backend restarts. A failed or interrupted registration is
shown as uncertain/pending and is not retried automatically: inspect the provider
for a possibly created client before deliberately changing registration settings
or using manual setup. Registration credentials remain encrypted and are never
returned by the API. The UI exposes the effective mode, registration status and a safe diagnostic
category with the provider HTTP status when a response was received. Upstream
bodies, descriptions, headers and credentials are never retained in diagnostics.
The diagnostic survives reloads and restarts; historical failures without it
cannot be reconstructed. A failed start clears the stale confirmation and reloads
the saved connection, without repeating the registration POST. Updating issuer, mode, scopes or endpoint clears incompatible local state;
changing the configured callback blocks reuse until the operator reviews and
changes the connection configuration. Such changes never delete remote clients.

CIMD uses `https://<installation>/api/v1/mcp/oauth/client/{id}` as the client ID.
This unauthenticated endpoint publishes only `Weft`, the exact client ID and
callback, grant/response types and public authentication method. It exists only
for an enabled, prepared CIMD connection; it exposes no tokens, scopes, private
connection name or client secret. The provider must retrieve this document over
public HTTPS. Weft checks URL eligibility, not reachability from the provider.
A local HTTP installation cannot host it: use DCR/manual setup, or separately
configure a suitable hosted installation. Weft never creates a tunnel or
changes installation authentication settings automatically.

GitLab.com currently advertises DCR; selected instances may disable it. CIMD must
not be inferred where absent. Atlassian MCP v2 currently advertises both; corporate
callback/site/IP restrictions can still prevent consent. GitHub remote MCP uses
manual registration. See the metadata sources for [Atlassian](#atlassian-cloud-mcp-v2) and
[GitLab](#gitlab-instance-setup).
These capabilities are not certification of real account acceptance.
See the [browser OAuth specification](../specs/providers-and-integrations/mcp-templates-and-browser-oauth/spec.md)
and [vendor qualification record](../specs/providers-and-integrations/mcp-vendor-profiles-and-qualification/spec.md).

OAuth still uses PKCE S256, session-bound state/cookies, encrypted tokens and
explicit per-agent grants. Revocation blocks this connection's access and attempts
provider token revocation; it preserves the client registration for reauthorization.
It does not delete a remote client or affect other connections. Registration events
use the existing `mcp.oauth.registration_started`, `registration_ready` and
`registration_failed` audit events with connection/revision context, never payloads
or secrets.

Unit tests cover mechanism selection, malformed responses, redaction, secret
storage, concurrent registration and restart reuse. TLS/SQLite API integration
exercises both mechanisms through consent, discovery, refresh and revoke. Chromium
covers DCR review/reuse/denial/failure/manual recovery and hosted CIMD with a real
TLS proxy in the disposable test harness. `make test-e2e` runs both the ordinary
browser suite and `playwright.oauth-cimd.config.ts`. No production transport bypass
is introduced. Live GitLab/Atlassian registration and corporate approvals remain
pending and require separately authorized test accounts/resources.


### Diagnosing an uncertain DCR registration

Do not create another connection, reset registration state, or change the issuer,
scopes or callback just to retry an uncertain registration. First establish with
the provider whether the original attempt created a client. Weft does not
currently implement remote registration reconciliation or client deletion. A
historical uncertain record without diagnostic details must remain blocked until
external evidence allows a deliberate recovery; an HTTP error reported by the
browser alone does not establish the upstream result.

A provider HTTP 403 points to refusal of the registration request. It does not by
itself prove a localhost incompatibility or prove that no remote client exists.
Atlassian lists HTTP `localhost` and `127.0.0.1` among its supported MCP client
domains; an organization can still block those domains. Review the exact callback
and organization policy with its administrator before changing settings. This is
separate from CIMD, which needs provider-reachable public HTTPS metadata.
See [Atlassian-supported domains](https://support.atlassian.com/security-and-access-policies/docs/available-atlassian-mcp-server-domains/)
and [MCP permissions](https://support.atlassian.com/security-and-access-policies/docs/configure-atlassian-mcp-server-permissions/)
(reviewed 2026-10-17). Real Atlassian registration and consent remain externally
unvalidated; fixture coverage is not vendor certification.


## Connection quotas and call history

Administrators configure **Connection calls per minute** on each connection.
`callsPerMinute` accepts integers from 0 to 60,000; omitted/0 means unlimited.
The quota counts tool invocations across all agents, tools and replicas using
that connection, in fixed UTC-minute windows. It is not a sliding window: calls
on either side of a minute boundary can form a burst. One atomic compare-and-swap
counter per connection persists in shared installation storage, so controller
restart does not reset usage. Controllers must use synchronized clocks and shared
storage to enforce one installation-wide limit. Backwards clock movement does not
reset a window. Changing a limit does not reset recorded consumption.

Admission occurs after authorization, argument/host checks, human approval and
credential readiness, immediately before remote dispatch. Rejected approvals and
unauthorized calls do not consume the connection quota. Admitted failures and
cancellations are charged; no automatic wait or retry is introduced. Discovery,
OAuth and session initialization are not separate billable calls. Existing
per-agent tool rate limits still apply independently. Exhaustion returns the
gateway rate-limit refusal and records `tool.denied` with `rate_limited`; no MCP
tool request is sent. Storage failure/corruption fails closed. The existing
15-second operation deadline and response bounds remain unchanged. Concurrent
in-flight limits and provider-specific daily/billing quotas are not implemented.

**Connections → View history across connections** queries persisted tool and approval events,
newest first, filtered by connection ID, graph, agent, exact qualified tool,
event type, outcome and time range. Apply filters explicitly; older-page requests
use the last applied filters. Results are safe summaries and correlation IDs,
not raw model/tool content, arguments, approval comments or upstream errors.
History remains queryable for deleted connections and follows event-store
retention. The page reports failures without replacing previous results with an
apparently successful empty list.

`GET /api/v1/mcp/history` is viewer-readable. Filters are `connection`, `graph`,
`node`, `tool`, `type`, `outcome`, `since`/`until` (RFC3339, inclusive), `before`
(exclusive event sequence), and `limit` (1–100, default 50). Responses include
`entries`, `hasMore` and `nextBefore`. Each request scans at most 500 eligible
stored events. Sparse filters may produce an empty page with `hasMore: true`;
continue with `before=nextBefore`. Newer incoming events do not shift an older
page's cursor. Historical event payloads are projected through an allowlist;
legacy raw fields are never forwarded by this endpoint. New MCP tool audit errors
also use platform-owned summaries. Approval review itself retains its existing
protected detail contract.

## Planned managed hosting

Remote connections remain the supported deployment model. The
[managed hosting design](mcp-hosting.md) describes a future dedicated-daemon
boundary for reviewed server profiles; it does not enable arbitrary server
commands or mark candidate vendor images as qualified.

## Atlassian v2 flat discovery

The reviewed `https://mcp.atlassian.com/v2/mcp?tools=all` endpoint requests
[Atlassian's paginated flat catalogue](https://developer.atlassian.com/cloud/rovo-mcp/guides/supported-tools/).
The console's Atlassian defaults use this endpoint. The API accepts only this
exact query on this exact endpoint; additional, repeated, encoded or secret query
parameters remain rejected. Existing query-free connections are preserved.

OAuth discovery must advertise the selected issuer and either this exact resource
or its query-free v2 resource. Authorization, exchange and refresh retain that
advertised resource identifier; other resources remain rejected. The query option
selects catalogue presentation and is not a scope or permission grant.

The existing transport limits apply: 20 pages, 500 unique tools, two MiB aggregate
catalogue, object schemas and bounded cursors. Refresh advances the catalogue
revision; calls awaiting approval cannot use stale authority. New operations need
explicit grants. Dependency failure clears the available catalogue and stays
visible. TLS fixtures cover pagination, catalogue drift, denied new operations,
resource binding and browser configuration. Live corporate/vendor qualification
remains pending.

### Atlassian dispatcher restrictions

Weft excludes `executeRead`, `executeWrite` and `executeDestructive` from
Atlassian catalogues. These generic dispatchers could route a single grant to
multiple underlying operations. Select individual tools from the `tools=all`
catalogue and explicitly grant them to each agent. Existing saved dispatcher
grants are also denied at invocation; refresh discovery and replace them with
reviewed individual operations. The restriction applies to Atlassian-family
service labels and the official MCP host even when labeled custom. It does not
provide site/project restrictions: those remain dependent on provider permissions
until explicit resource constraints are implemented.

### Atlassian personal API tokens

Select an Atlassian-family service, the v2 flat endpoint and **Atlassian personal
API token (Basic)** under Credential type. Enter the token owner's email and
personal API token together. Weft encrypts both values and builds the Basic
header only in the server-side transport. After saving, neither value is returned;
blank fields preserve the stored pair. Replacing either requires entering both.
Changing the endpoint or credential type requires replacing or clearing the
credential. Clearing it invalidates the catalogue and does not authorize agents.

Organization administrators must enable API-token access. Choose the required
`agent-interface` scopes and pass `cloudId` for operations that require it.
Service account keys use **Bearer token**. OAuth remains available for interactive
consent and tools unavailable to API tokens. API-token connections are governed by
provider IP policies; OAuth callback-domain policies are not equivalent to token
restrictions. See the [official authentication guide](https://developer.atlassian.com/cloud/rovo-mcp/guides/configuring-authentication-via-api-token/).

Local tests cover TLS discovery/calls, encrypted persistence, write-only UI,
invalid credentials, credential-type changes and response echo rejection. Live
corporate enablement, scoped tool availability and site access remain unverified.

### Typed dbt and standalone LangSmith headers

Custom connections offer bounded transport settings rather than an arbitrary
header editor. Keys stay in the encrypted credential field; metadata is non-secret
and visible in connection responses. Changing metadata requires replacing/clearing
a stored token, or reauthorizing OAuth. Discovery revisions and explicit agent
grants continue to apply. Protocol, Host, Cookie and arbitrary authorization
headers cannot be supplied as metadata.

For remote dbt, use the account's public HTTPS MCP endpoint and Bearer token.
Optional typed fields accept numeric production/development environment and user
IDs only. The provider requires production context for token authentication; SQL
execution needs a personal token and development/user context. Service tokens are
not sufficient for `execute_sql`. Review the [dbt setup contract](https://docs.getdbt.com/docs/dbt-ai/setup-remote-mcp)
for account permissions and tool-specific requirements. URL-form IDs and additional
dbt v2 headers are not supported by this profile.

For the standalone LangSmith MCP implementation, select **Standalone LangSmith API
key**. Weft sends `LANGSMITH-API-KEY`, with an optional workspace UUID and a
bounded US/EU API-region selection. The MCP endpoint itself must use public HTTPS;
this does not launch a local server. Arbitrary upstream URLs are refused. The
[standalone server contract](https://github.com/langchain-ai/langsmith-mcp-server/blob/main/README.md)
differs from the [hosted LangSmith OAuth service](https://docs.langchain.com/langsmith/langsmith-mcp-server).
Do not use the standalone header profile as proof of hosted-service compatibility.

TLS fixtures verify initialization, pagination and calls with the
expected headers, encrypted persistence and missing-context failures. Browser
fixtures verify save/discovery and rejection of unauthorized header names. Live
account access, per-tool permission/cost behavior and regional deployments remain
pending qualification.

### Sentry OAuth profile

Choose the versioned **Sentry** template to populate the public MCP endpoint,
issuer and `org:read` scope. Enter a compatible registered client or review the
existing automatic registration flow; no application is registered on save.
Use the exact callback shown by Weft. Agent tools still require explicit
grants after successful authorization and discovery.

The [Sentry MCP guide](https://mcp.sentry.dev/) and
[authorization metadata](https://mcp.sentry.dev/.well-known/oauth-authorization-server)
were reviewed on 2026-10-18. The server advertises PKCE S256, refresh tokens,
DCR, CIMD and revocation. This is metadata evidence, not completed consent or
refresh/revocation acceptance. The generic OAuth implementation still validates
protected-resource metadata, issuer binding and advertised client methods.
No provider-specific compatibility exception was added.

The template intentionally requests only the advertised `org:read` scope.
Additional project/team/event/alert write scopes require review; it does not
claim that all read tools work with this scope. Scoped organization/project
endpoints can be configured after detaching the template, but require their own
metadata verification and permission review. Generic dispatcher tools must not
be treated as grants to individual underlying operations. Catalogue qualification,
live tool permissions and account acceptance remain pending.

### Databricks workspace/service profile

Choose **Databricks workspace service**, replace the example HTTPS workspace
origin and enter the three-part MCP service name from Unity Gateway. The template
builds `/ai-gateway/mcp-services/<catalog>.<schema>.<service>`. Each segment accepts
letters, numbers, underscores and hyphens, beginning with a letter or underscore,
up to 63 characters. Unusual identifiers need a separately reviewed custom
connection. Query strings, credentials and paths are not workspace origins.

For testing, enter a PAT in the encrypted Bearer field. For browser OAuth, have
the administrator register a public application under account Settings → App
connections with the exact Weft callback and `ai-gateway` scope; request
`ai-gateway offline_access`. Obtain the issuer from the selected MCP's metadata.
Weft does not guess it or bypass resource/issuer validation. Changing workspace
clears the draft issuer/client ID for review; saved credential replacement and
reauthorization rules still apply. Automatic registration is not presumed.

The [current Databricks MCP client guide](https://docs.databricks.com/aws/en/ai-gateway/coding-agent-codex)
was reviewed on 2026-10-18. Workspace access, service permissions, beta availability
and downstream provider consent require separate qualification. Weft does not
follow tool-returned login links automatically. This profile does not implement
service-principal `client_credentials` acquisition/renewal, legacy managed endpoint
families, SQL cost limits or universal service scopes. Configuration fixtures do
not establish live workspace acceptance; tool grants remain explicit per agent.

## Explicit stateless transport

Custom connections can select **Stateless MCP 2026-07-28 (bounded support)**.
Omitting `transportProfile` preserves the existing session-based 2025-06-18
behavior. Pinned templates retain their original protocol: detach the template
before changing transport. Saving a profile change clears discovery and resets
OAuth authorization; existing grants do not authorize a stale catalogue.

The stateless path sends matching body metadata and protocol/method/name headers,
without initialization, notifications, session reuse or cleanup. It preserves
HTTPS/DNS protections, response/catalogue bounds, encrypted credentials and exact
per-agent permissions. Tool calls are never automatically replayed. JSON and SSE
responses are supported. Additional input requests (MRTR) and catalogues requiring
`x-mcp-header` parameter mirroring fail explicitly; they are not silently ignored.
See the [protocol binding](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http).

This is a bounded protocol option, not a qualified BigQuery/Snowflake profile,
IAM setup, resource boundary or spending limit. Review the
[warehouse qualification requirements](mcp-templates.md#warehouse-protocol-qualification-2026-10-18)
before enabling data-query tools. Transport selection never grants agent access.

## Atlassian read boundaries

An administrator may enable **Restrict Atlassian products and sites** on a
connection and select Jira and/or Confluence plus 1–32 exact cloud UUIDs.
The reviewed allowlist is intentionally small:

| Product | Eligible operation names |
| --- | --- |
| Jira | `getJiraIssue`, `listJiraProjects`, `searchJiraIssuesUsingJql` |
| Confluence | `getConfluenceContent`, `listConfluenceSpaces`, `getConfluenceSpace` |

These names follow the [official catalogue](https://developer.atlassian.com/cloud/rovo-mcp/guides/supported-tools/).
Discovery must also provide a string `cloudId` property for an operation to remain
eligible. Unknown tools, cross-product operations, broad search, site enumeration,
dispatchers, writes and Bitbucket operations are excluded under this policy.
Every call rechecks the current policy and its exact `cloudId` before accessing
credentials or dispatching remotely. Missing IDs, site URLs and disallowed UUIDs
are denied. Changing the policy invalidates discovery and stale tool instances.

Obtain the site UUID from the administrator; this configuration does not call a
site-enumeration tool automatically. Agent grants and upstream scopes/access are
still required. This limits the selected operations and their site argument,
not projects, spaces, linked content or the authenticated user's upstream access.
JQL is not rewritten or treated as a project boundary. Returned attachment links
are not fetched. Other provider resource constraints and live corporate acceptance
remain pending. Existing connections retain their behavior unless this optional
policy is explicitly configured.

## Exact tool argument restrictions

Connection administrators can expand **Tool argument restrictions** when creating
or editing a connection. Add the remote tool name (without the `mcp.<connection>.`
prefix), a top-level argument name and allowed non-secret values, one per line.
Values are exact and case-sensitive. Empty values, surrounding whitespace,
control characters, duplicate tool/argument pairs and duplicate values are
rejected. The bounds are 64 rules, 32 values per rule and 256 bytes per value.

Once any rule is configured, unlisted tools are excluded. All rules for a listed
tool must match, and each constrained argument must be explicitly supplied as a
string. Discovery excludes tools whose schema does not declare those arguments
as strings. Each call checks the current connection revision and policy before
credential access and remote dispatch. Policy edits clear discovery and invalidate
stale references. Existing agent grants, host restrictions and approvals still
apply. Removing all rules removes this additional boundary; review that change
as carefully as adding a grant.

For a reviewed tool with `workspace` and `repo` string arguments, independent
rules allow every combination of their allowed values. To restrict complete
pairs instead, select **Add combination rule**, enter the argument names in
order, and add each allowed combination. For example, allow only
`team-a / project-one` and `team-b / project-two`: neither cross-pair is allowed.
Every field must match the same row. Existing independent rules still apply
alongside the combination rule, and all tool grants remain explicit.

The API representation is an argument-policy entry such as:

```json
{"tool":"read","arguments":["workspace","repo"],"combinations":[["team-a","project-one"],["team-b","project-two"]]}
```

A combination rule accepts 2–8 distinct top-level string arguments and 1–32
complete, distinct rows. One combination rule per tool is allowed; it cannot
also contain the single-argument `argument`/`values` fields. The same 256-byte
value bound and whitespace/control-character exclusions apply. Saving clears
discovery and stale invocation authority just as for independent rules.
Values are visible connection metadata, so never put credentials in them.

These are request constraints, not vendor authorization or a semantic content
filter. Do not infer Jira project membership from an issue-key prefix, Confluence
space membership from a page ID, or SQL safety from an allowed project argument.
Review tools that accept alternate URLs, aliases or query expressions separately.
Returned links/content are not constrained by these rules. Repository IAM, site
permissions and query-cost controls remain necessary. Vendor-specific resource
qualification remains pending; the generic boundary is tested with local fixtures.


## Remote access failures

Discovery and tool calls expose authored guidance for remote HTTP statuses:

- **401:** review the connection credential; reauthorize OAuth or replace the
  configured credential and explicitly test the connection. OAuth connections
  retain the existing reauthorization-required transition and clear discovery.
- **403:** review the requested resource, credential scopes and organization,
  site or SSO policy with the provider administrator. The status alone does not
  establish which restriction caused the refusal. Reauthorization is not an
  automatic remedy; Weft does not revoke an otherwise authorized OAuth
  connection just because a resource returned 403.
- **429:** wait and check the provider's quota. This is distinct from Weft's
  per-connection admission quota.

Tool calls are not automatically replayed, including after these responses;
remote outcome may still be uncertain. Discovery failures clear the eligible
catalogue. Other connections and agent grants remain unchanged. Error response
bodies, authentication challenges and redirect headers are not copied into this
guidance, including when the provider echoes a credential. These diagnostics do
not certify any vendor's scopes or corporate policy; live acceptance is separate.
