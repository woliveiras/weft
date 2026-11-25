import {act, cleanup, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, expect, it} from 'vitest';
import {fakeApi, apiError} from '../test/fakeApi';
import {TasksPage} from './TasksPage';
import type {Info, Execution} from '../api/types';
import type {TaskDraft, TaskProgress} from '../api/tasks';

afterEach(() => {cleanup(); history.replaceState(null, '', '/');});
const task = {id:'tsk_demo',revision:2,status:'running',title:'Implement a greeting',request:'Original request <b>inert</b>',outcome:'implement',graphId:'factory',graphVersion:2,nodeId:'worker',executionGraphId:'task-run-demo',createdAt:'2026-11-20T01:00:00Z'} as TaskDraft;
const execution = {id:'exe_demo',graphId:'task-run-demo',status:'succeeded',nodeId:'worker',replica:0,startedAt:'2026-11-23T12:00:00Z',completedAt:'2026-11-23T12:02:00Z',summary:'Implemented the API.'} as Execution;
const progress = {task,state:'succeeded',executions:[execution],result:execution,pendingApprovals:[],blockers:[],delivery:{connection:'bb',repository:'team/repo',branch:'weft/demo',pullRequestNumber:1,pullRequestUrl:'https://bitbucket.org/team/repo/pull-requests/1'}} as TaskProgress;
const plan = {id:'plan',seq:3,graphId:'task-run-demo',executionId:'exe_demo',nodeId:'worker',replica:0,type:'agent.plan',time:'2026-11-23T12:01:00Z',data:{items:[{step:'Inspect code',status:'done'},{step:'Run verification',status:'done'}]}};
const budget = {scope:'run',id:'task-run-demo',currency:'USD',limitMicros:2_000_000,chargedMicros:110_000,estimatedMicros:0,reservedMicros:0,remainingMicros:1_890_000,paused:false,reached:false};
function backend(getProgress: () => unknown = () => progress, getResult: () => unknown = () => execution, getStatus: () => unknown = () => ({graphId:'task-run-demo',state:'stopped',desiredState:'stopped'}), cancel: () => unknown = () => task, getBudget: () => unknown = () => budget) {
  return fakeApi({
    'GET /api/v1/tasks/tsk_demo':()=>task,
    'GET /api/v1/providers':()=>[],
    'GET /api/v1/projects':()=>[],
    'GET /api/v1/tasks/tsk_demo/progress':getProgress,
    'GET /api/v1/cost/budgets/run/task-run-demo':getBudget,
    'GET /api/v1/graphs/task-run-demo/events':()=>[plan],
    'GET /api/v1/graphs/task-run-demo':()=>({graph:{metadata:{id:'task-run-demo'},spec:{nodes:[],checks:[]}}}),
    'GET /api/v1/executions/exe_demo':getResult,
    'GET /api/v1/graphs/task-run-demo/status':getStatus,
    'POST /api/v1/tasks/tsk_demo/cancel':cancel,
    'GET /api/v1/tasks/tsk_demo/authorization':()=>({scope:'scope',capabilities:[{id:'worker',isolation:'docker',grants:[{name:'shell.exec',commands:['go','gofmt']}]}]}),
  });
}

it('puts live plans before the readable result and defers request, configuration and evidence APIs', async () => {
  const api = backend();
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  const plans = await screen.findByRole('region',{name:'Agent plans'});
  expect(await within(plans).findByText('Run verification')).toBeTruthy();
  expect(plans.compareDocumentPosition(screen.getByRole('region',{name:'Result'})) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  expect(screen.queryByRole('region',{name:'Request'})).toBeNull();
  expect(screen.queryByRole('region',{name:'Task authorization'})).toBeNull();
  expect(api.find('GET','/api/v1/tasks/tsk_demo/authorization')).toHaveLength(0);
  expect(api.find('GET','/api/v1/graphs/task-run-demo')).toHaveLength(0);
  expect(screen.getByText(/\$0.11 spent of \$2.00/)).toBeTruthy();
  expect(screen.getByRole('link',{name:'Open pull request'}).getAttribute('href')).toBe(progress.delivery!.pullRequestUrl);
  expect(screen.queryByRole('button',{name:'Stop task'})).toBeNull();
  const summary = screen.getByLabelText('Agent summary');
  expect(summary.textContent).toBe(execution.summary);
  await userEvent.click(screen.getByRole('button',{name:'Read full result'}));
  expect(summary.classList.contains('task-result-preview')).toBe(false);
  await waitFor(()=>expect(api.find('GET','/api/v1/executions/exe_demo')).toHaveLength(1));
  expect(api.calls.every(call=>call.method==='GET')).toBe(true);
});

it('supports direct section links, back/forward changes and recorded delivery without inventing current remote state', async () => {
  history.replaceState(null,'', '#/tasks/tsk_demo?tab=changes');
  const api = backend(); render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  expect(await screen.findByRole('region',{name:'Review task evidence'})).toBeTruthy();
  expect(await screen.findByText(/Weft recorded a push of/)).toBeTruthy();
  expect(screen.queryByText('Remote state not verified. No automatic push is performed on task completion.')).toBeNull();
  await userEvent.click(screen.getByRole('link',{name:'Request'}));
  const request = await screen.findByRole('region',{name:'Request'});
  expect(request.textContent).toContain(task.request);
  expect(request.querySelector('b')).toBeNull();
  await act(async()=>{history.replaceState(null,'','#/tasks/tsk_demo?tab=settings');window.dispatchEvent(new HashChangeEvent('hashchange'));});
  const auth = await screen.findByRole('region',{name:'Task authorization'});
  await within(auth).findByText('Isolation: docker');
  expect(within(auth).getByRole('list',{name:'Tools granted to worker'}).textContent).toContain('go, gofmt');
  expect(within(auth).getByText('View raw grant data')).toBeTruthy();
  expect(within(auth).queryByRole('button',{name:'Confirm authorization'})).toBeNull();
  await act(async()=>{history.replaceState(null,'','#/tasks/tsk_demo');window.dispatchEvent(new HashChangeEvent('hashchange'));});
  expect(await screen.findByRole('region',{name:'Agent plans'})).toBeTruthy();
  expect(screen.queryByRole('region',{name:'Task authorization'})).toBeNull();
  expect(api.calls.every(call=>call.method==='GET')).toBe(true);
});

it('keeps pending decisions visible from Request and does not grant viewers approval rights', async () => {
  history.replaceState(null,'','#/tasks/tsk_demo?tab=request');
  backend(()=>({...progress,state:'running',pendingApprovals:[{id:'apr_1',graphId:'task-run-demo',status:'pending',kind:'tool',title:'Open a pull request',createdAt:'2026-11-23T12:00:00Z'}]}));
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  expect(await screen.findByText('Open a pull request')).toBeTruthy();
  expect(screen.getByRole('region',{name:'Request'})).toBeTruthy();
  expect(screen.queryByRole('button',{name:'Approve'})).toBeNull();
});

it('retains the last snapshot on refresh failure, labels it stale and recovers on retry', async () => {
  let fail=false;
  const api=backend(()=>fail?apiError(503,'Progress offline','unavailable'):progress);
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'admin'}} as Info}/>);
  await screen.findByText('Task status: succeeded');
  fail=true; await userEvent.click(screen.getByRole('button',{name:'Refresh task'}));
  expect(await screen.findByText('Progress offline')).toBeTruthy();
  expect(screen.getByText('Last recorded snapshot · refresh failed')).toBeTruthy();
  expect(screen.getByRole('link',{name:'Open pull request'})).toBeTruthy();
  fail=false; await userEvent.click(screen.getByRole('button',{name:'Refresh task'}));
  await waitFor(()=>expect(screen.queryByText('Progress offline')).toBeNull());
  expect(api.calls.every(call=>call.method==='GET')).toBe(true);
});

it('rejects a progress response for another task instead of displaying its result', async () => {
  backend(()=>({...progress,task:{...task,id:'tsk_other'},result:{...execution,summary:'Foreign private output'}}));
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  expect(await screen.findByText('The progress response does not belong to this task.')).toBeTruthy();
  expect(screen.queryByText('Foreign private output')).toBeNull();
});

it('loads the full persisted result on demand, retains the preview on failure and retries read-only', async () => {
  let fail=true;
  const api=backend(()=>progress,()=>fail?apiError(503,'Result storage offline','unavailable'):{...execution,summary:'Implemented the API.\n\nTests: all verification commands passed.'});
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  await screen.findByRole('button',{name:'Read full result'});
  expect(api.find('GET','/api/v1/executions/exe_demo')).toHaveLength(0);
  await userEvent.click(screen.getByRole('button',{name:'Read full result'}));
  await screen.findByText('Full result unavailable: Result storage offline');
  expect(screen.getByLabelText('Agent summary').textContent).toBe(execution.summary);
  fail=false; await userEvent.click(screen.getByRole('button',{name:'Retry full result'}));
  expect(await screen.findByText(/Tests: all verification commands passed/)).toBeTruthy();
  expect(api.calls.every(call=>call.method==='GET')).toBe(true);
});

it('does not display an expanded result returned for another execution', async () => {
  backend(()=>progress,()=>({...execution,id:'exe_foreign',summary:'Foreign full result'}));
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  await userEvent.click(await screen.findByRole('button',{name:'Read full result'}));
  expect(await screen.findByText('Full result unavailable: The result response does not belong to this execution.')).toBeTruthy();
  expect(screen.queryByText('Foreign full result')).toBeNull();
});

for (const outcome of ['failed', 'succeeded']) {
  it(`preserves explicit cancellation for a historical ${outcome} copy that is still running`, async () => {
    let cancelling=false; let fail=true;
    const routes=backend(()=>({...progress,state:outcome,task:{...task,status:cancelling?'cancel_requested':outcome}}),()=>execution,
      ()=>({graphId:'task-run-demo',state:'running',desiredState:'running'}),
      ()=>{if(fail)return apiError(503,'Cancellation unavailable','unavailable');cancelling=true;return {...task,status:'cancel_requested'};});
    render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'admin'}} as Info}/>);
    await userEvent.click(await screen.findByText('Stop task',{selector:'summary'}));
    await userEvent.click(screen.getByRole('checkbox',{name:/Stop this task's execution copy/}));
    await userEvent.click(screen.getByRole('button',{name:'Stop task'}));
    expect(await screen.findByText('Cancellation unavailable')).toBeTruthy();
    expect((screen.getByRole('checkbox') as HTMLInputElement).checked).toBe(true);
    fail=false;await userEvent.click(screen.getByRole('button',{name:'Refresh task'}));
    await waitFor(()=>expect((screen.getByRole('button',{name:'Stop task'}) as HTMLButtonElement).disabled).toBe(false));
    await userEvent.click(screen.getByRole('button',{name:'Stop task'}));
    await waitFor(()=>expect(screen.queryByRole('button',{name:'Stop task'})).toBeNull());
    expect(routes.find('POST','/api/v1/tasks/tsk_demo/cancel')).toHaveLength(2);
  });
}

it('does not offer cancellation for a completed copy confirmed stopped', async () => {
  const routes=backend();
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'admin'}} as Info}/>);
  await waitFor(()=>expect(routes.find('GET','/api/v1/graphs/task-run-demo/status')).toHaveLength(1));
  expect(screen.queryByText('Stop task',{selector:'summary'})).toBeNull();
});

it('labels a failed cost refresh while retaining the last recorded amount', async () => {
  let fail=false;
  backend(()=>progress,()=>execution,undefined,undefined,()=>fail?apiError(503,'Cost offline','unavailable'):budget);
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  await screen.findByText(/\$0.11 spent of \$2.00/);
  fail=true;await userEvent.click(screen.getByRole('button',{name:'Refresh task'}));
  expect(await screen.findByText(/Cost refresh failed/)).toBeTruthy();
  expect(screen.getByText(/\$0.11 spent of \$2.00/)).toBeTruthy();
});

it('acknowledges a manual refresh immediately and confirms even an unchanged snapshot', async () => {
  let release: (() => void) | undefined;
  let delayed = false;
  const api = backend(async () => {if (delayed) await new Promise<void>(resolve => {release = resolve;}); return progress;});
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  await screen.findByText('Task status: succeeded');
  delayed = true;
  await userEvent.click(screen.getByRole('button',{name:'Refresh task'}));
  const refreshing = screen.getByRole('button',{name:'Refreshing…'});
  expect(refreshing).toHaveProperty('disabled',true);
  expect(refreshing.getAttribute('aria-busy')).toBe('true');
  expect(screen.getByText('Refreshing the task snapshot…').getAttribute('role')).toBe('status');
  expect(screen.getByRole('link',{name:'Open pull request'})).toBeTruthy();
  await act(async () => {release!();});
  expect(await screen.findByText(/Task snapshot refreshed at/)).toBeTruthy();
  expect(screen.getByRole('button',{name:'Refresh task'})).toHaveProperty('disabled',false);
  expect(api.calls.every(call => call.method === 'GET')).toBe(true);
});

it('keeps environment monitoring scoped to its own linked section and retains read-only viewers', async () => {
  const api = backend(() => ({...progress, state:'environment_pending', task:{...task,status:'environment_pending'}, environment:{state:'building',phase:'building_image',log:'Build output <script>inert</script>',upToDate:false}}));
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  await screen.findByText('Preparing the environment. Agents have not started.');
  expect(screen.queryByRole('region',{name:'Build logs'})).toBeNull();
  await userEvent.click(screen.getByRole('link',{name:'View environment & logs'}));
  const logs = await screen.findByRole('region',{name:'Build logs'});
  expect(within(logs).getByLabelText('Build output').textContent).toContain('<script>inert</script>');
  expect(logs.querySelector('script')).toBeNull();
  expect(screen.queryByRole('button',{name:'Start prepared task'})).toBeNull();
  expect(screen.getByRole('link',{name:'Environment & logs'}).getAttribute('aria-current')).toBe('page');
  expect(screen.queryByRole('region',{name:'Agent plans'})).toBeNull();
  await userEvent.click(screen.getByRole('link',{name:'Request'}));
  expect(await screen.findByRole('region',{name:'Request'})).toBeTruthy();
  expect(screen.queryByRole('region',{name:'Build logs'})).toBeNull();
  expect(api.calls.every(call => call.method === 'GET')).toBe(true);
});

it('waits for a valid snapshot before reporting an absent task environment', async () => {
  history.replaceState(null,'','#/tasks/tsk_demo?tab=environment');
  let release!:()=>void;
  backend(async()=>{await new Promise<void>(resolve=>{release=resolve;});return progress;});
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  await screen.findByText('Loading the task environment snapshot…');
  expect(screen.queryByText(/No task environment has been prepared/)).toBeNull();
  await act(async()=>{release();});
  expect(await screen.findByText(/No task environment has been prepared/)).toBeTruthy();
  expect(screen.queryByLabelText('Build output')).toBeNull();
});

it('reports an unavailable initial environment snapshot and recovers without claiming absence', async () => {
  history.replaceState(null,'','#/tasks/tsk_demo?tab=environment');
  let fail=true;
  backend(()=>fail?apiError(503,'Progress unavailable','unavailable'):{...progress,environment:{state:'building',upToDate:false,log:'Build is running'}});
  render(<TasksPage id={task.id} graphs={[]} info={{identity:{role:'viewer'}} as Info}/>);
  expect(await screen.findByText('Environment snapshot unavailable. Use Refresh task to retry.')).toBeTruthy();
  expect(screen.queryByText(/No task environment has been prepared/)).toBeNull();
  fail=false;await userEvent.click(screen.getByRole('button',{name:'Refresh task'}));
  expect(await screen.findByLabelText('Build output')).toHaveProperty('textContent','Build is running');
});
