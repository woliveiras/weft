export function MCPServiceGuide({service}:{service:string}) {
 return <aside aria-label="OAuth service setup">
  <p>Connections belong to this Weft installation. Use separate IDs and names, such as Personal GitHub and Work Atlassian. Each connection has its own credentials and agent grants.</p>
  {service==='databricks'?<>
   <p>Select your workspace and the three-part service name from Unity Gateway. Confirm service permissions and any required beta enablement with the administrator. Use a PAT for testing, or register a public OAuth application under account Settings → App connections using the exact callback below and ai-gateway scope. Request ai-gateway and offline_access.</p>
   <p>Obtain the issuer from that MCP's protected-resource metadata. Weft checks resource and issuer binding; no Databricks compatibility exception is applied. Workspace consent and downstream provider consent are separate. Service-principal client_credentials renewal is not supported.</p>
   <a href="https://docs.databricks.com/aws/en/ai-gateway/coding-agent-codex" target="_blank" rel="noreferrer">Databricks MCP registration and service access</a>
  </>:service==='sentry'?<>
   <p>Use the Sentry MCP issuer https://mcp.sentry.dev and the exact callback below. Review automatic registration to choose advertised DCR/CIMD, or enter a compatible previously registered client. Metadata availability does not prove account approval.</p>
   <p>Start with org:read. Project, team, event and alert write scopes need separate review. OAuth consent does not grant agent tools. Organization/project endpoint defaults do not replace credential restrictions.</p>
   <a href="https://mcp.sentry.dev/" target="_blank" rel="noreferrer">Sentry MCP setup</a>
  </>:service==='github'?<>
   <p>Register a dedicated GitHub OAuth App with the callback below and your Weft homepage. Enter its Client ID and client secret. Start with read:user and offline_access; private repository access needs a separate permission review.</p>
   <a href="https://github.com/settings/applications/new" target="_blank" rel="noreferrer">Register GitHub OAuth App</a>
   <p>Organization approval and SSO may restrict access. GitHub does not advertise standard token revocation: also revoke the grant in GitHub account settings.</p>
  </>:service==='gitlab'?<>
   <p>Use DCR when enabled on the selected instance, or manually create a user, group or instance OAuth application. Set the exact callback below, select mcp and clear Confidential. Enter the application ID; no client secret is needed for that public client. Weft always uses PKCE S256.</p>
   <p>Pre-registration through the UI requires GitLab 19.3 or a compatible version. Ask the administrator to enable MCP for the instance or group. REST api scope and legacy personal tokens are not MCP authorization. Only publicly reachable HTTPS instances are supported.</p>
   <a href="https://docs.gitlab.com/user/model_context_protocol/mcp_server/#reuse-a-single-oauth-application" target="_blank" rel="noreferrer">GitLab MCP registration instructions</a>
  </>:['atlassian','jira','confluence','bitbucket'].includes(service)?<>
   <p>The tools=all endpoint option requests a paginated catalogue of individual operations for explicit agent grants. Generic executeRead, executeWrite and executeDestructive dispatchers are excluded; grant individual operations to each agent. No other query parameters are accepted.</p>
   <p>Use a client already registered with the authorization server advertised by Atlassian MCP v2. Ask your administrator or Atlassian to provision a client for the exact callback below, Authorization Code with PKCE S256, refresh tokens and the reviewed MCP scopes. A REST API 3LO application is not assumed to work.</p>
   <p>The current MCP issuer advertises CIMD and DCR; choose automatic setup to review the available mechanism. Manual setup remains available with a compatible pre-registered client. Live acceptance is still pending. Corporate callback-domain, site and IP policies may require administrator approval.</p>
   <a href="https://github.com/atlassian/atlassian-mcp-server" target="_blank" rel="noreferrer">Atlassian MCP setup and corporate requirements</a>
  </>:<p>Get the MCP endpoint, its advertised issuer, client ID and reviewed scopes from your server administrator. Register the exact callback below. REST OAuth support alone does not establish MCP compatibility.</p>}
  <p>Defaults are configuration guidance, not live-provider certification. Consent never assigns tools to agents. Check requested scopes before saving.</p>
 </aside>;
}
