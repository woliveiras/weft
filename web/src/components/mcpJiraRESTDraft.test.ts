import { describe, expect, it } from 'vitest';
import type { MCPConnection, MCPConnectionInput } from '../api/types';
import { prepareJiraRESTDraft } from './mcpJiraRESTDraft';
const cloudId='11111111-2222-3333-4444-555555555555';
const input:MCPConnectionInput={id:'jira',name:'Jira',service:'atlassian',endpoint:'https://mcp.atlassian.com/v2/mcp?tools=all',enabled:false};
const blank={email:'',token:'',clear:false};
const saved={...input,revision:3,credentialConfigured:true,status:'ready',tools:[],toolsRevision:1,updatedAt:'2026-11-20T10:00:00Z',updatedBy:'admin',jiraRest:{cloudId,credentialConfigured:true}} as MCPConnection;
describe('Jira REST draft preparation',()=>{
 it('keeps legacy configurations omitted and preserves an explicit saved mode without secret replacement',()=>{
  expect(prepareJiraRESTDraft(input,blank)).toEqual({});
  expect(prepareJiraRESTDraft({...input,jiraRest:{cloudId}},blank,saved)).toEqual({jiraRest:{cloudId}});
 });
 it('sends a separate replacement and never copies the MCP credential',()=>{
  expect(prepareJiraRESTDraft({...input,jiraRest:{cloudId},credential:{value:'synthetic-mcp'}},{...blank,email:'robot@example.test',token:'synthetic-rest'})).toEqual({jiraRest:{cloudId},jiraRestCredential:{username:'robot@example.test',value:'synthetic-rest'}});
 });
 it('clears only the REST credential while retaining explicit mode',()=>{
  expect(prepareJiraRESTDraft({...input,jiraRest:{cloudId}},{...blank,clear:true},saved)).toEqual({jiraRest:{cloudId},jiraRestCredential:{clear:true}});
 });
 it.each([
  [{email:'robot@example.test',token:'',clear:false},/email and API token together/i],
  [{email:'',token:'synthetic',clear:false},/email and API token together/i],
  [{email:'robot@example.test',token:'synthetic',clear:true},/clear and replace/i],
  [{email:'invalid',token:'synthetic',clear:false},/valid email/i],
  [{email:'robot@example.test',token:'bad\nvalue',clear:false},/control characters/i],
 ])('rejects incomplete or ambiguous replacements before a request', (draft,message)=>{
  expect(()=>prepareJiraRESTDraft({...input,jiraRest:{cloudId}},draft,saved)).toThrow(message);
 });
 it('requires a credential for new explicit mode and replacement or clear when the Cloud ID changes',()=>{
  expect(()=>prepareJiraRESTDraft({...input,jiraRest:{cloudId}},blank)).toThrow(/email and API token/i);
  expect(()=>prepareJiraRESTDraft({...input,jiraRest:{cloudId:'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'}},blank,saved)).toThrow(/replace or clear/i);
 });
 it.each(['bad/id','https://jira.example.test','', '../site'])('rejects invalid Cloud IDs',cloudId=>{
  expect(()=>prepareJiraRESTDraft({...input,jiraRest:{cloudId}},{...blank,email:'robot@example.test',token:'synthetic'})).toThrow(/Cloud ID/i);
 });
 it.each(['https://example.test/mcp','https://mcp.atlassian.com.attacker.test/v2/mcp','http://mcp.atlassian.com/v2/mcp','https://mcp.atlassian.com/v2/mcp/other','https://mcp.atlassian.com/v1/mcp','https://mcp.atlassian.com/v2/mcp?other=1'])('rejects incompatible endpoints instead of dropping saved REST settings',endpoint=>{
  expect(()=>prepareJiraRESTDraft({...input,endpoint,jiraRest:{cloudId}},blank,saved)).toThrow(/Atlassian MCP endpoint/i);
 });
 it('rejects an incompatible service instead of silently dropping saved configuration',()=>{
  expect(()=>prepareJiraRESTDraft({...input,service:'github',jiraRest:{cloudId}},blank,saved)).toThrow(/Atlassian MCP service/i);
 });
});

it('rejects incompatible restrictions without converting explicit REST to legacy mode',()=>{
 for (const policy of [{argumentPolicy:[{tool:'search'}]},{atlassianPolicy:{products:['jira'] as const,cloudIds:[cloudId]}},{metadataHeaders:{'x-dbt':'fixture'}}]) {
  expect(()=>prepareJiraRESTDraft({...input,...policy,jiraRest:{cloudId}} as MCPConnectionInput,blank,saved)).toThrow(/does not support/i);
 }
});
it('bounds UTF-8 credential bytes and rejects control characters',()=>{
 expect(()=>prepareJiraRESTDraft({...input,jiraRest:{cloudId}},{...blank,email:'robot@example.test',token:'x'.repeat(16385)})).toThrow(/size/);
 expect(()=>prepareJiraRESTDraft({...input,jiraRest:{cloudId}},{...blank,email:'robot@example.test',token:'x'.repeat(16384)})).not.toThrow();
 expect(()=>prepareJiraRESTDraft({...input,jiraRest:{cloudId}},{...blank,email:'robot@example.test',token:'x\u0000y'})).toThrow(/control/);
});
