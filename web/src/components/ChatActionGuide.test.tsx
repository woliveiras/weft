import {cleanup, fireEvent, render, screen, within} from '@testing-library/react';
import {afterEach, expect, it, vi} from 'vitest';
import {ChatPage} from './ChatPage';
import {apiError, fakeApi} from '../test/fakeApi';
import {guideAction, guidedMessage} from './chatGuide';

afterEach(()=>{cleanup();vi.unstubAllGlobals();});
const sessions = '/api/v1/chat/sessions';
const candidates = '/api/v1/chat/candidates';
const ready = {
  'GET /api/v1/chat/settings':()=>({status:'configured'}),
  ['GET '+sessions]:()=>({items:[]}),
};
const click = (name:string) => fireEvent.click(screen.getByRole('button',{name}));

it('shows the ordered categories and restores keyboard focus when navigating back',async()=>{
  fakeApi(ready);
  render(<ChatPage archived={false} onOpen={vi.fn()}/>);
  const guide = within(await screen.findByRole('region',{name:'Chat actions'}));
  const labels = () => guide.getAllByRole('button').map(el=>el.textContent);
  expect(labels()).toEqual(['Tasks','Agents','Workflows','Mais']);
  click('Mais');
  expect(labels().slice(4)).toEqual(['Projects','Connections','Models','Skills & instructions']);
  click('Models');
  expect(guide.queryByRole('link',{name:'Open model settings'})).toBeNull();
  click('View configured models');
  expect(screen.getByRole('button',{name:'View configured models'})).toHaveProperty('disabled',false);
  click('Back');
  expect(document.activeElement?.textContent).toBe('View configured models');
  click('Back');
  expect(document.activeElement?.textContent).toBe('Models');
  fireEvent.keyDown(document.activeElement!,{key:'Escape'});
  expect(document.activeElement?.textContent).toBe('Mais');
  expect(labels()).toEqual(['Tasks','Agents','Workflows','Mais']);
});

it('preserves notes through failed search, partial results and a stale target',async()=>{
  let attempt=0;
  const target={id:'tsk_second',label:'Release · tsk_second · draft', task:{title:'Release',status:'blocked',projectId:'weft'}};
  const backend=fakeApi({...ready,['POST '+sessions]:()=>apiError(400,'This selection is unavailable. Change or clear it before sending.','invalid'),['GET '+candidates]:()=>{
    attempt++;
    if(attempt===1)return apiError(503,'temporarily offline','unavailable');
    if(attempt===2)return {items:[target],partial:true};
    return {items:[],partial:false};
  }});
  render(<ChatPage archived={false} onOpen={vi.fn()}/>);
  await screen.findByRole('button',{name:'Tasks'});
  const field=screen.getByRole('textbox',{name:'Message'});
  fireEvent.change(field,{target:{value:'my notes'}});
  click('Tasks');click('Follow a task');
  await screen.findByText('temporarily offline');
  click('Retry search');
  await screen.findByText('Showing 1 task. Search by name or ID to narrow the list.');
  click('Release Blocked Project weft · …second');
  expect(screen.getByText('tsk_second')).toBeTruthy();
  expect(screen.queryByRole('searchbox')).toBeNull();
  expect(backend.find('POST',sessions)).toHaveLength(0);
  click('Send and follow');
  await screen.findByText('This selection is unavailable. Change or clear it before sending.');
  expect(backend.find('POST',sessions)).toHaveLength(1);
  click('Change task');
  await screen.findByText('No matches. Try another name or ID.');
  expect(field).toHaveProperty('value','my notes');
});

it('distinguishes equal titles and ID suffixes and keeps lookup separate from follow',async()=>{
  const items=['tsk_a123456','tsk_b123456'].map((id,i)=>({id,label:id,task:{title:'Release review',status:i?'failed':'blocked'}}));
  fakeApi({...ready,['GET '+candidates]:()=>({items,partial:false})});
  render(<ChatPage archived={false} onOpen={vi.fn()}/>);
  await screen.findByRole('button',{name:'Tasks'});
  click('Tasks');click('Find a task');
  await screen.findByRole('button',{name:'Release review Blocked …a123456'});
  click('Release review Failed …b123456');
  expect(screen.getByText('tsk_b123456')).toBeTruthy();
  expect(screen.getByRole('button',{name:'View task'})).toHaveProperty('disabled',false);
  expect(screen.queryByRole('button',{name:'Send and follow'})).toBeNull();
});

it('uses a complete eligible choice and keeps an incomplete branch out of the submitted text',()=>{
  const follow=guideAction({category:'Tasks',action:'Follow a task'},false);
  const target={id:'tsk_a',label:'Duplicate · tsk_a · draft'};
  expect(guidedMessage(follow,target,'')).toBe('/follow tsk_a');
  expect(guidedMessage(follow,target,'Watch this one')).toBe('/follow tsk_a\nWatch this one');
  expect(guidedMessage(follow,undefined,'/inspect tasks')).toBe('/inspect tasks');
  const creation={category:'Agents',action:'Create a catalog agent'} as const;
  expect(guideAction(creation,false)).toBeUndefined();
  expect(guidedMessage(guideAction(creation,true),undefined,'')).toBe('');
  expect(guidedMessage(guideAction(creation,true),undefined,'Reader checks notes')).toContain('Reader checks notes');
});

it('restores the guided draft after a lost acknowledgement and retries with the same key',async()=>{
  const target={id:'tsk_a',label:'Release · tsk_a · draft'};
  const open=vi.fn();
  const backend=fakeApi({...ready,['GET '+candidates]:()=>({items:[target],partial:false}),
    ['POST '+sessions]:()=>apiError(503,'Reply lost','network')});
  render(<ChatPage archived={false} onOpen={open}/>);
  await screen.findByRole('button',{name:'Tasks'});
  click('Tasks');click('Follow a task');
  fireEvent.click(await screen.findByRole('button',{name:target.label}));
  click('Add details');
  const field=screen.getByRole('textbox',{name:'Message'});
  expect(document.activeElement).toBe(field);
  fireEvent.change(field,{target:{value:'Notify me'}});
  fireEvent.keyDown(field,{key:'Enter',ctrlKey:true});
  await screen.findByText('Reply lost');
  expect(field).toHaveProperty('value','Notify me');
  fireEvent.keyDown(field,{key:'Enter',metaKey:true});
  await screen.findByText('Reply lost');
  const requests=backend.find('POST',sessions).map(r=>r.body);
  expect(requests).toHaveLength(2);
  expect(requests[1]).toEqual(requests[0]);
  expect(requests[0]).toMatchObject({text:'/follow tsk_a\nNotify me'});
});

it('keeps guided actions out of the model recovery and archived conversation states',async()=>{
  const id='8921e80ce56b4b50a83981d63b1a911b';
  fakeApi({...ready,'GET /api/v1/chat/settings':()=>({status:'unconfigured'}),['GET '+sessions+'/'+id]:()=>({id,title:'Old conversation',archived:true,messages:[]})});
  const page=render(<ChatPage archived={false} onOpen={vi.fn()}/>);
  await screen.findByText('No Chat model selected.');
  expect(screen.queryByRole('region',{name:'Chat actions'})).toBeNull();
  page.rerender(<ChatPage archived id={id} onOpen={vi.fn()}/>);
  await screen.findByRole('button',{name:'Restore conversation'});
  expect(screen.queryByRole('region',{name:'Chat actions'})).toBeNull();
  expect(screen.queryByRole('button',{name:'Send'})).toBeNull();
});

it.each([
  ['Agents','Find a catalog agent','agent','Version 2'],
  ['Workflows','Find a workflow','workflow','Version 2'],
  ['Projects','Find a project','project','Revision 7'],
  ['Skills & instructions','Find a skill','skill','Revision 7'],
])('reviews %s using the resource name, badge and exact ID',async(category,action,noun,badge)=>{
  const catalog = {name:'Release · review', ...(badge.startsWith('Version') ? {version:2} : {revision:7})};
  const items = ['first','second'].map(id=>({id,label:'Legacy '+id,catalog}));
  fakeApi({...ready,['GET '+candidates]:()=>({items,partial:true})});
  render(<ChatPage archived={false} onOpen={vi.fn()}/>);
  await screen.findByRole('button',{name:'Tasks'});
  if(category==='Projects' || category==='Skills & instructions') click('Mais');
  click(category);click(action);
  await screen.findByText('Showing 2 '+noun+'s. Search by name or ID to narrow the list.');
  const name = 'Release · review '+badge+' second';
  click(name);
  expect(screen.getByText('second')).toBeTruthy();
  expect(screen.queryByRole('searchbox')).toBeNull();
  click('Change '+noun);
  expect(await screen.findByRole('button',{name})).toBe(document.activeElement);
});

it.each([
  ['Tasks','See running tasks','Show running tasks','List the tasks currently running.'],
  ['Workflows','See active workflows','Show active workflows','Which workflows are active?'],
  ['Connections','Inspect MCP connections','Inspect MCP connections','/inspect mcp'],
  ['Connections','Inspect source control connections','Inspect source control connections','/inspect scm'],
  ['Connections','Inspect issue connections','Inspect issue connections','/inspect issue-connections'],
  ['Models','View configured models','View configured models','/inspect models'],
])('runs the prepared %s action %s without typing',async(category,action,submit,expected)=>{
  const backend=fakeApi({...ready,['POST '+sessions]:()=>apiError(503,'Reply lost','network')});
  render(<ChatPage archived={false} onOpen={vi.fn()}/>);
  await screen.findByRole('button',{name:'Tasks'});
  if(category==='Connections' || category==='Models') click('Mais');
  click(category);click(action);
  expect(screen.queryByRole('textbox',{name:'Message'})).toBeNull();
  expect(screen.getByRole('button',{name:'Add details'})).toBeTruthy();
  expect(backend.find('POST',sessions)).toHaveLength(0);
  click(submit);
  await screen.findByText('Reply lost');
  expect(backend.find('POST',sessions).map(call=>call.body)).toEqual([expect.objectContaining({text:expected})]);
});
