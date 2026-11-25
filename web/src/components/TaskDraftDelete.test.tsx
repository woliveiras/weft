import {cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach, beforeEach, expect, it, vi} from 'vitest';
import type {TaskDraft} from '../api/tasks';
import {apiError, fakeApi} from '../test/fakeApi';
import {canDeleteTaskDraft, TaskDraftDelete} from './TaskDraftDelete';

const task = {id:'tsk_draft', revision:7, status:'draft', title:'Fix dependencies', request:'Inspect dependencies', graphId:'dependency-security', graphVersion:1, nodeId:'fixer', outcome:'implement'} as TaskDraft;
const showModal = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal');
beforeEach(() => {
  // jsdom lacks the native dialog top layer; preserve all component event behavior.
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {configurable:true, value(this:HTMLDialogElement) {this.setAttribute('open','');}});
});
afterEach(() => {
  cleanup(); vi.restoreAllMocks(); vi.unstubAllGlobals();
  if (showModal) Object.defineProperty(HTMLDialogElement.prototype,'showModal',showModal);
  else Reflect.deleteProperty(HTMLDialogElement.prototype,'showModal');
});

it('limits draft eligibility to records without any execution or preparation', () => {
  expect(canDeleteTaskDraft(undefined)).toBe(false);
  expect(canDeleteTaskDraft(task)).toBe(true);
  expect(canDeleteTaskDraft({...task,submitted:true,resolution:{state:'invalid_proposal',message:'Repair settings'}})).toBe(true);
  for (const status of ['preparing','environment_pending','running','succeeded','failed','cancelled','interrupted','deleted']) {
    expect(canDeleteTaskDraft({...task,status})).toBe(false);
  }
  expect(canDeleteTaskDraft({...task,executionGraphId:'task-run-draft'})).toBe(false);
  expect(canDeleteTaskDraft({...task,environmentApprovalId:'apr_env'})).toBe(false);
});

it('requires named confirmation, sends the reviewed revision and exits only after success', async () => {
  const backend = fakeApi({'DELETE /api/v1/tasks/tsk_draft':() => new Response(null,{status:204})});
  const onDeleted = vi.fn();
  const onSubmit = vi.fn(event => event.preventDefault());
  render(<form onSubmit={onSubmit}><TaskDraftDelete task={task} canEdit onDeleted={onDeleted}/></form>);
  await userEvent.click(screen.getByRole('button',{name:'Delete draft'}));
  const popup = screen.getByRole('dialog',{name:'Delete task draft?'});
  expect(within(popup).getByText('Fix dependencies')).toBeTruthy();
  expect(backend.calls).toHaveLength(0);
  expect(onDeleted).not.toHaveBeenCalled();
  await userEvent.click(within(popup).getByRole('button',{name:'Delete draft permanently'}));
  await waitFor(() => expect(onDeleted).toHaveBeenCalledOnce());
  expect(backend.calls).toHaveLength(1);
  expect(backend.calls[0]).toMatchObject({method:'DELETE',path:'/api/v1/tasks/tsk_draft',headers:{'If-Match':'7'},body:{confirm:true}});
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(onSubmit).not.toHaveBeenCalled();
});

it('supports cancellation and Escape without mutation and restores trigger focus', async () => {
  const backend = fakeApi({});
  const onDeleted = vi.fn();
  render(<TaskDraftDelete task={task} canEdit onDeleted={onDeleted}/>);
  const trigger = screen.getByRole('button',{name:'Delete draft'});
  await userEvent.click(trigger);
  await userEvent.click(screen.getByRole('button',{name:'Cancel'}));
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(document.activeElement).toBe(trigger);
  await userEvent.click(trigger);
  fireEvent(screen.getByRole('dialog'),new Event('cancel',{cancelable:true}));
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(backend.calls).toHaveLength(0);
  expect(onDeleted).not.toHaveBeenCalled();
});

it('hides deletion for viewers and tasks with prepared or started execution', () => {
  const {rerender} = render(<TaskDraftDelete task={task} canEdit={false} onDeleted={vi.fn()}/>);
  expect(screen.queryByRole('button')).toBeNull();
  for (const record of [{...task,status:'succeeded'},{...task,status:'cancelled'},{...task,executionGraphId:'task-run-draft'},{...task,environmentApprovalId:'apr_env'}]) {
    rerender(<TaskDraftDelete task={record} canEdit onDeleted={vi.fn()}/>);
    expect(screen.queryByRole('button')).toBeNull();
  }
});

it('keeps confirmation available after failure and permits a retry without claiming a retained draft', async () => {
  let fail = true;
  const backend = fakeApi({'DELETE /api/v1/tasks/tsk_draft':() => fail ? apiError(503,'Storage unavailable','unavailable') : new Response(null,{status:204})});
  const onDeleted = vi.fn();
  render(<TaskDraftDelete task={task} canEdit onDeleted={onDeleted}/>);
  await userEvent.click(screen.getByRole('button',{name:'Delete draft'}));
  await userEvent.click(screen.getByRole('button',{name:'Delete draft permanently'}));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent',expect.stringContaining('Check Tasks or refresh the task before trying again'));
  expect(onDeleted).not.toHaveBeenCalled();
  expect(screen.getByRole('dialog')).toBeTruthy();
  fail = false;
  await userEvent.click(screen.getByRole('button',{name:'Delete draft permanently'}));
  await waitFor(() => expect(onDeleted).toHaveBeenCalledOnce());
  expect(backend.calls).toHaveLength(2);
});

it('explains conflicts and does not silently adopt an unreviewed revision', async () => {
  const backend = fakeApi({'DELETE /api/v1/tasks/tsk_draft':() => apiError(409,'storage: conflict','conflict')});
  const onDeleted = vi.fn();
  const {rerender} = render(<TaskDraftDelete task={task} canEdit onDeleted={onDeleted}/>);
  await userEvent.click(screen.getByRole('button',{name:'Delete draft'}));
  rerender(<TaskDraftDelete task={{...task,revision:8,title:'Another edit'}} canEdit onDeleted={onDeleted}/>);
  await userEvent.click(screen.getByRole('button',{name:'Delete draft permanently'}));
  expect(await screen.findByRole('alert')).toHaveProperty('textContent',expect.stringContaining('Cancel, refresh the task'));
  expect(backend.calls[0].headers['If-Match']).toBe('7');
  expect(onDeleted).not.toHaveBeenCalled();
});

it('prevents duplicate deletion and cancellation while the request is pending', async () => {
  let finish!:(result:Response) => void;
  const backend = fakeApi({'DELETE /api/v1/tasks/tsk_draft':() => new Promise<Response>(resolve => {finish=resolve;})});
  const onDeleted = vi.fn();
  render(<TaskDraftDelete task={task} canEdit onDeleted={onDeleted}/>);
  await userEvent.click(screen.getByRole('button',{name:'Delete draft'}));
  await userEvent.click(screen.getByRole('button',{name:'Delete draft permanently'}));
  expect(await screen.findByRole('status')).toHaveProperty('textContent','Deleting draft…');
  expect((screen.getByRole('button',{name:'Cancel'}) as HTMLButtonElement).disabled).toBe(true);
  await userEvent.click(screen.getByRole('button',{name:'Deleting…'}));
  fireEvent(screen.getByRole('dialog'),new Event('cancel',{cancelable:true}));
  expect(screen.getByRole('dialog')).toBeTruthy();
  expect(backend.calls).toHaveLength(1);
  expect(onDeleted).not.toHaveBeenCalled();
  finish(new Response(null,{status:204}));
  await waitFor(() => expect(onDeleted).toHaveBeenCalledOnce());
});

it('does not claim retention when the network loses the deletion response', async () => {
  const backend = fakeApi({'DELETE /api/v1/tasks/tsk_draft':() => {throw new TypeError('Failed to fetch');}});
  const onDeleted = vi.fn();
  render(<TaskDraftDelete task={task} canEdit onDeleted={onDeleted}/>);
  await userEvent.click(screen.getByRole('button',{name:'Delete draft'}));
  await userEvent.click(screen.getByRole('button',{name:'Delete draft permanently'}));
  const failure = await screen.findByRole('alert');
  expect(failure.textContent).toContain('Could not confirm deletion');
  expect(failure.textContent).toContain('Check Tasks or refresh');
  expect(failure.textContent).not.toContain('retained');
  expect(screen.getByRole('dialog')).toBeTruthy();
  expect(backend.calls).toHaveLength(1);
  expect(onDeleted).not.toHaveBeenCalled();
});
