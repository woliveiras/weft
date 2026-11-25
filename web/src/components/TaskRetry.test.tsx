import {act, cleanup, fireEvent, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, expect, it, vi} from 'vitest';
import type {TaskDraft} from '../api/tasks';
import {apiError, fakeApi} from '../test/fakeApi';
import {canRetryTask, TaskRetry, TaskRetryOrigin} from './TaskRetry';

const task = {id:'tsk_original', revision:7, status:'failed', title:'Fix dependencies', request:'Inspect dependencies', graphId:'dependency-security', graphVersion:1, nodeId:'fixer', outcome:'implement'} as TaskDraft;
const child = {...task,id:'tsk_retry',revision:1,status:'draft',retryOfTaskId:task.id,retryOfRevision:7};
afterEach(() => {cleanup(); vi.unstubAllGlobals(); history.replaceState(null,'','/');});

it('limits the action to a persisted failed manual task', () => {
  expect(canRetryTask(undefined)).toBe(false);
  expect(canRetryTask(task)).toBe(true);
  for (const status of ['draft','running','succeeded','blocked','interrupted','cancelled','deleted']) expect(canRetryTask({...task,status})).toBe(false);
  expect(canRetryTask({...task,triggerGitHubIssue:'owner/repo#1'})).toBe(false);
  expect(canRetryTask({...task,triggerIssueOrigin:{kind:'jira'}})).toBe(false);
});

it('explains the fresh draft and sends only explicit confirmation with the reviewed revision', async () => {
  const backend=fakeApi({'POST /api/v1/tasks/tsk_original/retry':()=>child});
  render(<TaskRetry task={task} canEdit/>);
  expect(screen.getByText(/new workspace and its own approvals/)).toBeTruthy();
  expect(screen.getByText(/original task and costs remain available/)).toBeTruthy();
  await userEvent.click(screen.getByRole('button',{name:'Try again'}));
  await waitFor(()=>expect(location.hash).toBe('#/tasks/tsk_retry'));
  expect(backend.calls).toHaveLength(1);
  expect(backend.calls[0]).toMatchObject({method:'POST',path:'/api/v1/tasks/tsk_original/retry',headers:{'If-Match':'7'},body:{confirm:true}});
});

it('acknowledges slow creation and prevents duplicate local submissions', async () => {
  let release!:(value:TaskDraft)=>void;
  const backend=fakeApi({'POST /api/v1/tasks/tsk_original/retry':()=>new Promise<TaskDraft>(resolve=>{release=resolve;})});
  render(<TaskRetry task={task} canEdit/>);
  fireEvent.click(screen.getByRole('button',{name:'Try again'}));
  const pending=screen.getByRole('button',{name:'Creating attempt…'});
  expect(pending).toHaveProperty('disabled',true);
  expect(pending.getAttribute('aria-busy')).toBe('true');
  expect(screen.getByRole('status').textContent).toContain('Creating a new attempt');
  fireEvent.click(pending);
  expect(backend.calls).toHaveLength(1);
  await act(async()=>release(child));
  expect(location.hash).toBe('#/tasks/tsk_retry');
});

it('keeps a failed response visible and retries the same source snapshot after an uncertain result', async () => {
  const backend=fakeApi({'POST /api/v1/tasks/tsk_original/retry':vi.fn().mockReturnValueOnce(apiError(503,'Storage unavailable','unavailable')).mockReturnValue(child)});
  render(<TaskRetry task={task} canEdit/>);
  await userEvent.click(screen.getByRole('button',{name:'Try again'}));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent',expect.stringContaining('Storage unavailable'));
  expect(location.hash).toBe('');
  await userEvent.click(screen.getByRole('button',{name:'Try again'}));
  await waitFor(()=>expect(location.hash).toBe('#/tasks/tsk_retry'));
  expect(backend.calls).toHaveLength(2);
  expect(backend.calls[0].body).toEqual(backend.calls[1].body);
  expect(backend.calls[0].headers['If-Match']).toBe(backend.calls[1].headers['If-Match']);
});

it('opens the same returned attempt after another caller has edited or started it', async () => {
  fakeApi({'POST /api/v1/tasks/tsk_original/retry':()=>({...child,status:'running',revision:5,title:'Edited attempt',executionGraphId:'task-run-retry'})});
  render(<TaskRetry task={task} canEdit/>);
  await userEvent.click(screen.getByRole('button',{name:'Try again'}));
  await waitFor(()=>expect(location.hash).toBe('#/tasks/tsk_retry'));
});

it('rejects unrelated retry responses instead of navigating to another task', async () => {
  fakeApi({'POST /api/v1/tasks/tsk_original/retry':()=>({...child,retryOfRevision:6})});
  render(<TaskRetry task={task} canEdit/>);
  await userEvent.click(screen.getByRole('button',{name:'Try again'}));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent',expect.stringContaining('does not belong to this failed task snapshot'));
  expect(location.hash).toBe('');
});

it('keeps readers and nonfailed records read-only', () => {
  const backend=fakeApi({});
  const view=render(<TaskRetry task={task} canEdit={false}/>);
  expect(screen.queryByRole('button',{name:'Try again'})).toBeNull();
  view.rerender(<TaskRetry task={{...task,status:'running'}} canEdit/>);
  expect(screen.queryByRole('button',{name:'Try again'})).toBeNull();
  expect(backend.calls).toHaveLength(0);
});

it('shows retained origin on drafts and started attempts using a local task link', () => {
  const view=render(<TaskRetryOrigin task={child}/>);
  expect(screen.getByRole('link',{name:'Original failed task'}).getAttribute('href')).toBe('#/tasks/tsk_original');
  expect(screen.getByText(/Review this attempt before starting/)).toBeTruthy();
  view.rerender(<TaskRetryOrigin task={{...child,status:'running'}}/>);
  expect(screen.getByRole('link',{name:'Original failed task'})).toBeTruthy();
  expect(screen.queryByText(/Review this attempt before starting/)).toBeNull();
});

it.each([
  {retryOfTaskId:'tsk_foreign'},
  {id:'../outside'},
  {id:task.id},
])('rejects a foreign or invalid returned route: %j',async invalid=>{
  fakeApi({'POST /api/v1/tasks/tsk_original/retry':()=>({...child,...invalid})});
  render(<TaskRetry task={task} canEdit/>);
  await userEvent.click(screen.getByRole('button',{name:'Try again'}));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent',expect.stringContaining('does not belong to this failed task snapshot'));
  expect(location.hash).toBe('');
});

it('focuses a new draft origin once without stealing focus on refresh',()=>{
 const view=render(<><button>Keep working</button><TaskRetryOrigin task={child}/></>);
 expect(screen.getByRole('complementary',{name:'Attempt origin'})).toBe(document.activeElement);
 screen.getByRole('button',{name:'Keep working'}).focus();
 view.rerender(<><button>Keep working</button><TaskRetryOrigin task={{...child,revision:2}}/></>);
 expect(screen.getByRole('button',{name:'Keep working'})).toBe(document.activeElement);
});

it('explains a stale source conflict without navigating or silently adopting a new revision',async()=>{
 const backend=fakeApi({'POST /api/v1/tasks/tsk_original/retry':()=>apiError(409,'Source revision changed','conflict')});
 render(<TaskRetry task={task} canEdit/>);
 await userEvent.click(screen.getByRole('button',{name:'Try again'}));
 expect(await screen.findByRole('alert')).toHaveProperty('textContent',expect.stringContaining('Refresh the task and review its current state'));
 expect(location.hash).toBe('');
 expect(backend.calls).toHaveLength(1);
 expect(backend.calls[0].headers['If-Match']).toBe('7');
});
