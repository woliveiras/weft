import { expect, it } from 'vitest';
import { gitLabOrigin, oauthDefaults, atlassianIssuer } from './mcpServices';
it('supplies distinct MCP contracts, never REST scopes or shared client credentials',()=>{
 expect(oauthDefaults('github').oauth?.clientId).toBe('');
 expect(oauthDefaults('atlassian').endpoint).toBe('https://mcp.atlassian.com/v2/mcp?tools=all');
 expect(oauthDefaults('atlassian').oauth?.issuer).toBe(atlassianIssuer);
 expect(oauthDefaults('atlassian').oauth?.scopes).toContain('read:jira:agent-interface');
 expect(oauthDefaults('gitlab','https://gitlab.example.org/')).toEqual({endpoint:'https://gitlab.example.org/api/v4/mcp',oauth:{issuer:'https://gitlab.example.org',clientId:'',scopes:['mcp']}});
 expect(oauthDefaults('generic')).toEqual({endpoint:'',oauth:{issuer:'',clientId:'',scopes:[]}});
 const first=oauthDefaults('github');first.oauth!.scopes.push('repo');expect(oauthDefaults('github').oauth?.scopes).not.toContain('repo');
});
it('rejects unsafe or ambiguous instance input',()=>{
 for(const value of ['gitlab.example','http://gitlab.example','https://user:secret@gitlab.example','https://gitlab.example/path','https://gitlab.example/?token=x','https://gitlab.example/#x']) expect(()=>gitLabOrigin(value)).toThrow();
});
it('starts Sentry with a public MCP issuer and minimal advertised scope',()=>{
 expect(oauthDefaults('sentry')).toEqual({endpoint:'https://mcp.sentry.dev/mcp',oauth:{issuer:'https://mcp.sentry.dev',clientId:'',scopes:['org:read']}});
 const first=oauthDefaults('sentry');first.oauth!.scopes.push('project:write');
 expect(oauthDefaults('sentry').oauth!.scopes).toEqual(['org:read']);
});

it('includes Atlassian identity and renewal scopes only in explicitly requested service defaults',()=>{
 const expected=['read:me','read:account','offline_access','read:jira:agent-interface','read:confluence:agent-interface'];
 for(const service of ['atlassian','jira','confluence','bitbucket']) {
  const first=oauthDefaults(service);
  expect(first.oauth?.scopes).toEqual(expected);
  first.oauth!.scopes.push('write:jira:agent-interface');
  expect(oauthDefaults(service).oauth?.scopes).toEqual(expected);
 }
 expect(oauthDefaults('generic').oauth?.scopes).toEqual([]);
 expect(oauthDefaults('github').oauth?.scopes).not.toContain('read:account');
});
