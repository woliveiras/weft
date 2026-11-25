import {cleanup, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, expect, it, vi} from 'vitest';
import type {ReactNode} from 'react';
import {App} from './App';
import {fakeApi, apiError} from './test/fakeApi';
vi.mock('./hooks/useEventStream',()=>({EventStreamProvider:({children}:{children:ReactNode})=>children,useEventStream:()=>({connected:false})}));
afterEach(()=>{cleanup();localStorage.clear();vi.restoreAllMocks();vi.unstubAllGlobals();});

it('keeps graph load failures recoverable through grouped navigation without mutations',async()=>{
 localStorage.setItem('weftToken','test-token');location.hash='#/graphs';
 const backend=fakeApi({
  'GET /api/v1/info':()=>({identity:{name:'viewer',role:'viewer'}}),
  'GET /api/v1/graphs':()=>apiError(503,'Graphs unavailable','unavailable'),
  'GET /api/v1/approvals':()=>[],
 });
 render(<App/>);
 await screen.findByText('Graphs unavailable');
 await userEvent.click(screen.getByRole('button',{name:'Approvals'}));
 await waitFor(()=>expect(location.hash).toBe('#/approvals'));
 await waitFor(()=>expect(screen.getByRole('button',{name:'Approvals'}).getAttribute('aria-current')).toBe('page'));
 await userEvent.click(screen.getByRole('button',{name:'Workflows'}));
 await screen.findByText('Graphs unavailable');
 expect(screen.getByRole('button',{name:'Workflows'}).getAttribute('aria-current')).toBe('page');
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
 expect(backend.calls.some(c=>c.path.includes('/tasks')||c.path.includes('/projects'))).toBe(false);
});

it('opens a task-owned run directly even when workflow inventory excludes execution copies', async () => {
 localStorage.setItem('weftToken','test-token');location.hash='#/g/task-run-example/runs?execution=exe_example&section=tools';
 const backend = fakeApi({
  'GET /api/v1/info':()=>({identity:{name:'admin',role:'admin'}}),
  'GET /api/v1/graphs':()=>[],
  'GET /api/v1/approvals':()=>[],
  'GET /api/v1/tasks/tsk_example':()=>({id:'tsk_example',title:'Demo task',executionGraphId:'task-run-example'}),
  'GET /api/v1/executions/exe_example':()=>({id:'exe_example',graphId:'task-run-example',nodeId:'implementer',graphVersion:2,status:'succeeded',startedAt:'2026-11-23T12:35:11Z'}),
  'GET /api/v1/graphs/task-run-example/events':()=>[],
 });
 render(<App/>);
 expect(await screen.findByRole('region',{name:'Run tool calls'})).toBeTruthy();
 expect(await screen.findByText('No linked events in this recent window.')).toBeTruthy();
 expect(screen.queryByText(/Graph .*not found/)).toBeNull();
 expect(screen.getByRole('button',{name:'Tasks'}).getAttribute('aria-current')).toBe('page');
 expect(screen.queryByRole('button',{name:'Save graph'})).toBeNull();
 expect(backend.calls.every(c=>c.method==='GET')).toBe(true);
});

it('retains the reviewed new task across its saved-task route transition without model or execution writes',async()=>{
 localStorage.setItem('weftToken','test-token');location.hash='#/tasks/new?workflow=echo';
 let draft:any;
 const graph={graph:{metadata:{id:'echo',name:'Echo',version:1},spec:{nodes:[{id:'worker',name:'Worker',runtime:{kind:'echo'}}],channels:[]}}};
 const backend=fakeApi({
  'GET /api/v1/info':()=>({identity:{name:'admin',role:'admin'}}),'GET /api/v1/graphs':()=>[graph],
  'GET /api/v1/approvals':()=>[],'GET /api/v1/providers':()=>[],'GET /api/v1/projects':()=>[],
  'GET /api/v1/scm/connections':()=>[],
  'POST /api/v1/tasks':call=>(draft={...call.body as object,id:'tsk_navigation',revision:1,status:'draft'}),
  'GET /api/v1/tasks/tsk_navigation':()=>draft,
  'GET /api/v1/tasks/tsk_navigation/review':()=>({task:draft,blockers:['Install the test environment'],canStart:false,validation:{},model:{},grants:[]}),
 });
 render(<App/>);
 await screen.findByLabelText('Request');
 await waitFor(()=>expect(screen.getByLabelText('Entry agent')).toHaveProperty('value','worker'));
 await userEvent.type(screen.getByLabelText('Request'),'Inspect the source');
 await userEvent.click(screen.getByRole('button',{name:'Review task'}));
 await waitFor(()=>expect(location.hash).toBe('#/tasks/tsk_navigation?review=1'));
 await screen.findByRole('heading',{name:'Task draft'});
 await waitFor(()=>expect(screen.getAllByText('Install the test environment').length).toBeGreaterThan(0));
 expect(screen.getByLabelText('Request')).toHaveProperty('value','Inspect the source');
 expect(backend.calls.filter(c=>c.method!=='GET').map(c=>c.path)).toEqual(['/api/v1/tasks']);
});
