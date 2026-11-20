import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it } from 'vitest';
import type { MCPConnection } from '../api/types';
import { apiError, fakeApi } from '../test/fakeApi';
import { MCPSection } from './MCPSection';
afterEach(cleanup);
const cloudId='11111111-2222-3333-4444-555555555555';
const connection:MCPConnection={id:'jira',name:'Test Jira',service:'atlassian',endpoint:'https://mcp.atlassian.com/v2/mcp?tools=all',enabled:false,status:'disabled',revision:4,credentialConfigured:true,tools:[],toolsRevision:1,updatedAt:'2026-11-20T10:00:00Z',updatedBy:'admin',jiraRest:{cloudId,credentialConfigured:true}};
function setup(c=connection,fail=false){return fakeApi({
 'GET /api/v1/mcp/connections':()=>[c],
 'GET /api/v1/info':()=>({}),
 'GET /api/v1/mcp/oauth/config':()=>({callbackUrl:'http://localhost:8080/api/v1/mcp/oauth/callback'}),
 'PUT /api/v1/mcp/connections/jira':()=>fail?apiError(409,'Connection changed; reload','conflict'):{...c,revision:5},
 });}
async function edit(){await userEvent.click(await screen.findByRole('button',{name:'Edit Test Jira'}));}
it('preserves saved Jira REST configuration with empty write-only fields through an ordinary edit',async()=>{
 const backend=setup();render(<MCPSection role="admin"/>);await edit();
 expect((screen.getByLabelText('Jira Cloud ID') as HTMLInputElement).value).toBe(cloudId);
 expect((screen.getByLabelText('Jira REST API token owner email') as HTMLInputElement).value).toBe('');
 expect((screen.getByLabelText('Jira REST API token') as HTMLInputElement).value).toBe('');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(backend.find('PUT','/api/v1/mcp/connections/jira')[0]).toMatchObject({headers:{'If-Match':'4'},body:{jiraRest:{cloudId}}});
 expect(backend.find('PUT','/api/v1/mcp/connections/jira')[0].body).not.toHaveProperty('jiraRestCredential');
 expect(backend.calls.some(c=>c.method==='POST')).toBe(false);
});
it('sends replacement REST email and token separately from the MCP credential and clears draft secrets after save',async()=>{
 const backend=setup();render(<MCPSection role="admin"/>);await edit();
 await userEvent.type(screen.getByLabelText('Jira REST API token owner email'),'robot@example.test');
 await userEvent.type(screen.getByLabelText('Jira REST API token'),'synthetic-rest-token');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(backend.find('PUT','/api/v1/mcp/connections/jira')[0].body).toMatchObject({jiraRest:{cloudId},jiraRestCredential:{username:'robot@example.test',value:'synthetic-rest-token'}});
 expect(backend.find('PUT','/api/v1/mcp/connections/jira')[0].body).not.toHaveProperty('credential');
 await edit();expect((screen.getByLabelText('Jira REST API token') as HTMLInputElement).value).toBe('');
});
it('clears only REST credentials and explains that MCP authentication will not be used as fallback',async()=>{
 const backend=setup();render(<MCPSection role="admin"/>);await edit();
 await userEvent.click(screen.getByRole('checkbox',{name:'Clear stored Jira REST credential'}));
 expect(screen.getByText(/MCP credential will not be used as a fallback/i)).toBeTruthy();
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(backend.find('PUT','/api/v1/mcp/connections/jira')[0].body).toMatchObject({jiraRest:{cloudId},jiraRestCredential:{clear:true}});
 expect(backend.find('PUT','/api/v1/mcp/connections/jira')[0].body).not.toHaveProperty('credential');
});
it('preserves explicit REST metadata when preparing OAuth and reports API failures without discarding the draft',async()=>{
 const backend=setup(connection,true);render(<MCPSection role="admin"/>);await edit();
 await userEvent.type(screen.getByLabelText('Jira REST API token owner email'),'robot@example.test');
 await userEvent.type(screen.getByLabelText('Jira REST API token'),'synthetic-rest-token');
 await userEvent.click(screen.getByRole('checkbox',{name:'Use browser OAuth'}));
 await userEvent.type(screen.getByLabelText('OAuth issuer'),'https://auth.atlassian.com');
 await userEvent.type(screen.getByLabelText('OAuth client ID'),'synthetic-client');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText('Connection changed; reload')).toBeTruthy();
 expect(backend.find('PUT','/api/v1/mcp/connections/jira')[0].body).toMatchObject({jiraRest:{cloudId},oauth:{clientId:'synthetic-client'}});
 expect((screen.getByLabelText('Jira Cloud ID') as HTMLInputElement).value).toBe(cloudId);
 expect((screen.getByLabelText('Jira REST API token owner email') as HTMLInputElement).value).toBe('robot@example.test');
 expect((screen.getByLabelText('Jira REST API token') as HTMLInputElement).value).toBe('synthetic-rest-token');
});
it('resets entered REST secrets on service switch and exposes incompatible saved metadata without saving',async()=>{
 const backend=setup();render(<MCPSection role="admin"/>);await edit();
 await userEvent.type(screen.getByLabelText('Jira REST API token owner email'),'robot@example.test');
 await userEvent.type(screen.getByLabelText('Jira REST API token'),'synthetic-rest-token');
 await userEvent.selectOptions(screen.getByLabelText('Service'),'github');
 expect((screen.getByLabelText('Jira REST API token') as HTMLInputElement).value).toBe('');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText('Jira REST access requires an Atlassian MCP service. Restore the saved service before saving.')).toBeTruthy();
 expect(backend.find('PUT','/api/v1/mcp/connections/jira')).toHaveLength(0);
});
it('shows REST storage availability separately from MCP discovery and never treats it as verified REST access',async()=>{
 setup({...connection,status:'ready',enabled:true,jiraRest:{cloudId,credentialConfigured:false}});
 render(<MCPSection role="viewer" presentation="detail" connectionID="jira"/>);
 expect(await screen.findByText('REST credential missing')).toBeTruthy();
 expect(screen.getByText(/MCP Ready confirms tool discovery only/i)).toBeTruthy();
 expect(screen.getByText(/Test a scoped Jira REST read before activating an issue trigger/i)).toBeTruthy();
 expect(screen.queryByRole('button',{name:'Edit Test Jira'})).toBeNull();
});
it('creates optional explicit REST access with its own credential while keeping ordinary MCP authentication separate',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[], 'GET /api/v1/info':()=>({}),
 'POST /api/v1/mcp/connections':()=>connection});
 render(<MCPSection role="admin" presentation="create"/>);
 await userEvent.type(screen.getByLabelText('Connection ID'),'jira');
 await userEvent.type(screen.getByLabelText('Connection name'),'Test Jira');
 await userEvent.selectOptions(screen.getByLabelText('Service'),'atlassian');
 await userEvent.type(screen.getByLabelText('MCP endpoint'),connection.endpoint);
 await userEvent.type(screen.getByLabelText('Bearer token'),'synthetic-mcp-token');
 await userEvent.click(screen.getByRole('checkbox',{name:'Configure separate Jira REST access'}));
 await userEvent.type(screen.getByLabelText('Jira Cloud ID'),cloudId);
 await userEvent.type(screen.getByLabelText('Jira REST API token owner email'),'robot@example.test');
 await userEvent.type(screen.getByLabelText('Jira REST API token'),'synthetic-rest-token');
 await userEvent.click(screen.getByRole('checkbox',{name:'Connection enabled'}));
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(backend.find('POST','/api/v1/mcp/connections')[0].body).toMatchObject({credential:{value:'synthetic-mcp-token'},jiraRest:{cloudId},jiraRestCredential:{username:'robot@example.test',value:'synthetic-rest-token'}});
});
it('blocks incomplete REST replacement locally and preserves the entered draft for correction',async()=>{
 const backend=setup();render(<MCPSection role="admin"/>);await edit();
 await userEvent.type(screen.getByLabelText('Jira REST API token'),'synthetic-rest-token');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText(/email and API token together/i)).toBeTruthy();
 expect(backend.find('PUT','/api/v1/mcp/connections/jira')).toHaveLength(0);
 expect((screen.getByLabelText('Jira REST API token') as HTMLInputElement).value).toBe('synthetic-rest-token');
});
