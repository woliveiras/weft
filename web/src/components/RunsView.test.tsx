import { act,cleanup,render,screen,within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach,expect,it,vi } from 'vitest';
import { apiError,fakeApi } from '../test/fakeApi';
import { RunsView } from './RunsView';
afterEach(cleanup);
const execution={id:'e',graphId:'g',nodeId:'worker',graphVersion:3,replica:0,iteration:1,status:'succeeded',summary:'Completed inspection',toolCalls:2,startedAt:'2026-10-17T10:00:00Z'};
const event={id:'denied',graphId:'g',executionId:'e',nodeId:'worker',seq:2,type:'tool.denied',time:'2026-10-17T10:00:00Z',data:{tool:'mcp.demo.read',outcome:'rate_limited'}};
function routes(){return {'GET /api/v1/executions/e':()=>execution,'GET /api/v1/approvals':()=>[],'GET /api/v1/graphs/g/events':()=>[event]};}
it('lists bounded runs and filters them without mutations',async()=>{
 const backend=fakeApi({'GET /api/v1/graphs/g/executions':()=>[execution,{...execution,id:'secret',graphId:'other',summary:'Other graph'}]});
 render(<RunsView graphId="g"/>);expect((await screen.findByRole('link',{name:'e'})).getAttribute('href')).toBe('#/g/g/runs?execution=e&section=result');expect(screen.queryByText('Other graph')).toBeNull();
 await userEvent.type(screen.getByLabelText('Search runs'),'missing');expect(screen.getByText('No runs match these filters.')).toBeTruthy();
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);expect(String(backend.fetch.mock.calls[0][0])).toContain('limit=100');
});
it('reports list errors and recovers without presenting failure as empty success',async()=>{
 let fail=true;fakeApi({'GET /api/v1/graphs/g/executions':()=>fail?apiError(503,'Storage unavailable','unavailable'):[]});
 render(<RunsView graphId="g"/>);expect(await screen.findByText('Storage unavailable')).toBeTruthy();expect(screen.queryByText('No executions recorded yet.')).toBeNull();
 fail=false;await userEvent.click(screen.getByRole('button',{name:'Refresh runs'}));expect(await screen.findByText('No executions recorded yet.')).toBeTruthy();
});
it('does not expose an execution from another graph or load its linked records',async()=>{
 const backend=fakeApi({'GET /api/v1/executions/e':()=>({...execution,graphId:'other',summary:'Private result'})});
 render(<RunsView graphId="g" executionId="e"/>);expect(await screen.findByText('This execution does not belong to the selected graph.')).toBeTruthy();expect(screen.queryByText('Private result')).toBeNull();expect(backend.calls).toHaveLength(1);
});
it('keeps successful execution distinct from blocked tool calls and scopes events exactly',async()=>{
 fakeApi({...routes(),'GET /api/v1/graphs/g/events':()=>[event,{...event,id:'other',executionId:'other',data:{tool:'Private tool'}}]});
 render(<RunsView graphId="g" executionId="e" section="tools"/>);
 expect(await screen.findByText('1 blocked tool calls in this window. The execution status remains Completed.')).toBeTruthy();expect(screen.queryByText(/Private tool/)).toBeNull();expect(screen.getByText('Completed',{exact:true})).toBeTruthy();
});
it('links messages only by persisted IDs, not a shared agent',async()=>{
 fakeApi({...routes(),'GET /api/v1/graphs/g/messages':()=>[{graphId:'g',message:{id:'linked',type:'Linked message',metadata:{execution_id:'e'}}},{graphId:'g',consumer:'worker',message:{id:'unlinked',type:'Unlinked message'}}]});
 render(<RunsView graphId="g" executionId="e" section="messages"/>);expect(await screen.findByText('Linked message')).toBeTruthy();expect(screen.queryByText('Unlinked message')).toBeNull();
});
it('preserves approval role gates and excludes unrelated approvals',async()=>{
 const approval={id:'a',graphId:'g',executionId:'e',kind:'environment',status:'pending',title:'Review environment',createdAt:'2026-10-17T10:00:00Z'};
 const backend=fakeApi({...routes(),'GET /api/v1/approvals':()=>[approval,{...approval,id:'b',executionId:'other',title:'Other approval'}]});
 const view=render(<RunsView graphId="g" executionId="e" role="operator"/>);
 expect(await screen.findByText('Review environment')).toBeTruthy();expect(screen.queryByRole('button',{name:'Approve'})).toBeNull();expect(screen.queryByText('Other approval')).toBeNull();
 view.rerender(<RunsView graphId="g" executionId="e" role="admin"/>);expect(within(screen.getByRole('region',{name:'Run approvals'})).getByRole('button',{name:'Approve'})).toBeTruthy();expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});
it('reports missing executions and event failures with explicit retry',async()=>{
 let fail=true;fakeApi({...routes(),'GET /api/v1/graphs/g/events':()=>fail?apiError(503,'Event history unavailable','unavailable'):[event]});
 const view=render(<RunsView graphId="g" executionId="e" section="timeline"/>);expect(await screen.findByText('Event history unavailable')).toBeTruthy();expect(screen.queryByText('No linked events in this recent window.')).toBeNull();
 fail=false;await userEvent.click(screen.getByRole('button',{name:'Refresh run events'}));expect(await screen.findByText('tool.denied',{exact:true})).toBeTruthy();
 fakeApi({'GET /api/v1/executions/missing':()=>apiError(404,'Execution not found','not_found')});view.rerender(<RunsView graphId="g" executionId="missing"/>);expect(await screen.findByText('Execution not found')).toBeTruthy();expect(screen.queryByText('Completed inspection')).toBeNull();
});

it('waits for an in-flight response before scheduling another poll',async()=>{
 vi.useFakeTimers();
 try {
  let finish!:(value:unknown[])=>void;
  const pending=new Promise<unknown[]>(resolve=>{finish=resolve;});
  const backend=fakeApi({'GET /api/v1/graphs/g/executions':()=>pending});
  render(<RunsView graphId="g"/>);
  await act(async()=>{await vi.advanceTimersByTimeAsync(6000);});expect(backend.calls).toHaveLength(1);
  await act(async()=>{finish([]);});expect(screen.getByText('No executions recorded yet.')).toBeTruthy();
  await act(async()=>{await vi.advanceTimersByTimeAsync(5000);});expect(backend.calls).toHaveLength(2);
 } finally {cleanup();vi.useRealTimers();}
});
it('surfaces blocked calls on Result without changing the persisted outcome',async()=>{
 fakeApi(routes());render(<RunsView graphId="g" executionId="e"/>);
 expect(await screen.findByText('1 blocked tool calls in this window. The execution status remains Completed.')).toBeTruthy();
 expect(screen.getByText('Completed inspection')).toBeTruthy();expect(screen.getByRole('link',{name:'Inspect tool calls'}).getAttribute('href')).toBe('#/g/g/runs?execution=e&section=tools');
});

it('counts invocation events separately from completion events and retained attempt totals',async()=>{
 const events=['tool.invoked','tool.completed','tool.invoked','tool.failed'].map((type,i)=>({...event,id:String(i),seq:i,type}));
 fakeApi({...routes(),'GET /api/v1/graphs/g/events':()=>events});
 render(<RunsView graphId="g" executionId="e" section="tools"/>);
 expect(await screen.findByText('2 tool invocations observed · 4 tool lifecycle events in this window. These are not additional calls.')).toBeTruthy();
});

it('shows a scoped child-parent link and explains inclusive usage without configuration changes', async()=>{
 const backend=fakeApi({...routes(),'GET /api/v1/executions/e':()=>({...execution,parentExecutionId:'parent/id',delegationTemplate:'analysis',delegationConfig:{policy:{revision:2,policy:{approvalMode:'automatic'}},profile:{revision:3}}})});
 render(<RunsView graphId="g" executionId="e"/>);
 const context=await screen.findByRole('complementary',{name:'Delegation context'});
 expect(within(context).getByRole('link',{name:'View parent execution'}).getAttribute('href')).toBe('#/g/g/runs?execution=parent%2Fid&section=result');
 expect(context.textContent).toContain('No tools or workspace access');
 expect(context.textContent).toContain('do not add both totals');
 expect(context.textContent).toContain('Policy revision 2');
 expect(context.textContent).toContain('Profile revision 3');
 expect(context.textContent).toContain('Approval mode: automatic');
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});

it('shows persisted delegation audit and leaves legacy evidence explicitly unknown',async()=>{
 const child={...execution,parentExecutionId:'p',delegationObjective:'Inspect supplied text',delegationModel:{provider:'fixture',name:'test'},delegationAudit:{runtime:'native',effectiveGrants:[],workspaceAccess:false,recursiveAccess:false,maxOutputTokens:512,decision:'rejected',decidedBy:'reviewer'}};
 fakeApi({...routes(),'GET /api/v1/executions/e':()=>child});
 render(<RunsView graphId="g" executionId="e"/>);
 await screen.findByRole('complementary',{name:'Delegation context'});
 await userEvent.click(screen.getByText('Delegation audit'));
 expect(screen.getByText('Decision: rejected · reviewer')).toBeTruthy();
 expect(screen.getByText('Effective grants: None')).toBeTruthy();
 expect(screen.getByText('Model: fixture/test')).toBeTruthy();
 expect(screen.getByText('Objective: Inspect supplied text')).toBeTruthy();
 cleanup();
 fakeApi({...routes(),'GET /api/v1/executions/e':()=>({...execution,parentExecutionId:'p'})});
 render(<RunsView graphId="g" executionId="e"/>);
 expect(await screen.findByText('Access and decision snapshot not recorded for this historical execution.')).toBeTruthy();
});
