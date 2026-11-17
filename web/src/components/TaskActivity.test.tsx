import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { EventStreamProvider } from '../hooks/useEventStream';
import { apiError, fakeApi } from '../test/fakeApi';
import { TaskActivity } from './TaskActivity';
class EventSourceStub { addEventListener() {} close() {} }
beforeEach(() => vi.stubGlobal('EventSource', EventSourceStub));
afterEach(cleanup);
const execution = {id:'e',graphId:'g',nodeId:'worker',replica:0,status:'failed',summary:'Partial result',error:'Provider unavailable',toolCalls:2,startedAt:'2026-10-16T10:00:00Z'};
const approval = {id:'a',graphId:'g',executionId:'e',title:'Allow operation',status:'pending',kind:'tool',createdAt:'2026-10-16T10:00:00Z'};
function routes() { return {
  'GET /api/v1/graphs/g/status': () => ({graphId:'g',state:'running',desiredState:'running',approvalsPending:1,nodes:[{nodeId:'worker',replicas:[{replica:0,state:'waiting',detail:'Waiting for CPU capacity'}]}]}),
  'GET /api/v1/graphs/g/executions': () => [execution],
  'GET /api/v1/graphs/g/events': () => [{id:'ev',graphId:'g',executionId:'e',type:'execution.failed',data:{error:'failed turn'}},{id:'op',graphId:'g',type:'operator.message',data:{text:'Graph instruction'}}],
  'GET /api/v1/graphs/g/messages': () => [{graphId:'g',message:{id:'m',type:'Linked message',metadata:{execution_id:'e'}}},{graphId:'g',message:{id:'unlinked',type:'Unlinked message'}}],
  'GET /api/v1/approvals': () => [approval],
  'GET /api/v1/reviews': () => [{id:'r',graphId:'other',request:{title:'Other graph private review'}}],
}; }
function view(role='admin', graphId='g') { return <EventStreamProvider token="fixture"><TaskActivity key={graphId} graphId={graphId} role={role} /></EventStreamProvider>; }
it('integrates real client responses, results, wait reasons and explicit execution filtering', async () => {
  fakeApi(routes()); render(view());
  expect(await screen.findByText('Partial result')).toBeTruthy();
  expect(screen.getByText(/Waiting for CPU capacity/)).toBeTruthy();
  expect(screen.getByText('Provider unavailable')).toBeTruthy();
  expect(screen.queryByText('Other graph private review')).toBeNull();
  expect(screen.getByText('Unlinked message')).toBeTruthy();
  await userEvent.selectOptions(screen.getByLabelText('Execution'), 'e');
  expect(screen.getByText('Linked message')).toBeTruthy();
  expect(screen.queryByText('Unlinked message')).toBeNull();
  expect(screen.queryByText(/Graph instruction/)).toBeNull();
  expect(screen.getByRole('link',{name:'Inspect files and diff'}).getAttribute('href')).toBe('#/g/g/files');
});
it('keeps healthy sections usable on dependency failure and retries explicitly', async () => {
  let failed=true;
  fakeApi({...routes(),'GET /api/v1/graphs/g/messages':()=>failed ? apiError(503,'Broker unavailable','unavailable'):[]});
  render(view());
  expect(await screen.findByText('messages: Broker unavailable')).toBeTruthy();
  expect(screen.getByText('Partial result')).toBeTruthy();
  expect(within(screen.getByRole('region',{name:'Task messages'})).queryByText(/No messages/)).toBeNull();
  failed=false; await userEvent.click(screen.getByRole('button',{name:'Refresh activity'}));
  expect(await screen.findByText(/No messages in this recent scope/)).toBeTruthy();
  expect(screen.queryByText('messages: Broker unavailable')).toBeNull();
});
it('handles approval rejection errors and refreshes the persisted decision', async () => {
  let decided=false; let fail=true;
  const backend=fakeApi({...routes(),'GET /api/v1/approvals':()=>[{...approval,status:decided?'rejected':'pending'}],
    'POST /api/v1/approvals/a/reject':()=> { if(fail)return apiError(409,'Already changed; refresh','conflict'); decided=true;return {...approval,status:'rejected'}; }});
  render(view()); await screen.findByText('Allow operation');
  await userEvent.click(screen.getByRole('button',{name:'Reject'}));
  expect(await screen.findByText('Already changed; refresh')).toBeTruthy();
  fail=false; await userEvent.click(screen.getByRole('button',{name:'Reject'}));
  expect(await screen.findByText('rejected')).toBeTruthy();
  expect(backend.find('POST','/api/v1/approvals/a/reject')).toHaveLength(2);
});
it('shows approval state without mutation controls for viewers', async () => {
  const backend=fakeApi(routes());render(view('viewer'));
  await screen.findByText('Allow operation');
  expect(screen.queryByRole('button',{name:'Approve'})).toBeNull();
  expect(screen.queryByRole('button',{name:'Reject'})).toBeNull();
  expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});
it('clears previous graph data immediately and ignores its delayed responses', async () => {
  let release!: (value: unknown)=>void;
  fakeApi({...routes(),'GET /api/v1/graphs/g/executions':()=>new Promise(resolve=>{release=resolve;}),
    'GET /api/v1/graphs/other/status':()=>({state:'stopped'}),'GET /api/v1/graphs/other/executions':()=>[],
    'GET /api/v1/graphs/other/events':()=>[],'GET /api/v1/graphs/other/messages':()=>[]});
  const rendered=render(view());rendered.rerender(view('admin','other'));
  release([execution]);
  expect(await screen.findByText(/No executions recorded yet/)).toBeTruthy();
  expect(screen.queryByText('Partial result')).toBeNull();
});
it.each(['operator','viewer'])('keeps environment decisions admin-only for %s', async role => {
  fakeApi({...routes(),'GET /api/v1/approvals':()=>[{...approval,kind:'environment'}]});
  render(view(role)); await screen.findByText('Allow operation');
  expect(screen.queryByRole('button',{name:'Approve'})).toBeNull();
});
it('allows an administrator to approve an environment through the existing endpoint', async () => {
  let approved=false;
  const backend=fakeApi({...routes(),'GET /api/v1/approvals':()=>[{...approval,kind:'environment',status:approved?'approved':'pending'}],
    'POST /api/v1/approvals/a/approve':()=>{approved=true;return {...approval,status:'approved'};}});
  render(view());await screen.findByText('Allow operation');
  await userEvent.click(screen.getByRole('button',{name:'Approve'}));
  expect(await screen.findByText('approved')).toBeTruthy();
  expect(backend.find('POST','/api/v1/approvals/a/approve')).toHaveLength(1);
});
it.each([[404,'Human reviews are not configured on this installation.'],[403,'Your role cannot access human reviews.']])('explains unavailable reviews (%s) without claiming an empty history', async (status, explanation) => {
  fakeApi({...routes(),'GET /api/v1/reviews':()=>apiError(Number(status),'storage: not found','unavailable')});
  render(view());
  expect(await screen.findByText(`reviews: ${explanation}`)).toBeTruthy();
  expect(screen.getByText('Partial result')).toBeTruthy();
  expect(screen.queryByText('No human reviews for this graph.')).toBeNull();
});
