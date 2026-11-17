import type { MCPConnectionInput } from '../api/types';

export const atlassianIssuer = 'https://auth.atlassian.com/VCeDsk8ZHncYF1g234fKtc4lNipbBhu3';
export const serviceOptions = [
 ['generic','Custom'],['github','GitHub'],['atlassian','Atlassian'],['gitlab','GitLab'],['sentry','Sentry'],['databricks','Databricks'],
 ['bitbucket','Bitbucket (existing profile)'],['jira','Jira (existing profile)'],['confluence','Confluence (existing profile)'],
] as const;

export function gitLabOrigin(value:string):string {
 let url:URL;
 try { url=new URL(value); } catch { throw new Error('Enter an HTTPS GitLab instance origin, such as https://gitlab.com.'); }
 if(url.protocol!=='https:'||!url.hostname||url.username||url.password||url.search||url.hash||url.pathname!=='/') throw new Error('GitLab instance must be an HTTPS origin without a path, credentials, query or fragment.');
 return url.origin;
}

export function oauthDefaults(service:string, instance='https://gitlab.com'):Pick<MCPConnectionInput,'endpoint'|'oauth'> {
 if(service==='databricks') return {endpoint:'https://workspace.example/ai-gateway/mcp-services/catalog.schema.service',oauth:{issuer:'',clientId:'',scopes:['ai-gateway','offline_access']}};
 if(service==='sentry') return {endpoint:'https://mcp.sentry.dev/mcp',oauth:{issuer:'https://mcp.sentry.dev',clientId:'',scopes:['org:read']}};
 if(service==='github') return {endpoint:'https://api.githubcopilot.com/mcp/',oauth:{issuer:'https://github.com/login/oauth',clientId:'',scopes:['read:user','offline_access']}};
 if(service==='atlassian'||['jira','confluence','bitbucket'].includes(service)) return {endpoint:'https://mcp.atlassian.com/v2/mcp?tools=all',oauth:{issuer:atlassianIssuer,clientId:'',scopes:['read:me','offline_access','read:jira:agent-interface','read:confluence:agent-interface']}};
 if(service==='gitlab'){const origin=gitLabOrigin(instance);return {endpoint:origin+'/api/v4/mcp',oauth:{issuer:origin,clientId:'',scopes:['mcp']}};}
 return {endpoint:'',oauth:{issuer:'',clientId:'',scopes:[]}};
}
