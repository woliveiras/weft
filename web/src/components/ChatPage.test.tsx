import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
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
