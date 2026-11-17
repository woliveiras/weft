import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {fakeApi,apiError} from '../test/fakeApi';
import {DelegationSection} from './DelegationSection';
const current={revision:0,policy:{enabled:true,approvalMode:'required',maxDepth:1,maxChildrenPerExecution:3,maxConcurrentChildren:4,maxDurationSeconds:120,maxOutputTokens:1024,maxObjectiveBytes:4096,maxResultBytes:16384,maxTreeTokens:65536}};
const profiles=[{revision:0,profile:{id:'analysis',name:'Analysis',instructions:'Review text',enabled:true,maxOutputTokens:1024}}];
afterEach(cleanup);
it('reads policy and profiles without exposing edits to non-admins',async()=>{
 const backend=fakeApi({'GET /api/v1/delegation-policy':()=>current,'GET /api/v1/delegation-profiles':()=>profiles});
 render(<DelegationSection canManage={false}/>);
 expect((await screen.findByLabelText('Approval mode') as HTMLSelectElement).disabled).toBe(true);
 expect(await screen.findByText('Analysis')).toBeTruthy();
 expect(screen.queryByRole('button',{name:'New delegation profile'})).toBeNull();
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});
it('reviews automatic policy before saving without changing agent grants',async()=>{
 const backend=fakeApi({'GET /api/v1/delegation-policy':()=>current,'GET /api/v1/delegation-profiles':()=>profiles,'PUT /api/v1/delegation-policy':c=>{expect(c.body).toEqual({...current.policy,approvalMode:'automatic'});return {...current,revision:1};}});
 const view=render(<DelegationSection canManage/>);
 await userEvent.selectOptions(await screen.findByLabelText('Approval mode'),'automatic');
 await userEvent.click(screen.getByRole('button',{name:'Review delegation policy'}));
 expect(screen.getByText(/without individual delegation approval/)).toBeTruthy();
 expect(backend.calls.filter(c=>c.method==='PUT')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Cancel policy changes'}));
 await userEvent.click(screen.getByRole('button',{name:'Review delegation policy'}));
 await userEvent.click(screen.getByRole('button',{name:'Confirm delegation policy'}));
 expect(await screen.findByText(/Delegation policy saved/)).toBeTruthy();
 expect(backend.calls.filter(c=>c.method==='PUT')).toHaveLength(1);
 await userEvent.click(screen.getByRole('button',{name:'Review delegation policy'}));
 view.rerender(<DelegationSection canManage={false}/>);
 expect(screen.queryByRole('button',{name:'Confirm delegation policy'})).toBeNull();
});
it('creates a reviewed profile and handles a stale revision without retry',async()=>{
 const backend=fakeApi({'GET /api/v1/delegation-policy':()=>current,'GET /api/v1/delegation-profiles':()=>profiles,'POST /api/v1/delegation-profiles':()=>apiError(409,'revision conflict','conflict')});
 render(<DelegationSection canManage/>);await screen.findByText('Analysis');
 await userEvent.click(screen.getByRole('button',{name:'New delegation profile'}));
 expect((screen.getByRole('button',{name:'Review delegation profile'}) as HTMLButtonElement).disabled).toBe(true);
 await userEvent.type(screen.getByLabelText('Profile ID'),'review');await userEvent.type(screen.getByLabelText('Profile name'),'Review');await userEvent.type(screen.getByLabelText('Profile instructions'),'Review the provided design');
 await userEvent.click(screen.getByRole('button',{name:'Review delegation profile'}));
 await userEvent.click(screen.getByRole('button',{name:'Confirm delegation profile'}));
 expect(await screen.findByText(/Save could not be confirmed: revision conflict/)).toBeTruthy();
 expect(backend.calls.filter(c=>c.method==='POST')).toHaveLength(1);
 expect(screen.queryByLabelText('Profile ID')).toBeNull();
});
it('keeps failed reads unavailable and rejects invalid policy limits',async()=>{
 let fail=true;
 fakeApi({'GET /api/v1/delegation-policy':()=>fail?apiError(500,'storage unavailable','internal'):current,'GET /api/v1/delegation-profiles':()=>profiles});
 render(<DelegationSection canManage/>);expect(await screen.findByText('storage unavailable')).toBeTruthy();expect(screen.queryByLabelText('Approval mode')).toBeNull();
 fail=false;await userEvent.click(screen.getByRole('button',{name:'Reload delegation policy'}));const input=await screen.findByLabelText('Concurrent children');
 await userEvent.clear(input);await userEvent.type(input,'33');expect((screen.getByRole('button',{name:'Review delegation policy'}) as HTMLButtonElement).disabled).toBe(true);
});

it('reviews and persists the tree token ceiling through the configuration API',async()=>{
 const backend=fakeApi({'GET /api/v1/delegation-policy':()=>current,'GET /api/v1/delegation-profiles':()=>profiles,'PUT /api/v1/delegation-policy':c=>({...current,revision:1,policy:c.body})});
 render(<DelegationSection canManage/>);
 const input=await screen.findByLabelText('Tree token budget');
 await userEvent.clear(input);await userEvent.type(input,'8000');
 await userEvent.click(screen.getByRole('button',{name:'Review delegation policy'}));
 expect(backend.calls.filter(c=>c.method==='PUT')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Confirm delegation policy'}));
 await screen.findByText(/Delegation policy saved/);
 expect(backend.find('PUT','/api/v1/delegation-policy')[0].body).toEqual({...current.policy,maxTreeTokens:8000});
});
it('reviews inherited capability names while preserving the agent-grant boundary',async()=>{
 const backend=fakeApi({'GET /api/v1/delegation-policy':()=>current,'GET /api/v1/delegation-profiles':()=>profiles,'POST /api/v1/delegation-profiles':c=>{expect(c.body).toMatchObject({tools:['filesystem.read','mcp.personal.get_me']});return {revision:1,profile:c.body};}});
 render(<DelegationSection canManage/>);await screen.findByText('Analysis');
 await userEvent.click(screen.getByRole('button',{name:'New delegation profile'}));
 await userEvent.type(screen.getByLabelText('Profile ID'),'reader');await userEvent.type(screen.getByLabelText('Profile name'),'Reader');await userEvent.type(screen.getByLabelText('Profile instructions'),'Inspect supplied files');
 await userEvent.type(screen.getByLabelText('Inherited capabilities (one per line)'),'filesystem.read\nmcp.personal.get_me');
 await userEvent.click(screen.getByRole('button',{name:'Review delegation profile'}));
 expect(screen.getByText('Inherited capabilities: filesystem.read, mcp.personal.get_me')).toBeTruthy();
 expect(backend.calls.filter(c=>c.method==='POST')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Confirm delegation profile'}));
 expect(await screen.findByText('Delegation profile saved. No agent permissions were changed.')).toBeTruthy();
 expect(backend.calls.filter(c=>c.method==='POST')).toHaveLength(1);
});
