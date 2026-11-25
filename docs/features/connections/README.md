# Connections

**Configuration → Connections** (`#/connections`) groups MCP, source-control and
issue-integration resources. Each group has its own purpose. Configuring one
does not automatically configure the others or grant an agent access.
Configuration writes require an administrator; ordinary tests/discovery are
operator actions. Read-only access does not imply permission to change secrets.

## MCP tool-server connection

1. Select **Add connection** in MCP connections (`#/connections/new`). Browse
   optional templates or enter custom settings. Templates prepare reviewed
   defaults; opening one does not create a connection or authorize OAuth.
2. Enter stable ID/name, service and MCP endpoint. Choose authentication and
   supply the required credential or start the reviewed browser OAuth flow.
   Credentials are server-held and are not returned after saving.
3. Review optional rate limits, transport profile, argument restrictions and
   service-specific settings in **Advanced settings**. Enable and save.
4. For OAuth, review the registration/authorization preview, complete provider
   consent and return to Weft. Registration recovery and token authorization
   are distinct states; do not blindly repeat uncertain registrations.
5. Test the saved connection or refresh discovered tools. Discovery does not
   execute an agent tool call. Local-process connections can start their reviewed
   discovery process, as described in the owning MCP guide.
6. Open `#/connections/c/<id>/overview`. **Overview**, **Tools**, **Usage &
   history** (`history` in the route) and **Settings** separate connection facts,
   searchable tool descriptions/schemas, call outcomes and configuration.
7. Explicitly select discovered tools in the agent's permissions and publish/
   save the owning agent or workflow. Connection consent alone grants no tools.

Ready means connection/discovery readiness, not guaranteed authorization for
every future tool call. Disabled, untested and unavailable states have different
causes. Check credentials, endpoint, OAuth status and restrictions before retrying.
History filters load bounded outcomes rather than raw secret-bearing payloads.
Deletion requires the connection's disable/revoke preconditions and preserves
history; deleted IDs cannot simply be reused.

## Source-control delivery connection

1. Select the source-control group's **Add connection**. It opens a dedicated
   page at `#/connections/source-control/new`.
2. Choose provider kind, name, repository, branch prefix and any required API/
   Git base URLs. Configure whether PR creation requires human approval.
3. Supply the provider's supported authentication. Bitbucket Cloud currently
   supports scoped API token plus Atlassian account email, or an existing user
   OAuth access token. It does not automatically renew OAuth or fall back to
   another authentication method. Data Center uses its separate adapter/config.
4. Save, then **Test connection**. The row shows loading and an explicit success
   or failure result; untested is not failure. Inspect repository/access facts.
5. Select this connection in task delivery or workflow tool grants. Agent Git
   commit capability, repository access, push capability and PR approval remain
   separate requirements.

Source-control search/status filters operate independently of MCP filters. A
successful read test is not proof that a future branch push or merge is allowed.
Tokens stay in the server's credential boundary rather than an agent SCM CLI.

## Jira issue integrations and lifecycle mappings

Jira Cloud has independent MCP and REST credential consumers. MCP provides
discovered tools; explicit separate REST access supports typed issue operations,
trigger reads and configured lifecycle operations. MCP OAuth/API-token
authorization is not a REST credential. A chat's Atlassian plugin connection
is also not a Weft installation connection.

1. Configure and discover the Atlassian MCP connection when using the Cloud
   tracker path. Resolve any provider-side consent/organization restrictions.
2. In the supported Atlassian MCP connection editor, enable **Configure separate
   Jira REST access** and enter the exact Cloud ID plus separate REST token-owner
   email/token. Save, then test a scoped REST read; MCP Ready only proves MCP
   discovery. Separate REST mode does not fall back to MCP credentials on failure.
3. For lifecycle synchronization, add a Jira Cloud workflow profile at
   `#/connections/jira-cloud/new`. Select the existing MCP authority and reviewed
   revision, Cloud ID and numeric project ID, verify the project-restricted
   account, **Review profile**, then save. This profile holds no credential;
   its authority/credential configuration remains in the connection.
4. For Jira Data Center use `#/connections/issues/new`, with its own base URL,
   project and REST credential; it is not the Cloud MCP path.
5. Discover available transitions for a representative issue of the intended
   type/source status. Choose actual transition/status IDs and **Save mapping**.
   Saving revalidates the sample and records the mapping revision; there is no
   separate mapping-preview operation and no Jira transition is executed here.
6. Select the appropriate source/profile/mapping in the issue trigger and PR
   lifecycle configuration. Mapping alone does not activate a trigger.

Never assume a column is named **In Review**: **Review**, localized names and
project-specific workflows are valid. Missing/ambiguous transitions require
mapping repair, not guessed names or silent state substitution. Automatic writes
require the reviewed project-restricted technical-account contract for both
Cloud and Data Center. Account permissions cannot be replaced by token scopes
or a local project check.

## Implementation and verification map

- [Installation pages](../../../web/src/components/InstallationPages.tsx),
  [ConnectionsList](../../../web/src/components/ConnectionsList.tsx),
  [SCMConnectionsSection](../../../web/src/components/SCMConnectionsSection.tsx)
  and [IssueConnectionsSection](../../../web/src/components/IssueConnectionsSection.tsx).
- [MCP navigation](../../../web/src/components/mcpNavigation.ts),
  [separate Jira REST fields](../../../web/src/components/MCPJiraRESTFields.tsx),
  [Cloud profiles](../../../web/src/components/JiraCloudProfilesSection.tsx),
  [lifecycle mappings](../../../web/src/components/IssueWorkflowMappingsPage.tsx),
  [API routes](../../../server/internal/api/server.go),
  [SCM adapters](../../../server/internal/scm).
- [SCM UI tests](../../../web/src/components/SCMConnectionsSection.test.tsx),
  [issue connection tests](../../../web/src/components/IssueConnectionsSection.test.tsx).

Owning guides: [MCP](../../mcp.md), [integration contracts](../../mcp-integrations.md),
[Atlassian/Bitbucket setup](../../atlassian-bitbucket-setup.md). Qualification is
provider/operation-specific; adapter availability is not proof of every real
Cloud/Data Center deployment combination.
