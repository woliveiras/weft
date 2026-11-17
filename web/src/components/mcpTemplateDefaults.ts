import type { MCPTemplate, MCPConnectionInput } from '../api/types';
import { gitLabOrigin } from './mcpServices';

export function templateEndpoint(template: MCPTemplate, parameters: Record<string,string> = {}): string {
  const values: Record<string,string> = {};
  for (const parameter of template.parameters ?? []) {
    const value = parameters[parameter.name] ?? parameter.default;
    if (value.length > 2048 || parameter.options && !parameter.options.includes(value)) throw new Error(`Invalid template parameter: ${parameter.label}.`);
    values[parameter.name] = value;
  }
  if (Object.keys(parameters).some(key => !(key in values))) throw new Error('Unknown template parameter.');
  if (template.id === 'github' && template.version === 2) return values.toolset === 'default' ? template.endpoint : `https://api.githubcopilot.com/mcp/x/${values.toolset}/readonly`;
  if (template.id === 'databricks') {
    let origin:URL;try{origin=new URL(values.workspace);}catch{throw new Error('Enter an HTTPS Databricks workspace origin.');}
    if(origin.protocol!=='https:'||!origin.hostname||origin.username||origin.password||origin.pathname!=='/'||/[?#]/.test(values.workspace)) throw new Error('Databricks workspace must be an HTTPS origin without path, credentials, query or fragment.');
    if(!/^[A-Za-z_][A-Za-z0-9_-]{0,62}\.[A-Za-z_][A-Za-z0-9_-]{0,62}\.[A-Za-z_][A-Za-z0-9_-]{0,62}$/.test(values.service)) throw new Error('Use a three-part MCP service name with letters, numbers, underscores or hyphens.');
    return values.workspace.replace(/\/$/,'')+'/ai-gateway/mcp-services/'+values.service;
  }
  if (template.id === 'gitlab') { if (/[?#]/.test(values.instance)) throw new Error('GitLab instance must be an HTTPS origin without query or fragment.'); gitLabOrigin(values.instance); return values.instance.replace(/\/$/, '') + '/api/v4/mcp'; }
  return template.endpoint;
}

export function templateDraft(input: MCPConnectionInput, template: MCPTemplate, parameters: Record<string,string> = {}): MCPConnectionInput {
  if(input.transportProfile) throw new Error('Select the session-based transport before applying this template.');
  const endpoint = templateEndpoint(template, parameters);
  const mode = input.oauth ? 'oauth' : input.credentialType ?? 'bearer';
  if (template.authenticationModes?.length && !template.authenticationModes.includes(mode)) throw new Error('Choose a supported authentication mode before applying this template.');
  const issuer = endpoint.replace(/\/api\/v4\/mcp$/, '');
  let oauth = template.id === 'gitlab' && input.oauth && input.oauth.issuer !== issuer ? {...input.oauth,issuer,clientId:''} : input.oauth;
  if(template.id==='databricks' && input.oauth && new URL(input.endpoint).origin!==new URL(endpoint).origin) oauth={...input.oauth,issuer:'',clientId:''};
  return {...input, oauth, service:template.service, endpoint, template:{id:template.id,version:template.version}, templateParameters:parameters};
}
