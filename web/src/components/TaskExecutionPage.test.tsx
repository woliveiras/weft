import {cleanup, render, screen, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, expect, it, vi} from 'vitest';
import {fakeApi, apiError} from '../test/fakeApi';
import type {TaskDraft} from '../api/tasks';
import {TaskExecutionPage, isTaskExecutionGraphId, ownsTaskExecution, taskOwnerId} from './TaskExecutionPage';

afterEach(() => {cleanup();vi.unstubAllGlobals();});
const task = {id:'tsk_example', title:'Inspect the demo', executionGraphId:'task-run-example'};
const execution = {id:'exe_example', graphId:'task-run-example', nodeId:'implementer', graphVersion:2, status:'succeeded', summary:'Greeting implemented', toolCalls:1, startedAt:'2026-11-23T12:35:11Z'};
const tool = {id:'tool', graphId:'task-run-example', nodeId:'implementer', executionId:'exe_example', type:'tool.completed', time:'2026-11-23T12:35:12Z', seq:1, data:{tool:'shell.exec', outcome:'ok'}};
const workspaceStatus = {graphId:'task-run-example',state:'stopped',desiredState:'stopped',nodes:[],channels:[]};

const longGraphId = `task-run-h-${'a'.repeat(52)}`;
const longTask = {...task, id:`tsk_${'b'.repeat(64)}`, executionGraphId:longGraphId};

it('does not reverse hashed execution names or accept an unbound owner', () => {
  expect(isTaskExecutionGraphId(longGraphId)).toBe(true);
  expect(isTaskExecutionGraphId('task-run-example')).toBe(true);
  expect(isTaskExecutionGraphId('task-run-h-invalid')).toBe(false);
  expect(isTaskExecutionGraphId('workflow')).toBe(false);
  expect(taskOwnerId(longGraphId)).toBeUndefined();
  expect(taskOwnerId('task-run-example')).toBe('tsk_example');
  expect(ownsTaskExecution(longTask as TaskDraft,longGraphId)).toBe(false);
  expect(ownsTaskExecution(longTask as TaskDraft,longGraphId,longTask.id)).toBe(true);
  expect(ownsTaskExecution({...longTask,executionGraphId:'other'} as TaskDraft,longGraphId,longTask.id)).toBe(false);
  expect(ownsTaskExecution(longTask as TaskDraft,longGraphId,'tsk_other')).toBe(false);
});

it('resolves long task execution owners through graph labels before loading evidence', async () => {
  const backend = fakeApi({
    [`GET /api/v1/graphs/${longGraphId}`]:() => ({graph:{metadata:{id:longGraphId,labels:{'weft.task':longTask.id}}}}),
    [`GET /api/v1/tasks/${longTask.id}`]:() => longTask,
    [`GET /api/v1/graphs/${longGraphId}/executions`]:() => [],
  });
  render(<TaskExecutionPage graphId={longGraphId} tab="runs"/>);
  expect(await screen.findByText('No executions recorded yet.')).toBeTruthy();
  expect(screen.getByRole('link',{name:'Back to task'}).getAttribute('href')).toBe(`#/tasks/${longTask.id}`);
  expect(backend.calls.map(c=>c.path)).toEqual([`/api/v1/graphs/${longGraphId}`,`/api/v1/tasks/${longTask.id}`,`/api/v1/graphs/${longGraphId}/executions`]);
});

it.each([
  {name:'foreign graph',metadata:{id:'other',labels:{'weft.task':longTask.id}},error:'The workflow response does not match this execution copy.'},
  {name:'missing owner',metadata:{id:longGraphId},error:'This execution copy does not belong to the selected task.'},
  {name:'invalid owner',metadata:{id:longGraphId,labels:{'weft.task':'../other'}},error:'This execution copy does not belong to the selected task.'},
])('rejects $name without fetching a task or its evidence', async ({metadata,error}) => {
  const backend = fakeApi({[`GET /api/v1/graphs/${longGraphId}`]:()=>({graph:{metadata}})});
  render(<TaskExecutionPage graphId={longGraphId} tab="runs"/>);
  expect(await screen.findByText(error)).toBeTruthy();
  expect(backend.calls).toHaveLength(1);
});

it.each([
  {...longTask,id:'tsk_other'},
  {...longTask,executionGraphId:'task-run-other'},
])('rejects mismatched long task ownership before loading evidence', async returnedTask => {
  const backend = fakeApi({
    [`GET /api/v1/graphs/${longGraphId}`]:()=>({graph:{metadata:{id:longGraphId,labels:{'weft.task':longTask.id}}}}),
    [`GET /api/v1/tasks/${longTask.id}`]:()=>returnedTask,
  });
  render(<TaskExecutionPage graphId={longGraphId} tab="runs"/>);
  expect(await screen.findByText('This execution copy does not belong to the selected task.')).toBeTruthy();
  expect(backend.calls).toHaveLength(2);
});

it('recovers failed long owner discovery without loading evidence early', async () => {
  let failed = true;
  const backend = fakeApi({
    [`GET /api/v1/graphs/${longGraphId}`]:()=>failed?apiError(503,'Owner lookup unavailable','unavailable'):{graph:{metadata:{id:longGraphId,labels:{'weft.task':longTask.id}}}},
    [`GET /api/v1/tasks/${longTask.id}`]:()=>longTask,
    [`GET /api/v1/graphs/${longGraphId}/executions`]:()=>[],
  });
  render(<TaskExecutionPage graphId={longGraphId} tab="runs"/>);
  expect(await screen.findByText('Owner lookup unavailable')).toBeTruthy();
  expect(backend.calls).toHaveLength(1);
  failed=false;
  await userEvent.click(screen.getByRole('button',{name:'Retry task execution'}));
  expect(await screen.findByText('No executions recorded yet.')).toBeTruthy();
});

it('opens retained task execution evidence without loading an editable workflow', async () => {
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => task,
    'GET /api/v1/executions/exe_example':() => execution,
    'GET /api/v1/graphs/task-run-example/events':() => [tool],
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="runs" executionId="exe_example" section="tools"/>);
  expect(await screen.findByRole('region', {name:'Run tool calls'})).toBeTruthy();
  expect(await screen.findByText('tool.completed', {exact:true})).toBeTruthy();
  expect(screen.getByRole('link', {name:'Back to task'}).getAttribute('href')).toBe('#/tasks/tsk_example');
  expect(backend.calls.every(c => c.method === 'GET')).toBe(true);
  expect(backend.calls.some(c => c.path === '/api/v1/graphs/task-run-example')).toBe(false);
  for (const name of ['Save graph','Run all','Start','Delete','Stop entire tree']) expect(screen.queryByRole('button', {name})).toBeNull();
});

it('does not load evidence until task ownership matches the copy', async () => {
  const backend = fakeApi({'GET /api/v1/tasks/tsk_example':() => ({...task, executionGraphId:'task-run-other'})});
  render(<TaskExecutionPage graphId="task-run-example" tab="runs" executionId="exe_example"/>);
  expect(await screen.findByText('This execution copy does not belong to the selected task.')).toBeTruthy();
  expect(backend.calls).toHaveLength(1);
  expect(screen.queryByRole('region', {name:'Run detail'})).toBeNull();
});

it('retains the run graph fence for an execution belonging elsewhere', async () => {
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => task,
    'GET /api/v1/executions/exe_example':() => ({...execution, graphId:'other', summary:'Unrelated result'}),
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="runs" executionId="exe_example"/>);
  expect(await screen.findByText('This execution does not belong to the selected graph.')).toBeTruthy();
  expect(screen.queryByText('Unrelated result')).toBeNull();
  expect(backend.calls).toHaveLength(2);
});

it('recovers task lookup failures without calling execution or graph endpoints prematurely', async () => {
  let failed = true;
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => failed ? apiError(503,'Task storage unavailable','unavailable') : task,
    'GET /api/v1/graphs/task-run-example/executions':() => [],
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="runs"/>);
  expect(await screen.findByText('Task storage unavailable')).toBeTruthy();
  expect(screen.queryByText('No executions recorded yet.')).toBeNull();
  expect(backend.calls).toHaveLength(1);
  failed = false;
  await userEvent.click(screen.getByRole('button', {name:'Retry task execution'}));
  expect(await screen.findByText('No executions recorded yet.')).toBeTruthy();
  expect(backend.calls.every(c => c.method === 'GET')).toBe(true);
});

it('shows missing task errors and exposes no workflow editor for unsupported task-copy tabs', async () => {
  fakeApi({'GET /api/v1/tasks/tsk_example':() => apiError(404,'Task not found','not_found')});
  render(<TaskExecutionPage graphId="task-run-example" tab="runs"/>);
  expect(await screen.findByText('Task not found')).toBeTruthy();
  cleanup();
  const backend = fakeApi({'GET /api/v1/tasks/tsk_example':() => task});
  render(<TaskExecutionPage graphId="task-run-example" tab="agent"/>);
  expect(await screen.findByText(/This task-owned copy has no workflow editor/)).toBeTruthy();
  expect(screen.getByRole('link', {name:'View task settings and access'}).getAttribute('href')).toBe('#/tasks/tsk_example?tab=settings');
  expect(backend.calls).toHaveLength(1);
});

it('loads a matching task workspace read-only and keeps failures recoverable', async () => {
  let fail = true;
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => task,
    'GET /api/v1/graphs/task-run-example':() => fail ? apiError(503,'Workspace unavailable','unavailable') : {graph:{metadata:{id:'task-run-example'},spec:{nodes:[]}}},
    'GET /api/v1/graphs/task-run-example/status':() => workspaceStatus,
    'GET /api/v1/graphs/task-run-example/files':() => [],
    'GET /api/v1/graphs/task-run-example/git/status':() => ({repository:false}),
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="files"/>);
  expect(await screen.findByText('Workspace unavailable')).toBeTruthy();
  expect(backend.calls.some(c => c.path.endsWith('/files'))).toBe(false);
  fail = false;
  await userEvent.click(screen.getByRole('button', {name:'Retry workspace'}));
  const files = await screen.findByRole('region', {name:'Task workspace files'});
  expect(await within(files).findByText('Read-only view. Workspace files can only be changed by agents.')).toBeTruthy();
  expect(backend.calls.every(c => c.method === 'GET')).toBe(true);
});

it('rejects workspace responses from a different graph before fetching files', async () => {
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => task,
    'GET /api/v1/graphs/task-run-example':() => ({graph:{metadata:{id:'other'},spec:{nodes:[]}}}),
    'GET /api/v1/graphs/task-run-example/status':() => workspaceStatus,
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="files"/>);
  expect(await screen.findByText('The workspace response does not match this execution copy.')).toBeTruthy();
  expect(backend.calls).toHaveLength(3);
});

it('preserves read-only access to replica one with exact workspace request scope', async () => {
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => task,
    'GET /api/v1/graphs/task-run-example':() => ({graph:{metadata:{id:'task-run-example'},spec:{nodes:[{id:'worker',workspace:{mode:'worktree'}}]}}}),
    'GET /api/v1/graphs/task-run-example/status':() => ({...workspaceStatus,nodes:[{nodeId:'worker',desiredReplicas:2,replicas:[]}]}),
    'GET /api/v1/graphs/task-run-example/files':() => [{name:'result.txt',path:'result.txt',dir:false,size:6,modTime:'2026-11-23T12:35:12Z'}],
    'GET /api/v1/graphs/task-run-example/files/content':() => new Response('replica one result'),
    'GET /api/v1/graphs/task-run-example/git/status':() => ({repository:false}),
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="files"/>);
  await userEvent.selectOptions(await screen.findByLabelText('Workspace'),'worker');
  await userEvent.selectOptions(screen.getByLabelText('Replica'),'1');
  await userEvent.click(await screen.findByRole('button',{name:/result.txt/}));
  expect(await screen.findByText('replica one result')).toBeTruthy();
  const fileURLs = backend.fetch.mock.calls.map(([url]) => new URL(String(url),'http://localhost')).filter(url => url.pathname.endsWith('/files') || url.pathname.endsWith('/files/content'));
  expect(fileURLs.some(url => url.pathname.endsWith('/files') && url.searchParams.get('node') === 'worker' && url.searchParams.get('replica') === '1')).toBe(true);
  const content = fileURLs.find(url => url.pathname.endsWith('/files/content'))!;
  expect(content.searchParams.get('node')).toBe('worker');
  expect(content.searchParams.get('replica')).toBe('1');
  expect(backend.calls.every(c => c.method === 'GET')).toBe(true);
});

it('retains historical replica indices after scale-down', async () => {
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => task,
    'GET /api/v1/graphs/task-run-example':() => ({graph:{metadata:{id:'task-run-example'},spec:{nodes:[{id:'worker',workspace:{mode:'worktree'}}]}}}),
    'GET /api/v1/graphs/task-run-example/status':() => ({...workspaceStatus,nodes:[{nodeId:'worker',desiredReplicas:1,replicas:[{graphId:'task-run-example',nodeId:'worker',replica:1,state:'stopped'}]}]}),
    'GET /api/v1/graphs/task-run-example/files':() => [],
    'GET /api/v1/graphs/task-run-example/git/status':() => ({repository:false}),
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="files"/>);
  await userEvent.selectOptions(await screen.findByLabelText('Workspace'),'worker');
  expect(within(screen.getByLabelText('Replica')).getByRole('option',{name:'1'})).toBeTruthy();
  expect(backend.calls.every(c => c.method === 'GET')).toBe(true);
});

it('reports unavailable historical replica inventory without silently restricting inspection to replica zero', async () => {
  let failed = true;
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => task,
    'GET /api/v1/graphs/task-run-example':() => ({graph:{metadata:{id:'task-run-example'},spec:{nodes:[]}}}),
    'GET /api/v1/graphs/task-run-example/status':() => failed ? apiError(404,'Historical runtime unavailable','not_found') : workspaceStatus,
    'GET /api/v1/graphs/task-run-example/files':() => [],
    'GET /api/v1/graphs/task-run-example/git/status':() => ({repository:false}),
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="files"/>);
  expect(await screen.findByText('Workspace replica inventory unavailable: Historical runtime unavailable')).toBeTruthy();
  expect(screen.getByText(/No replica is assumed for this historical task/)).toBeTruthy();
  expect(backend.calls.some(c => c.path.endsWith('/files'))).toBe(false);
  failed = false;
  await userEvent.click(screen.getByRole('button',{name:'Retry workspace inventory'}));
  expect(await screen.findByText('No files in this directory.')).toBeTruthy();
  expect(backend.calls.every(c => c.method === 'GET')).toBe(true);
});

it('rejects a foreign replica inventory before reading files', async () => {
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => task,
    'GET /api/v1/graphs/task-run-example':() => ({graph:{metadata:{id:'task-run-example'},spec:{nodes:[]}}}),
    'GET /api/v1/graphs/task-run-example/status':() => ({...workspaceStatus,graphId:'other'}),
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="files"/>);
  expect(await screen.findByText('The workspace inventory does not match this execution copy.')).toBeTruthy();
  expect(backend.calls).toHaveLength(3);
});

it('does not assume replica zero when a worktree node is omitted from historical status', async () => {
  const backend = fakeApi({
    'GET /api/v1/tasks/tsk_example':() => task,
    'GET /api/v1/graphs/task-run-example':() => ({graph:{metadata:{id:'task-run-example'},spec:{nodes:[{id:'worker',workspace:{mode:'worktree'}}]}}}),
    'GET /api/v1/graphs/task-run-example/status':() => workspaceStatus,
  });
  render(<TaskExecutionPage graphId="task-run-example" tab="files"/>);
  expect(await screen.findByText('Workspace replica inventory is incomplete for worker. No replica is assumed.')).toBeTruthy();
  expect(backend.calls).toHaveLength(3);
  expect(screen.queryByLabelText('Workspace')).toBeNull();
});
