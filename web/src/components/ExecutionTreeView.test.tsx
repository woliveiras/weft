import {cleanup,render,screen,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {fakeApi,apiError} from '../test/fakeApi';
import {ExecutionTreeView} from './ExecutionTreeView';
afterEach(cleanup);
const root={id:'r',graphId:'g',runId:'run',nodeId:'parent',status:'running',startedAt:'2026-10-18T00:00:00Z'};
const child={...root,id:'c',parentExecutionId:'r',delegationTemplate:'analysis'};
const tree={rootExecutionId:'r',selectedExecutionId:'c',nodes:[{execution:root},{execution:child}]};
it('requires review and sends a child request through the real client without changing permissions',async()=>{
 const backend=fakeApi({'GET /api/v1/executions/c/tree':()=>tree,'POST /api/v1/executions/c/cancel':()=>({alreadyTerminal:false,request:{executionId:'c'}})});
 render(<ExecutionTreeView graphId="g" executionId="c" role="operator"/>);
 await userEvent.click(await screen.findByRole('button',{name:'Stop child'}));
 expect(backend.calls.filter(c=>c.method==='POST')).toHaveLength(0);
 const review=screen.getByRole('region',{name:'Confirm interruption'});expect(within(review).getByText(/Its parent receives an error/)).toBeTruthy();
 await userEvent.click(within(review).getByRole('button',{name:'Confirm interruption'}));
 expect(await screen.findByText(/Interruption requested. Waiting/)).toBeTruthy();expect(backend.find('POST','/api/v1/executions/c/cancel')[0].body).toEqual({scope:'child',confirm:true});
});
it('keeps viewers read-only and fails closed for inconsistent trees',async()=>{
 fakeApi({'GET /api/v1/executions/c/tree':()=>tree});const view=render(<ExecutionTreeView graphId="g" executionId="c" role="viewer"/>);
 await screen.findByRole('link',{name:'parent · c'});expect(screen.queryByRole('button',{name:'Stop child'})).toBeNull();
 fakeApi({'GET /api/v1/executions/c/tree':()=>({...tree,nodes:[{execution:{...root,graphId:'other'}}]})});view.rerender(<ExecutionTreeView graphId="g" executionId="c" role="admin"/>);await userEvent.click(screen.getByRole('button',{name:'Refresh tree'}));
 expect(await screen.findByText('Inconsistent execution tree.')).toBeTruthy();expect(screen.queryByRole('button',{name:'Stop entire tree'})).toBeNull();
});
it('allows abandoning review and retries a failed tree stop without pretending success',async()=>{
 let fail=true;const backend=fakeApi({'GET /api/v1/executions/c/tree':()=>tree,'POST /api/v1/executions/r/cancel':()=>fail?apiError(503,'Store unavailable','unavailable'):({alreadyTerminal:true})});
 render(<ExecutionTreeView graphId="g" executionId="c" role="admin"/>);await userEvent.click(await screen.findByRole('button',{name:'Stop entire tree'}));await userEvent.click(screen.getByRole('button',{name:'Keep running'}));expect(backend.calls.filter(c=>c.method==='POST')).toHaveLength(0);
 await userEvent.click(screen.getByRole('button',{name:'Stop entire tree'}));await userEvent.click(screen.getByRole('button',{name:'Confirm interruption'}));expect(await screen.findByText('Store unavailable')).toBeTruthy();fail=false;await userEvent.click(screen.getByRole('button',{name:'Confirm interruption'}));expect(await screen.findByText('The execution has already finished.')).toBeTruthy();expect(backend.find('POST','/api/v1/executions/r/cancel')[0].body).toEqual({scope:'tree',confirm:true});
});

it('shows charged, reserved and estimated tree totals without summing inclusive execution usage',async()=>{
 fakeApi({'GET /api/v1/executions/c/tree':()=>({...tree,budget:{limit:1000,charged:200,reserved:300,estimated:50}})});
 render(<ExecutionTreeView graphId="g" executionId="c" role="viewer"/>);
 const budget=await screen.findByRole('region',{name:'Tree token budget'});
 expect(budget.textContent).toContain('Limit: 1000 · Charged: 200 · Reserved: 300 · Estimated charges: 50');
 expect(budget.textContent).toContain('Parent and child calls are counted once');
});
