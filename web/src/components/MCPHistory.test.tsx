import { cleanup,render,screen,within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach,expect,it } from 'vitest';
import { MCPHistory } from './MCPHistory';
import { fakeApi,apiError } from '../test/fakeApi';
afterEach(cleanup);
const event={seq:11,time:'2026-10-17T10:00:00Z',type:'tool.denied',connection:'team',graph:'g',node:'worker',execution:'exec',tool:'mcp.team.search',outcome:'rate_limited',durationMs:0,summary:'Call quota exceeded'};
it('loads only on request, preserves applied filters for pagination and resets explicitly',async()=>{
 let requests=0;
 const backend=fakeApi({'GET /api/v1/mcp/history':()=>{requests++;return requests===1?{entries:[],nextBefore:20,hasMore:true}:{entries:[event],hasMore:false};}});
 render(<MCPHistory/>);expect(backend.calls).toHaveLength(0);
 await userEvent.type(screen.getByLabelText('History connection ID'),'team');await userEvent.selectOptions(screen.getByLabelText('History outcome'),'rate_limited');await userEvent.click(screen.getByRole('button',{name:'Apply history filters'}));
 expect(await screen.findByText('No matches in this scanned page; older events remain.')).toBeTruthy();
 await userEvent.click(screen.getByRole('button',{name:'Older MCP events'}));expect(await screen.findByText('Call quota exceeded')).toBeTruthy();
 const urls=backend.fetch.mock.calls.map(c=>String(c[0]));expect(urls[0]).toContain('connection=team');expect(urls[1]).toContain('before=20');expect(urls[1]).toContain('outcome=rate_limited');
 expect(screen.getByRole('button',{name:'Older MCP events'}).hasAttribute('disabled')).toBe(true);
 await userEvent.click(screen.getByRole('button',{name:'Clear history filters'}));expect(screen.queryByText('Call quota exceeded')).toBeNull();expect(screen.getByText('Apply filters to load history.')).toBeTruthy();
});
it('reports failures while keeping the last successful page and permits retry',async()=>{
 let fail=false;
 fakeApi({'GET /api/v1/mcp/history':()=>fail?apiError(503,'History unavailable','unavailable'):{entries:[event],hasMore:false}});
 render(<MCPHistory/>);await userEvent.click(screen.getByRole('button',{name:'Apply history filters'}));expect(await screen.findByText('Call quota exceeded')).toBeTruthy();
 fail=true;await userEvent.click(screen.getByRole('button',{name:'Apply history filters'}));expect(await screen.findByText('History unavailable')).toBeTruthy();expect(within(screen.getByRole('table',{name:'MCP events'})).getByText('Call quota exceeded')).toBeTruthy();
 fail=false;await userEvent.click(screen.getByRole('button',{name:'Apply history filters'}));expect(screen.queryByText('History unavailable')).toBeNull();
});
it('keeps detail history bound to its connection when filters are cleared', async()=>{
 const backend=fakeApi({'GET /api/v1/mcp/history':()=>({entries:[],hasMore:false})});
 render(<MCPHistory connectionID="team"/>);
 expect(screen.getByLabelText('History connection ID').hasAttribute('readonly')).toBe(true);
 await userEvent.click(screen.getByRole('button',{name:'Clear history filters'}));
 expect(screen.getByLabelText('History connection ID')).toHaveProperty('value','team');
 await userEvent.click(screen.getByRole('button',{name:'Apply history filters'}));
 expect(backend.fetch.mock.calls.map(c=>String(c[0]))[0]).toContain('connection=team');
});
