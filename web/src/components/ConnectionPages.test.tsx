import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it } from 'vitest';
import { fakeApi, apiError } from '../test/fakeApi';
import { ConnectionsList } from './ConnectionsList';
import { MCPSection } from './MCPSection';
import { ConnectionGrants } from './ConnectionGrants';
const connection={id:'team',name:'Team',service:'github',endpoint:'https://example.com/mcp',enabled:true,status:'ready',revision:1,credentialConfigured:true,tools:[{name:'search',description:'Search repositories',inputSchema:{type:'object'}}],toolsRevision:1,updatedAt:'2026-10-16T10:00:00Z',updatedBy:'admin'};
afterEach(cleanup);
it('lists and filters connections without loading setup or mutating permissions',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[connection]});
 render(<ConnectionsList role="admin"/>);
 expect((await screen.findByRole('link',{name:'Team'})).getAttribute('href')).toBe('#/connections/c/team/overview');
 expect(screen.getByRole('link',{name:'Add connection'})).toBeTruthy();
 await userEvent.type(screen.getByLabelText('Search connections'),'missing');
 expect(screen.getByText('No connections match these filters.')).toBeTruthy();
 expect(backend.calls.map(c=>c.method)).toEqual(['GET']);
});
it('distinguishes list failures from empty results and retries for viewers',async()=>{
 let fail=true;fakeApi({'GET /api/v1/mcp/connections':()=>fail?apiError(503,'Offline','unavailable'):[]});
 render(<ConnectionsList role="viewer"/>);
 expect(await screen.findByText('Offline')).toBeTruthy();
 expect(screen.queryByText('No MCP connections configured.')).toBeNull();
 expect(screen.queryByRole('link',{name:'Add connection'})).toBeNull();
 fail=false;await userEvent.click(screen.getByRole('button',{name:'Reload MCP connections'}));
 expect(await screen.findByText('No MCP connections configured.')).toBeTruthy();
});
it('scopes tools and settings to one connection and preserves viewer restrictions',async()=>{
 fakeApi({'GET /api/v1/mcp/connections':()=>[connection,{...connection,id:'other',name:'Other'}]});
 const view=render(<MCPSection role="viewer" presentation="detail" connectionID="team" tab="tools"/>);
 expect(await screen.findByText('mcp.team.search')).toBeTruthy();expect(screen.queryByRole('article',{name:'MCP Other'})).toBeNull();
 expect(screen.queryByRole('button',{name:'Refresh tools for Team'})).toBeNull();
 await userEvent.type(screen.getByLabelText('Search tools'),'absent');expect(screen.getByText('No tools match this search.')).toBeTruthy();
 view.rerender(<MCPSection role="viewer" presentation="detail" connectionID="team" tab="settings"/>);
 expect(screen.getByText('Only administrators can edit connection settings.')).toBeTruthy();
 expect(screen.queryByRole('button',{name:'Edit Team'})).toBeNull();
});
it('shows missing connections without creating a form',async()=>{
 fakeApi({'GET /api/v1/mcp/connections':()=>[]});
 render(<MCPSection role="admin" presentation="detail" connectionID="missing"/>);
 expect(await screen.findByText('Connection not found. It may have been deleted.')).toBeTruthy();
 expect(screen.queryByRole('button',{name:'Save MCP connection'})).toBeNull();
});
it('preserves revisions, credential omission and edit drafts after conflicts',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[connection], 'PUT /api/v1/mcp/connections/team':()=>apiError(409,'Revision conflict','conflict')});
 render(<MCPSection role="admin" presentation="detail" connectionID="team" tab="settings"/>);
 await userEvent.click(await screen.findByRole('button',{name:'Edit Team'}));
 expect(screen.getByLabelText('Bearer token')).toHaveProperty('value','');
 await userEvent.clear(screen.getByLabelText('Connection name'));await userEvent.type(screen.getByLabelText('Connection name'),'Updated');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText('Revision conflict')).toBeTruthy();expect(screen.getByLabelText('Connection name')).toHaveProperty('value','Updated');
 const call=backend.find('PUT','/api/v1/mcp/connections/team')[0];expect(call.headers['If-Match']).toBe('1');expect(call.body).not.toHaveProperty('credential');
});
it('shows explicit and inherited saved grants, without treating empty grants as inheritance',async()=>{
 fakeApi({'GET /api/v1/graphs':()=>[{graph:{metadata:{id:'g',name:'Graph'},spec:{defaults:{tools:['mcp.team.search']},nodes:[{id:'inherited'},{id:'empty',tools:[]},{id:'other',tools:['mcp.other.search']},{id:'explicit',tools:[{name:'mcp.team.search'}]}]}}}]});
 render(<ConnectionGrants connectionID="team"/>);
 expect(await screen.findByRole('link',{name:'inherited'})).toBeTruthy();expect(screen.getByRole('link',{name:'explicit'})).toBeTruthy();
 expect(screen.queryByRole('link',{name:'empty'})).toBeNull();expect(screen.queryByRole('link',{name:'other'})).toBeNull();
});
it('does not report absent grants on a graph API failure',async()=>{
 fakeApi({'GET /api/v1/graphs':()=>apiError(403,'Forbidden','forbidden')});
 render(<ConnectionGrants connectionID="team"/>);
 expect(await screen.findByText('Forbidden')).toBeTruthy();expect(screen.queryByText('No saved agent grants for this connection.')).toBeNull();
});
it('retains OAuth consent gates on detail and does not authorize operators implicitly',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[{...connection,oauth:{issuer:'https://issuer.example',clientId:'client',scopes:['read'],status:'authorized',clientSecretConfigured:false}}],'GET /api/v1/graphs':()=>[]});
 const view=render(<MCPSection role="operator" presentation="detail" connectionID="team"/>);
 expect(await screen.findByText('Authorized')).toBeTruthy();expect(screen.queryByRole('button',{name:'Reauthorize Team'})).toBeNull();
 expect(screen.getByRole('button',{name:'Refresh tools for Team'})).toBeTruthy();
 view.rerender(<MCPSection role="admin" presentation="detail" connectionID="team"/>);
 await userEvent.click(screen.getByRole('button',{name:'Revoke OAuth Team'}));
 expect(screen.getByRole('button',{name:'Confirm revoke Team'})).toBeTruthy();
 await userEvent.click(screen.getByRole('button',{name:'Cancel revocation'}));
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});

it('automatically discovers after saving, distinguishes pending discovery and never grants tools',async()=>{
 let saved=false;let finish!:(value:unknown)=>void;
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>saved?[connection]:[],
  'GET /api/v1/mcp/templates':()=>[],
  'POST /api/v1/mcp/connections':()=>{saved=true;return connection;},
  'POST /api/v1/mcp/connections/team/tools/refresh':()=>new Promise(resolve=>{finish=resolve;})});
 render(<MCPSection role="admin" presentation="create"/>);
 await userEvent.type(screen.getByLabelText('Connection ID'),'team');
 await userEvent.type(screen.getByLabelText('Connection name'),'Team');
 await userEvent.type(screen.getByLabelText('MCP endpoint'),'https://example.com/mcp');
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText('Connection saved. Discovering tools…')).toBeTruthy();
 await act(async()=>finish(connection));
 await waitFor(()=>expect(location.hash).toBe('#/connections/c/team/overview'));
 expect(backend.calls.filter(c=>c.method==='POST').map(c=>c.path)).toEqual(['/api/v1/mcp/connections','/api/v1/mcp/connections/team/tools/refresh']);
});

it('keeps a saved connection recoverable when discovery fails without submitting it again',async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/connections':()=>[connection],
 'PUT /api/v1/mcp/connections/team':()=>connection,
 'POST /api/v1/mcp/connections/team/tools/refresh':()=>apiError(503,'Discovery unavailable','unavailable')});
 render(<MCPSection role="admin" presentation="detail" connectionID="team" tab="settings"/>);
 await userEvent.click(await screen.findByRole('button',{name:'Edit Team'}));
 await userEvent.click(screen.getByRole('button',{name:'Save MCP connection'}));
 expect(await screen.findByText('Connection saved. Tool discovery could not finish. Open the connection to retry.')).toBeTruthy();
 expect(screen.getByRole('link',{name:'Open saved connection'}).getAttribute('href')).toBe('#/connections/c/team/overview');
 expect(backend.find('PUT','/api/v1/mcp/connections/team')).toHaveLength(1);
});
