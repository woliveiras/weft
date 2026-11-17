import { expect,it } from 'vitest';
import { templateDraft,templateEndpoint } from './mcpTemplateDefaults';
import type { MCPTemplate } from '../api/types';
export const github:MCPTemplate={id:'github',version:2,name:'GitHub read-only',service:'github',endpoint:'https://api.githubcopilot.com/mcp/readonly',authentication:'bearer or OAuth',authenticationModes:['bearer','oauth'],parameters:[{name:'toolset',label:'Read-only toolset',default:'default',options:['default','repos','issues']}],guidance:'Review access',source:'https://example.com',reviewedAt:'2026-10-18',validation:'Fixture only'};
it('resolves reviewed parameters without accepting unknown values or detaching permissions',()=>{
 expect(templateEndpoint(github,{toolset:'repos'})).toBe('https://api.githubcopilot.com/mcp/x/repos/readonly');
 expect(()=>templateEndpoint(github,{toolset:'../write'})).toThrow();
 expect(()=>templateEndpoint(github,{token:'secret'})).toThrow();
 const input={id:'personal',name:'Personal',service:'github',enabled:false,endpoint:github.endpoint,oauth:{issuer:'https://github.com/login/oauth',clientId:'client',scopes:['read:user']}};
 const result=templateDraft(input,github);
 expect(result.oauth).toEqual(input.oauth);expect(result.enabled).toBe(false);expect(result).not.toHaveProperty('credential');
});
it('requires a new client identity when selecting another GitLab instance',()=>{
 const template={...github,id:'gitlab',version:1,service:'gitlab',endpoint:'https://gitlab.com/api/v4/mcp',authenticationModes:['oauth'],parameters:[{name:'instance',label:'GitLab instance URL',default:'https://gitlab.com'}]};
 const input={id:'work',name:'Work',service:'gitlab',enabled:true,endpoint:template.endpoint,oauth:{issuer:'https://gitlab.com',clientId:'old-client',scopes:['mcp']}};
 expect(templateDraft(input,template,{instance:'https://gitlab.example'}).oauth).toEqual({issuer:'https://gitlab.example',clientId:'',scopes:['mcp']});
 expect(()=>templateDraft({...input,oauth:undefined},template)).toThrow(/authentication/);
 expect(()=>templateEndpoint(template,{instance:'https://gitlab.example/?token=x'})).toThrow();
});

it('does not reinterpret typed credentials as Bearer during template upgrades',()=>{
 for(const credentialType of ['basic','langsmith'] as const){
  expect(()=>templateDraft({id:'typed',name:'Typed',service:'generic',endpoint:'https://fixture.example/mcp',enabled:true,credentialType},github)).toThrow(/authentication/);
 }
});
it('binds Databricks service paths and requires issuer/client review when changing workspace',()=>{
 const template:MCPTemplate={...github,id:'databricks',service:'databricks',version:1,parameters:[{name:'workspace',label:'Workspace',default:'https://workspace.example'},{name:'service',label:'Service',default:'catalog.schema.service',inputType:'text'}]};
 const endpoint='https://workspace.example/ai-gateway/mcp-services/catalog.schema.service';
 expect(templateEndpoint(template)).toBe(endpoint);
 const input={id:'data',name:'Data',service:'databricks',enabled:false,endpoint,oauth:{issuer:'https://workspace.example/oidc',clientId:'old',scopes:['ai-gateway','offline_access']}};
 expect(templateDraft(input,template,{workspace:'https://other.example'}).oauth).toEqual({...input.oauth,issuer:'',clientId:''});
 expect(templateDraft(input,template,{service:'team.data.github-reader'}).oauth).toEqual(input.oauth);
 for(const service of ['a.b','a.b/../c','a.b.c?token=x','a.b.%2e']) expect(()=>templateEndpoint(template,{service})).toThrow();
 for(const workspace of ['http://host.example','https://user:secret@host.example','https://host.example/path','https://host.example?']) expect(()=>templateEndpoint(template,{workspace})).toThrow();
});
