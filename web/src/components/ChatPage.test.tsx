import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { ChatPage } from './ChatPage';
import { fakeApi } from '../test/fakeApi';

const paths = '/api/v1/chat/sessions';
const id = '8921e80ce56b4b50a83981d63b1a911b';
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it('offers an offline setup path and distinguishes shared local history', async () => {
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'unconfigured', revision:0}),
    [`GET ${paths}`]: () => ({items:[],shared:true}),
  });
  render(<ChatPage archived={false} onOpen={vi.fn()}/>);
  expect(await screen.findByText('Shared local history')).toBeTruthy();
  expect((await screen.findByRole('link',{name:'Open Chat model settings'})).getAttribute('href')).toBe('#/models/chat');
  fireEvent.change(screen.getByRole('textbox',{name:'Message'}),{target:{value:'Follow a task'}});
  expect((screen.getByRole('button',{name:'Send'}) as HTMLButtonElement).disabled).toBe(true);
  expect(backend.find('POST',paths)).toHaveLength(0);
});

it('shows simultaneous running and unread success cues with metadata and keyboard access', async () => {
  const open = vi.fn();
  fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'unconfigured',revision:0}),
    [`GET ${paths}`]: () => ({shared:false,items:[{
      id, title:'Follow important release work', lastActivity:'2026-11-26T08:00:00Z',
      running:1, unread:2, unreadSuccess:1, unreadFailure:1, unreadCancellation:0, decisions:1, unknown:0,
    }]}),
  });
  render(<ChatPage archived={false} onOpen={open}/>);
  const row = await screen.findByRole('listitem');
  expect(within(row).getByRole('img',{name:/running.*successful result unread.*failed or blocked result unread.*decision required/})).toBeTruthy();
  const disclosure = within(row).getByRole('button',{name:'Details for Follow important release work'});
  fireEvent.click(disclosure);
  expect(disclosure.getAttribute('aria-expanded')).toBe('true');
  fireEvent.keyDown(disclosure,{key:'Escape'});
  expect(disclosure.getAttribute('aria-expanded')).toBe('false');
  fireEvent.click(within(row).getByRole('button',{name:/^Follow important release work$/}));
  expect(open).toHaveBeenCalledWith(id,false);
});

it('shows the reviewed definition and sends a scoped decline without publishing', async () => {
  const review = {
    id:'chatop-test', agentId:'release-reader',
    definition:{name:'Release reader', prompt:'Summarize release notes.', model:{provider:'local',name:'model'},
      runtime:{kind:'native'}, isolation:{mode:'process'}, readOnly:true},
    digest:'recorded-digest', expiresAt:'2999-11-26T08:00:00Z', state:'awaiting_confirmation',
  };
  const session = (state:string) => ({
    id, archived:false, messages:[{id:'reply',role:'assistant',text:'Review the definition.',review:{...review,state}}],
  });
  const declinePath = `${paths}/${id}/operations/chatop-test/decline`;
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${paths}`]: () => ({items:[],shared:false}),
    [`GET ${paths}/${id}`]: () => session('awaiting_confirmation'),
    [`POST ${declinePath}`]: () => session('declined'),
  });
  render(<ChatPage id={id} archived={false} canCreateAgent onOpen={vi.fn()}/>);
  const card = await screen.findByRole('region',{name:'Catalog agent Release reader'});
  expect(within(card).getByText('Summarize release notes.')).toBeTruthy();
  expect(within(card).getByText('Read-only workspace',{exact:false})).toBeTruthy();
  fireEvent.click(within(card).getByRole('button',{name:'Decline'}));
  expect(await screen.findByText('Proposal declined.')).toBeTruthy();
  expect(backend.find('POST',declinePath).map(req => req.body)).toEqual([{digest:'recorded-digest'}]);
});

it.each([
  {state:'created', version:1, link:'View workflow', href:'#/g/release/canvas'},
  {state:'unknown', version:undefined, link:'Workflows', href:'#/graphs'},
])('shows the pinned workflow definition and the $state outcome link', async ({state,version,link,href}) => {
  const review = {
    id:'workflow-review', digest:'workflow-digest', expiresAt:'2999-11-26T08:00:00Z',
    workflow:{
      graph:{metadata:{id:'release',name:'Release notes'},spec:{
        nodes:[{id:'worker',agent:{id:'reader',version:1}}],
        task:{nodeId:'worker',resultNodeId:'worker',channelId:'requests',messageType:'work.request'},
      }},
      agentName:'Release reader',runtime:'native',readOnly:true,
    },
  };
  const session = (state:string, resultVersion?:number) => ({
    id, archived:false, messages:[{id:'reply',role:'assistant',text:'Review the workflow.',review:{...review,state,resultVersion}}],
  });
  const confirmPath = `${paths}/${id}/operations/workflow-review/confirm`;
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${paths}`]: () => ({items:[],shared:false}),
    [`GET ${paths}/${id}`]: () => session('awaiting_confirmation'),
    [`POST ${confirmPath}`]: () => session(state,version),
  });
  render(<ChatPage id={id} archived={false} canCreateAgent onOpen={vi.fn()}/>);
  const card = await screen.findByRole('region',{name:'Workflow Release notes'});
  expect(within(card).getByText('Release reader · reader · Version 1')).toBeTruthy();
  expect(within(card).getByText(/saves a stopped workflow/)).toBeTruthy();
  expect(within(card).queryByRole('link',{name:'View workflow'})).toBeNull();
  fireEvent.click(within(card).getByRole('button',{name:'Create workflow'}));
  expect((await within(card).findByRole('link',{name:link})).getAttribute('href')).toBe(href);
  expect(backend.find('POST',confirmPath).map(request => request.body)).toEqual([{digest:'workflow-digest'}]);
});

it('lets an operator submit the reviewed draft and inspect the persisted task', async () => {
  const idOfTask = 'tsk_chatreviewed';
  const review = {
    id:'task-review', digest:'task-digest', expiresAt:'2999-11-26T08:00:00Z',
    taskDraft:{input:{title:'Summarize notes',request:'Read the notes.',outcome:'understand',graphId:'editorial-check',
      graphVersion:2,nodeId:'reader',resultNodeId:'reader',channelId:'requests',messageType:'work.request'}},
  };
  const session = (state:string, resultId?:string) => ({
    id, archived:false, messages:[{id:'reply',role:'assistant',text:'Review the draft.',review:{...review,state,resultId}}],
  });
  const confirmPath = `${paths}/${id}/operations/task-review/confirm`;
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${paths}`]: () => ({items:[],shared:false}),
    [`GET ${paths}/${id}`]: () => session('awaiting_confirmation'),
    [`POST ${confirmPath}`]: () => session('created',idOfTask),
  });
  render(<ChatPage id={id} archived={false} canOperate onOpen={vi.fn()}/>);
  const reviewPanel = await screen.findByRole('region',{name:'Task draft Summarize notes'});
  expect(within(reviewPanel).getByText('editorial-check · Version 2')).toBeTruthy();
  expect(within(reviewPanel).getByText('Read the notes.')).toBeTruthy();
  expect(within(reviewPanel).getByText(/without repository access or execution authorization/)).toBeTruthy();
  fireEvent.click(within(reviewPanel).getByRole('button',{name:'Create task draft'}));
  expect((await within(reviewPanel).findByRole('link',{name:'View task draft'})).getAttribute('href')).toContain(`#/tasks/${idOfTask}`);
  expect(backend.find('POST',confirmPath).map(request => request.body)).toEqual([{digest:'task-digest'}]);
});

it('shows task admission separately from the outcome following confirmation', async () => {
  const taskId = 'tsk_admission';
  const review = {
    id:'start-review', digest:'start-digest', expiresAt:'2999-11-26T08:00:00Z',
    taskStart:{id:taskId,title:'Summarize notes',workflowId:'editorial',workflowVersion:2,taskRevision:1,
      consent:{mode:'ask',revision:0},agentGrants:[{id:'reader',isolation:'process'}]},
  };
  const session = (state:string) => ({
    id, archived:false, messages:[{id:'reply',role:'assistant',text:'Review the start.',review:{...review,state}}],
    watches: state==='accepted' ? [{requestId:'start-review',task:{id:taskId,state:'running'},observedAt:'2026-11-26T10:00:00Z'}] : [],
  });
  const confirmPath = `${paths}/${id}/operations/start-review/confirm`;
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${paths}`]: () => ({items:[],shared:false}),
    [`GET ${paths}/${id}`]: () => session('awaiting_confirmation'),
    [`POST ${confirmPath}`]: () => session('accepted'),
  });
  render(<ChatPage id={id} archived={false} canCreateAgent canOperate onOpen={vi.fn()}/>);
  const card = await screen.findByRole('region',{name:'Start task Summarize notes'});
  expect(within(card).getByText('Ask for approval as required · Default')).toBeTruthy();
  fireEvent.click(within(card).getByRole('button',{name:'Start task'}));
  expect(await within(card).findByText(/Task admission recorded/)).toBeTruthy();
  expect(screen.getByText(/Following task: running/)).toBeTruthy();
  expect(screen.queryByRole('link',{name:'View task result'})).toBeNull();
  expect(backend.find('POST',confirmPath).map(request => request.body)).toEqual([{digest:'start-digest'}]);
});

it('displays the reviewed node and a recorded control while runtime is still draining', async () => {
  const review = {
    id:'node-pause', digest:'bound-control', expiresAt:'2999-11-26T08:00:00Z',
    workflowControl:{action:'pause',scope:{workflowId:'publication',nodeId:'reviewer',version:2,runId:'run-first',desiredState:'running'}},
  };
  const session = (state:string) => ({
    id, archived:false, messages:[{id:'reply',role:'assistant',text:'Review this control.',review:{...review,state,
      controlResult:state==='accepted' ? {runId:'run-first',desired:'paused',observed:'running',settled:false} : undefined}}],
  });
  const path = `${paths}/${id}/operations/node-pause/confirm`;
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${paths}`]: () => ({items:[],shared:false}),
    [`GET ${paths}/${id}`]: () => session('awaiting_confirmation'),
    [`POST ${path}`]: () => session('accepted'),
  });
  render(<ChatPage id={id} archived={false} canOperate onOpen={vi.fn()}/>);
  const card = await screen.findByRole('region',{name:'Pause new turns publication / reviewer'});
  expect(within(card).getByText('publication / reviewer · Version 2')).toBeTruthy();
  expect(within(card).getByText(/while active turns continue/)).toBeTruthy();
  fireEvent.click(within(card).getByRole('button',{name:'Pause new turns'}));
  expect(await within(card).findByText(/Control recorded. Waiting/)).toBeTruthy();
  expect(within(card).getByText('paused')).toBeTruthy();
  expect(within(card).getByText('Observed runtime state')).toBeTruthy();
  expect(within(card).queryByText('The runtime reached the requested state.')).toBeNull();
  expect(backend.find('POST',path).map(request => request.body)).toEqual([{digest:'bound-control'}]);
});

it('links the original failed task and the separate retry draft', async () => {
  const review = {
    id:'retry-action', digest:'retry-revision', expiresAt:'2999-11-26T08:00:00Z',
    taskRetry:{id:'tsk_original',title:'Rebuild index',revision:6,workflowId:'search',workflowVersion:2,repositoryCount:0},
  };
  const session = (state:string, resultId?:string) => ({
    id, archived:false, messages:[{id:'reply',role:'assistant',text:'Review the failed attempt.',review:{...review,state,resultId}}],
  });
  const path = `${paths}/${id}/operations/retry-action/confirm`;
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${paths}`]: () => ({items:[],shared:false}),
    [`GET ${paths}/${id}`]: () => session('awaiting_confirmation'),
    [`POST ${path}`]: () => session('created','tsk_linked'),
  });
  render(<ChatPage id={id} archived={false} canOperate onOpen={vi.fn()}/>);
  const card = await screen.findByRole('region',{name:'Retry task Rebuild index'});
  expect(within(card).getByRole('link',{name:'View failed task'}).getAttribute('href')).toContain('#/tasks/tsk_original');
  expect(within(card).getByText(/requires its own review and authorization/)).toBeTruthy();
  expect(within(card).queryByRole('link',{name:'View retry'})).toBeNull();
  fireEvent.click(within(card).getByRole('button',{name:'Prepare retry draft'}));
  expect((await within(card).findByRole('link',{name:'View retry'})).getAttribute('href')).toContain('#/tasks/tsk_linked');
  expect(backend.find('POST',path).map(request => request.body)).toEqual([{digest:'retry-revision'}]);
});

it('shows archived novelty from Recents and navigates across bounded pages', async () => {
  const open = vi.fn();
  const item = {
    id, title:'Most recent', lastActivity:'2026-11-26T08:00:00Z',
    running:0, unread:0, unreadSuccess:0, unreadFailure:0, unreadCancellation:0, decisions:0, unknown:0,
  };
  let requests = 0;
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'unconfigured',revision:0}),
    [`GET ${paths}`]: () => ++requests === 1
      ? ({shared:false,items:[item],archivedUnread:3,next:'123~'+id})
      : ({shared:false,items:[{...item,title:'Older conversation'}],archivedUnread:3}),
  });
  render(<ChatPage archived={false} onOpen={open}/>);
  expect(await screen.findByRole('button',{name:'Most recent'})).toBeTruthy();
  fireEvent.click(screen.getByRole('button',{name:'Archived · 3 unread'}));
  expect(open).toHaveBeenCalledWith(undefined,true);
  fireEvent.click(screen.getByRole('button',{name:'Older conversations'}));
  expect(await screen.findByRole('button',{name:'Older conversation'})).toBeTruthy();
  expect(screen.queryByRole('button',{name:'Most recent'})).toBeNull();
  expect(backend.fetch.mock.calls.some(([url]) => new URL(String(url),'http://localhost').searchParams.get('after') === '123~'+id)).toBe(true);
});

it('offers title-only retry after an untitled conversation', async () => {
  const original = {id,title:'Untitled chat',archived:false,messages:[{id:'request',role:'user',text:'Follow release work'}]};
  const path = `${paths}/${id}/title/retry`;
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${paths}`]: () => ({items:[],shared:false}),
    [`GET ${paths}/${id}`]: () => original,
    [`POST ${path}`]: () => ({...original,title:'Release work',titleRetryState:'completed'}),
  });
  render(<ChatPage id={id} archived={false} onOpen={vi.fn()}/>);
  expect(await screen.findByText('A title could not be generated.')).toBeTruthy();
  fireEvent.click(await screen.findByRole('button',{name:'Retry title'}));
  expect(await screen.findByRole('heading',{name:'Release work'})).toBeTruthy();
  expect(screen.getByText('Follow release work')).toBeTruthy();
  expect(backend.calls.filter(request => request.method === 'POST').map(request => request.path)).toEqual([path]);
});

it.each([true,false])('keeps a late response in its original chat (success %s)', async success => {
  const open = vi.fn();
  let release!: (value: unknown) => void;
  const first = new Promise(resolve => { release = resolve; });
  fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'configured',revision:1}),
    [`GET ${paths}`]: () => ({items:[],shared:true}),
    [`POST ${paths}`]: () => first,
  });
  render(<ChatPage archived={false} onOpen={open}/>);
  await screen.findByText('Shared local history');
  const input = screen.getByRole('textbox',{name:'Message'});
  fireEvent.change(input,{target:{value:'Follow the earlier request'}});
  fireEvent.click(screen.getByRole('button',{name:'Send'}));
  await screen.findByRole('button',{name:'Sending…'});
  fireEvent.click(screen.getByRole('button',{name:'New chat'}));
  fireEvent.change(input,{target:{value:'A different request'}});
  expect((screen.getByRole('button',{name:'Send'}) as HTMLButtonElement).disabled).toBe(false);
  await act(async () => release(success ? {id, messages:[]} : new Response('Failure',{status:503})));
  expect((input as HTMLTextAreaElement).value).toBe('A different request');
  expect(open.mock.calls).toEqual([[undefined,false]]);
  expect(screen.queryByRole('alert')).toBeNull();
});

it('allows an explicit review decision while model inference is unavailable', async () => {
  const conversation = {id,title:'Saved review',messages:[]};
  const backend = fakeApi({
    'GET /api/v1/chat/settings': () => ({status:'unconfigured',revision:1}),
    [`GET ${paths}`]: () => ({items:[],shared:true}),
    [`GET ${paths}/${id}`]: () => conversation,
    [`POST ${paths}/${id}/messages`]: () => conversation,
  });
  render(<ChatPage id={id} archived={false} onOpen={vi.fn()}/>);
  await screen.findByRole('heading',{name:'Saved review'});
  fireEvent.change(screen.getByRole('textbox',{name:'Message'}),{target:{value:'Recuse esta proposta.'}});
  await act(async () => fireEvent.click(screen.getByRole('button',{name:'Send'})));
  expect(backend.find('POST',`${paths}/${id}/messages`)).toHaveLength(1);
});

it('opens the selected child execution and distinguishes a request from terminal state', async () => {
  fakeApi({
    'GET /api/v1/chat/settings':()=>({status:'unconfigured',revision:0}),
    [`GET ${paths}`]:()=>({items:[],shared:true}),
    [`GET ${paths}/${id}`]:()=>({id,title:'Execution status',messages:[{id:'a1',role:'assistant',
      text:'Current execution state',execution:{id:'child',root:'parent',graph:'review',state:'running',nodes:2,active:1,cancellationRequested:true}}]}),
  });
  render(<ChatPage archived={false} id={id} onOpen={vi.fn()}/>);
  const summary=await screen.findByRole('region',{name:'Execution evidence'});
  expect(within(summary).getByText(/child · running/)).toBeTruthy();
  expect(within(summary).getByText('Cancellation requested')).toBeTruthy();
  expect(within(summary).getByRole('link',{name:'View execution'}).getAttribute('href')).toContain('#/g/review/runs?execution=child');
});

it('announces an unread reply while preserving the running indicator', async () => {
  fakeApi({
    'GET /api/v1/chat/settings':()=>({status:'configured',revision:1}),
    [`GET ${paths}`]:()=>({items:[{id,title:'A conversation',running:1,unread:1,unreadSuccess:0,unreadFailure:0,unreadCancellation:0,decisions:0,unknown:0}]}),
  });
  render(<ChatPage archived={false} onOpen={vi.fn()}/>);
  expect(await screen.findByRole('img',{name:/1 unread repl.*1 running/})).toBeTruthy();
});
