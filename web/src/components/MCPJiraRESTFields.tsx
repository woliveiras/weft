import type { MCPConnection, MCPConnectionInput } from '../api/types';
import { HelpField } from './FieldHelp';
import { supportsJiraREST, type JiraRESTCredentialDraft } from './mcpJiraRESTDraft';

export function JiraRESTStatus({connection}:{connection:MCPConnection}) {
  if (!connection.jiraRest) return null;
  return <section className="connection-discovery" aria-label="Jira REST access status">
    <div><h3>Jira Cloud REST access</h3>
      <p>{connection.jiraRest.credentialConfigured?'REST credential configured':'REST credential missing'}</p>
      <p className="muted">Cloud ID: {connection.jiraRest.cloudId}</p>
      <p>MCP Ready confirms tool discovery only. Test a scoped Jira REST read before activating an issue trigger.</p>
    </div>
  </section>;
}
export function MCPJiraRESTFields({input,onChange,draft,onDraft,saved,busy}:{input:MCPConnectionInput;onChange:(value:MCPConnectionInput)=>void;draft:JiraRESTCredentialDraft;onDraft:(value:JiraRESTCredentialDraft)=>void;saved?:MCPConnection;busy:boolean}) {
  const supported=supportsJiraREST(input);
  if (!supported && !input.jiraRest) return null;
  return <section className="mcp-setup-group mcp-jira-rest" aria-label="Separate Jira Cloud REST access">
    <h3>Jira Cloud REST access <span className="muted small">Optional</span></h3>
    <p>Issue triggers and typed Jira tools use the Jira REST API. An MCP token with agent-interface scopes does not authorize these REST calls. Configure a separate token with Jira REST scopes for this site.</p>
    {!saved?.jiraRest && <HelpField label="Configure separate Jira REST access" help="Stores an independent Jira API credential. This does not grant agent tools, activate triggers or start tasks."><input type="checkbox" disabled={busy} checked={!!input.jiraRest} onChange={e=>{onChange({...input,jiraRest:e.target.checked?{cloudId:''}:undefined});onDraft({email:'',token:'',clear:false});}}/></HelpField>}
    {input.jiraRest && <>
      {!supported && <p role="alert">The saved Jira REST configuration requires an Atlassian MCP service and canonical endpoint. Restore them before saving; the stored configuration has not been removed.</p>}
      <HelpField label="Jira Cloud ID" help="The immutable UUID of the Jira site, not its project key or URL. Get cloudId from your Atlassian site metadata or administrator. Use the exact site UUID intended for this connection. Separate REST access cannot be combined with MCP Atlassian policies or argument restrictions."><input value={input.jiraRest.cloudId} disabled={busy} onChange={e=>onChange({...input,jiraRest:{cloudId:e.target.value}})}/></HelpField>
      <p className="muted">{saved?.jiraRest?.credentialConfigured?'A Jira REST credential is stored. Leave both fields blank to keep it.':saved?.jiraRest?'The Jira REST credential is missing. Enter an email and API token to restore access.':'Enter the email and API token together. Weft stores both encrypted and never returns them.'} Changing the Cloud ID requires a replacement credential or clearing the stored one.</p>
      <HelpField label="Jira REST API token owner email" help="Email of the account that created the separate Jira REST API token. Enter it together with the replacement token. This field never displays a stored email."><input type="email" autoComplete="off" disabled={busy||draft.clear} value={draft.email} onChange={e=>onDraft({...draft,email:e.target.value})}/></HelpField>
      <HelpField label="Jira REST API token" help="A separate scoped Jira Cloud API token for REST issue search, reads and the writes you intend to enable. MCP agent-interface scopes are insufficient. Use an account whose writes are restricted to the selected project."><input type="password" autoComplete="new-password" disabled={busy||draft.clear} value={draft.token} onChange={e=>onDraft({...draft,token:e.target.value})}/></HelpField>
      {saved?.jiraRest && <HelpField label="Clear stored Jira REST credential" help="Clears only the separate Jira REST credential when saved. MCP credentials and OAuth are preserved. The explicit REST mode remains unavailable until replaced."><input type="checkbox" disabled={busy} checked={draft.clear} onChange={e=>onDraft({email:'',token:'',clear:e.target.checked})}/></HelpField>}
      <p className="muted">The MCP credential will not be used as a fallback if this separate REST credential is missing, cleared or rejected. Saving does not verify Jira REST access or change agent grants.</p>
    </>}
  </section>;
}
