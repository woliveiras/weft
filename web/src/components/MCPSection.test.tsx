import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useState } from 'react';
import { afterEach, expect, it } from 'vitest';
import type { MCPConnection, ToolGrant } from '../api/types';
import { fakeApi, apiError } from '../test/fakeApi';
import { MCPSection } from './MCPSection';
import { MCPGrants } from './MCPGrants';
afterEach(cleanup);
const connection:MCPConnection={id:'team',name:'Team',service:'github',endpoint:'https://example.com/mcp',enabled:true,status:'untested',revision:1,credentialConfigured:true,tools:[],toolsRevision:0,updatedAt:'2026-10-16T10:00:00Z',updatedBy:'admin'};
it('creates, discovers and edits with write-only credentials and optimistic concurrency',async()=>{
 let saved:MCPConnection|undefined;
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>saved?[saved]:[],
 'POST /api/v1/mcp/connections':()=>{saved={...connection};return saved;},
 'POST /api/v1/mcp/connections/team/tools/refresh':()=>{saved={...connection,status:'ready',revision:2,tools:[{name:'search',inputSchema:{type:'object'}}]};return saved;},
 'PUT /api/v1/mcp/connections/team':()=>apiError(409,'Connection changed; reload','conflict')});
 render(<MCPSection role="admin"/>);
 await userEvent.type(screen.getByLabelText('Connection ID'),'team');await userEvent.type(screen.getByLabelText('Connection name'),'Team');await userEvent.type(screen.getByLabelText('MCP endpoint'),'https://example.com/mcp');await userEvent.type(screen.getByLabelText('Bearer token'),'synthetic-token');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText('Credential configured · revision 2 · catalogue 0')).toBeTruthy();
 expect((screen.getByLabelText('Bearer token') as HTMLInputElement).value).toBe('');
 await userEvent.click(screen.getByRole('button',{name:'Refresh tools for Team'}));expect(await screen.findByText('mcp.team.search')).toBeTruthy();
 await userEvent.click(screen.getByRole('button',{name:'Edit Team'}));expect((screen.getByLabelText('Bearer token') as HTMLInputElement).value).toBe('');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));expect(await screen.findByText('Connection changed; reload')).toBeTruthy();
 expect(backend.find('PUT','/api/v1/mcp/connections/team')[0].headers['If-Match']).toBe('2');
 expect(backend.find('PUT','/api/v1/mcp/connections/team')[0].body).not.toHaveProperty('credential');
});
it('shows dependency errors without claiming an empty successful catalogue',async()=>{fakeApi({'GET /api/v1/mcp/connections':()=>apiError(503,'Storage unavailable','unavailable')});render(<MCPSection role="viewer"/>);expect(await screen.findByText('Storage unavailable')).toBeTruthy();expect(screen.queryByText('No MCP connections configured.')).toBeNull();expect(screen.queryByRole('button',{name:'Save MCP connection'})).toBeNull();});
it('limits operators to discovery and displays discovery failures',async()=>{fakeApi({'GET /api/v1/mcp/connections':()=>[{...connection,status:'unavailable',error:'Discovery failed'}]});render(<MCPSection role="operator"/>);expect(await screen.findByText('Discovery failed')).toBeTruthy();expect(screen.getByRole('button',{name:'Test Team'})).toBeTruthy();expect(screen.queryByRole('button',{name:'Edit Team'})).toBeNull();});
it('adds exact grants, preserves existing restrictions and removes stale grants',async()=>{
 fakeApi({'GET /api/v1/mcp/tools':()=>[{name:'mcp.team.search',description:'Search',schema:{},capability:'mcp'}]});
 let latest:(ToolGrant|string)[]=[];
 function Host(){const [grants,set]=useState<(ToolGrant|string)[]>([{name:'filesystem.read',paths:['src/**']},'mcp.removed.search']);latest=grants;return <MCPGrants grants={grants} onChange={set}/>;}
 render(<Host/>);const region=screen.getByRole('region',{name:'MCP permissions'});
 await userEvent.click(await within(region).findByRole('checkbox',{name:'mcp.team.search'}));expect(latest).toContainEqual({name:'mcp.team.search'});expect(latest).toContainEqual({name:'filesystem.read',paths:['src/**']});
 await userEvent.click(within(region).getByRole('checkbox',{name:/mcp.removed.search/}));expect(latest).not.toContain('mcp.removed.search');
});
it('prepares known services explicitly, validates GitLab domains and saves public OAuth without secrets',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[], 'GET /api/v1/mcp/oauth/config':()=>({callbackUrl:'https://weft.example/api/v1/mcp/oauth/callback'}),'POST /api/v1/mcp/connections':()=>connection});
 render(<MCPSection role="admin"/>);
 await userEvent.selectOptions(screen.getByRole('combobox',{name:/Service/}),'atlassian');
 await userEvent.click(screen.getByRole('button',{name:'Use OAuth defaults'}));
 expect((screen.getByLabelText('MCP endpoint') as HTMLInputElement).value).toBe('https://mcp.atlassian.com/v2/mcp?tools=all');
 expect(await screen.findByText('https://weft.example/api/v1/mcp/oauth/callback')).toBeTruthy();
 expect(screen.getByText(/REST API 3LO application is not assumed/)).toBeTruthy();
 await userEvent.selectOptions(screen.getByRole('combobox',{name:/Service/}),'gitlab');
 await userEvent.clear(screen.getByLabelText('GitLab instance URL'));await userEvent.type(screen.getByLabelText('GitLab instance URL'),'http://gitlab.example');
 await userEvent.click(screen.getByRole('button',{name:'Use OAuth defaults'}));expect(await screen.findByText(/GitLab instance must be an HTTPS origin/)).toBeTruthy();
 await userEvent.clear(screen.getByLabelText('GitLab instance URL'));await userEvent.type(screen.getByLabelText('GitLab instance URL'),'https://gitlab.example');
 await userEvent.click(screen.getByRole('button',{name:'Use OAuth defaults'}));
 await userEvent.type(screen.getByLabelText('Connection ID'),'gitlab-work');await userEvent.type(screen.getByLabelText('Connection name'),'GitLab work');await userEvent.type(screen.getByLabelText('OAuth client ID'),'registered-public-client');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 const body=backend.find('POST','/api/v1/mcp/connections')[0].body;
 expect(body).toMatchObject({id:'gitlab-work',service:'gitlab',endpoint:'https://gitlab.example/api/v4/mcp',oauth:{issuer:'https://gitlab.example',clientId:'registered-public-client',scopes:['mcp']}});
 expect(body).not.toHaveProperty('oauthClientSecret');expect(body).not.toHaveProperty('credential');
});
it('keeps automatic setup credential-free and preserves the manual alternative',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[], 'GET /api/v1/mcp/oauth/config':()=>({callbackUrl:'http://localhost:8080/api/v1/mcp/oauth/callback'}),'POST /api/v1/mcp/connections':()=>({...connection,oauth:{issuer:'https://issuer.example',clientId:'',scopes:[],status:'authorization_required',clientSecretConfigured:false}})});
 render(<MCPSection role="admin"/>);
 await userEvent.type(screen.getByLabelText('Connection ID'),'automatic');await userEvent.type(screen.getByLabelText('Connection name'),'Automatic');await userEvent.type(screen.getByLabelText('MCP endpoint'),'https://example.com/mcp');
 await userEvent.click(screen.getByLabelText('Use browser OAuth'));
 await userEvent.type(screen.getByLabelText('OAuth client secret (optional)'),'synthetic-manual-secret');
 await userEvent.selectOptions(screen.getByLabelText('Client registration method'),'auto');expect(screen.queryByLabelText('OAuth client ID')).toBeNull();expect(screen.queryByLabelText('OAuth client secret (optional)')).toBeNull();
 await userEvent.type(screen.getByLabelText('OAuth issuer'),'https://issuer.example');await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 const body=backend.find('POST','/api/v1/mcp/connections')[0].body;expect(body).toMatchObject({oauth:{registrationMode:'auto',clientId:''},oauthClientSecret:{clear:true}});expect(JSON.stringify(body)).not.toContain('synthetic-manual-secret');
 await userEvent.click(screen.getByLabelText('Use browser OAuth'));expect(screen.getByLabelText('OAuth client ID')).toBeTruthy();
});
it('requires disabled connections, explicit ID confirmation and a current revision for deletion',async()=>{
 let present=true;
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>present?[{...connection,enabled:false,status:'disabled'}]:[],
 'DELETE /api/v1/mcp/connections/team':()=>{present=false;return new Response(null,{status:204});}});
 render(<MCPSection role="admin"/>);
 await userEvent.click(await screen.findByRole('button',{name:'Delete Team'}));
 expect(screen.getByRole('button',{name:'Confirm deletion'}).hasAttribute('disabled')).toBe(true);
 await userEvent.click(screen.getByRole('button',{name:'Cancel deletion'}));expect(backend.find('DELETE','/api/v1/mcp/connections/team')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Delete Team'}));
 await userEvent.type(screen.getByLabelText('Type connection ID to confirm'),'team');await userEvent.click(screen.getByRole('button',{name:'Confirm deletion'}));
 expect(await screen.findByText('No MCP connections configured.')).toBeTruthy();
 expect(backend.find('DELETE','/api/v1/mcp/connections/team')[0].headers['If-Match']).toBe('1');
});
it('blocks deletion while enabled and presents stale deletion failures without hiding the connection',async()=>{
 let enabled=true;
 fakeApi({'GET /api/v1/mcp/connections':()=>[{...connection,enabled}],
 'DELETE /api/v1/mcp/connections/team':()=>apiError(409,'Connection changed; reload','conflict')});
 render(<MCPSection role="admin"/>);
 expect((await screen.findByRole('button',{name:'Delete Team'})).hasAttribute('disabled')).toBe(true);
 enabled=false;await userEvent.click(screen.getByRole('button',{name:'Reload MCP connections'}));
 await userEvent.click(await screen.findByRole('button',{name:'Delete Team'}));await userEvent.type(screen.getByLabelText('Type connection ID to confirm'),'team');await userEvent.click(screen.getByRole('button',{name:'Confirm deletion'}));
 expect(await screen.findByText('Connection changed; reload')).toBeTruthy();expect(screen.getByRole('article',{name:'MCP Team'})).toBeTruthy();
});
it('persists and edits a connection quota without exposing credentials',async()=>{
 let saved:MCPConnection={...connection,callsPerMinute:12};
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[saved],'PUT /api/v1/mcp/connections/team':call=>{saved={...saved,...call.body as object};return saved;}});
 render(<MCPSection role="admin"/>);await userEvent.click(await screen.findByRole('button',{name:'Edit Team'}));
 expect((screen.getByLabelText('Connection calls per minute') as HTMLInputElement).value).toBe('12');
 await userEvent.clear(screen.getByLabelText('Connection calls per minute'));await userEvent.type(screen.getByLabelText('Connection calls per minute'),'5');await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText('Connection quota: 5 calls per UTC minute')).toBeTruthy();expect(backend.find('PUT','/api/v1/mcp/connections/team')[0].body).toMatchObject({callsPerMinute:5});
});
it('submits Basic credentials together and clears both fields after saving',async()=>{
 let saved:MCPConnection|undefined;
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>saved?[saved]:[],
 'POST /api/v1/mcp/connections':()=>{saved={...connection,service:'atlassian',credentialType:'basic',enabled:false,status:'disabled'};return saved;}});
 render(<MCPSection role="admin"/>);
 await userEvent.type(screen.getByLabelText('Connection ID'),'team');
 await userEvent.type(screen.getByLabelText('Connection name'),'Team');
 await userEvent.selectOptions(screen.getByRole('combobox',{name:/^Service/}),'atlassian');
 await userEvent.type(screen.getByLabelText('MCP endpoint'),'https://mcp.atlassian.com/v2/mcp?tools=all');
 await userEvent.selectOptions(screen.getByLabelText('Credential type'),'basic');
 await userEvent.type(screen.getByLabelText('API token owner email'),'fixture@example.test');
 await userEvent.type(screen.getByLabelText('Personal API token'),'fixture-token');
 await userEvent.click(screen.getByLabelText('Connection enabled'));
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText('Credential configured · revision 1 · catalogue 0')).toBeTruthy();
 expect(backend.find('POST','/api/v1/mcp/connections')[0].body).toMatchObject({credentialType:'basic',credential:{username:'fixture@example.test',value:'fixture-token'}});
 await userEvent.click(screen.getByRole('button',{name:'Edit Team'}));
 expect((screen.getByLabelText('API token owner email') as HTMLInputElement).value).toBe('');
 expect((screen.getByLabelText('Personal API token') as HTMLInputElement).value).toBe('');
});

it('preserves an explicit stateless transport when editing and saving',async()=>{
 const current={...connection,transportProfile:'stateless-2026-07-28',enabled:false,status:'disabled'};
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[current],'PUT /api/v1/mcp/connections/team':()=>current});
 render(<MCPSection role="admin"/>);
 await userEvent.click(await screen.findByRole('button',{name:'Edit Team'}));
 expect((screen.getByLabelText('Transport profile') as HTMLSelectElement).value).toBe('stateless-2026-07-28');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(backend.find('PUT','/api/v1/mcp/connections/team')[0].body).toMatchObject({transportProfile:'stateless-2026-07-28'});
});

it('reviews native process configuration before saving and keeps environment values write-only',async()=>{
 let saved:MCPConnection|undefined;
 const backend=fakeApi({'GET /api/v1/info':()=>({nativeProfile:true}), 'GET /api/v1/mcp/connections':()=>saved?[saved]:[],
 'POST /api/v1/mcp/connections':({body})=>{const value=body as {localProcess:MCPConnection['localProcess']};saved={...connection,id:'local',name:'Local process',service:'generic',endpoint:'',localProcess:value.localProcess,environmentKeys:['FIXTURE_KEY']};return saved;},
 'POST /api/v1/mcp/connections/local/tools/refresh':()=>{saved={...saved!,status:'ready',revision:2,tools:[{name:'inspect',inputSchema:{type:'object'}}]};return saved;}});
 render(<MCPSection role="admin"/>);
 await userEvent.selectOptions(await screen.findByLabelText('Connection transport'),'stdio');
 await userEvent.type(screen.getByLabelText('Connection ID'),'local');await userEvent.type(screen.getByLabelText('Connection name'),'Local process');
 await userEvent.type(screen.getByLabelText('Executable path'),'/usr/local/bin/node');await userEvent.type(screen.getByLabelText('Working directory'),'/tmp/fixture');
 await userEvent.type(screen.getByLabelText('Process arguments'),'/tmp/fixture/server.js');
 await userEvent.click(screen.getByLabelText('Environment values (JSON)'));await userEvent.paste('{"FIXTURE_KEY":"synthetic-key"}');
 expect((screen.getByRole('button',{name:'Save MCP connection'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox',{name:'I reviewed this process and allow it to run on the Weft host.'}));
 await userEvent.type(screen.getByLabelText('Process arguments'),' --changed');
 expect((screen.getByRole('button',{name:'Save MCP connection'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.click(screen.getByRole('checkbox',{name:'I reviewed this process and allow it to run on the Weft host.'}));
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText('mcp.local.inspect')).toBeTruthy();
 expect(backend.find('POST','/api/v1/mcp/connections')[0].body).toMatchObject({endpoint:'',localProcess:{command:'/usr/local/bin/node'},processEnvironment:{FIXTURE_KEY:'synthetic-key'}});
 await userEvent.click(screen.getByRole('button',{name:'Edit Local process'}));
 expect((screen.getByLabelText('Environment values (JSON)') as HTMLInputElement).value).toBe('');
 expect(screen.getByText('Stored variable names: FIXTURE_KEY')).toBeTruthy();
 expect(document.body.textContent).not.toContain('synthetic-key');
});
