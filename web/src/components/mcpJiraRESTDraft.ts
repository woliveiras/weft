import type { MCPConnection, MCPConnectionInput } from '../api/types';

export interface JiraRESTCredentialDraft { email: string; token: string; clear: boolean }
export function supportsJiraREST(input: Pick<MCPConnectionInput, 'service' | 'endpoint' | 'localProcess'>): boolean {
  if (input.localProcess || !['atlassian','jira'].includes(input.service)) return false;
  return ['https://mcp.atlassian.com/v2/mcp','https://mcp.atlassian.com/v2/mcp?tools=all'].includes(input.endpoint);
}
export function prepareJiraRESTDraft(input: MCPConnectionInput, draft: JiraRESTCredentialDraft, saved?: MCPConnection): Pick<MCPConnectionInput, 'jiraRest' | 'jiraRestCredential'> {
  const config=input.jiraRest ?? saved?.jiraRest;
  if (!config) return {};
  if (!['atlassian','jira'].includes(input.service)) throw new Error('Jira REST access requires an Atlassian MCP service. Restore the saved service before saving.');
  if (!supportsJiraREST(input)) throw new Error('Jira REST access requires a canonical HTTPS Atlassian MCP endpoint. Restore the endpoint before saving.');
  if (input.atlassianPolicy || input.argumentPolicy?.length || Object.keys(input.metadataHeaders??{}).length) throw new Error('Separate Jira REST access does not support MCP argument restrictions, Atlassian policies or metadata headers. Review these settings before saving.');
  const cloudId=config.cloudId;
  if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(cloudId)) throw new Error('Enter a valid Jira Cloud ID (UUID).');
  const jiraRest={cloudId};
  if (draft.clear && (draft.email || draft.token)) throw new Error('Cannot clear and replace the Jira REST credential together.');
  if (draft.clear) return {jiraRest,jiraRestCredential:{clear:true}};
  if (/[\x00-\x1f\x7f]/.test(draft.email+draft.token)) throw new Error('Jira REST credentials cannot contain control characters.');
  if (new TextEncoder().encode(draft.email).length>320 || new TextEncoder().encode(draft.token).length>16384) throw new Error('Jira REST credential exceeds its supported size.');
  if (draft.email || draft.token) {
    if (!draft.email || !draft.token) throw new Error('Enter the Jira REST email and API token together.');
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(draft.email)) throw new Error('Enter a valid email for the Jira REST API token owner.');
    return {jiraRest,jiraRestCredential:{username:draft.email,value:draft.token}};
  }
  if (!saved?.jiraRest) throw new Error('Enter the Jira REST email and API token together to configure separate access.');
  if (cloudId!==saved.jiraRest.cloudId) throw new Error('Replace or clear the Jira REST credential when changing its Cloud ID.');
  return {jiraRest};
}
