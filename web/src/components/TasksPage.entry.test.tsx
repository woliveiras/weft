import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import type { Info } from '../api/types';
import { apiError, fakeApi } from '../test/fakeApi';
import { TasksPage } from './TasksPage';

const info = { identity: { role: 'admin' } } as Info;

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

it('lists saved example drafts through the ordinary Tasks entry point without writing data', async () => {
  const backend = fakeApi({
    'GET /api/v1/tasks': () => ({ items: [{ id: 'tsk_example', title: 'Synthetic first task', status: 'draft' }] }),
  });
  render(<TasksPage info={info} graphs={[]} />);
  expect(await screen.findByRole('link', { name: 'Synthetic first task' })).toHaveProperty('hash', '#/tasks/tsk_example');
  expect(screen.getByRole('link', { name: 'New task' })).toHaveProperty('hash', '#/tasks/new');
  expect(screen.queryByRole('heading', { name: 'Try the task flow' })).toBeNull();
  expect(backend.calls.every(call => call.method === 'GET')).toBe(true);
});

it('keeps the normal task entry and error recovery available if the history cannot load', async () => {
  fakeApi({ 'GET /api/v1/tasks': () => apiError(503, 'Task storage unavailable', 'unavailable') });
  render(<TasksPage info={info} graphs={[]} />);
  await screen.findByText('Task storage unavailable');
  expect(screen.getByRole('button', { name: 'Refresh tasks' })).toHaveProperty('disabled', false);
  expect(screen.getByRole('link', { name: 'New task' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Preview example' })).toBeNull();
});

it('filters by observed status and issue and keeps the filters when moving between pages', async () => {
  const user = userEvent.setup();
  const createdAt = '2026-11-18T12:00:00Z';
  const first = { id: 'tsk_one', title: 'Review change', status: 'running', observedState: 'failed',
    graphId: 'reviewer', graphVersion: 2, triggerGitHubIssue: 'repo/change#12', createdAt, updatedAt: createdAt };
  const second = { ...first, id: 'tsk_two', observedState: 'succeeded', graphVersion: 3 };
  const backend = fakeApi({ 'GET /api/v1/tasks': vi.fn()
    .mockReturnValueOnce({ items: [first, second], next: second.id })
    .mockReturnValueOnce({ items: [{ ...first, id: 'tsk_other', graphId: 'other', triggerGitHubIssue: 'other/repo#5' }] })
    .mockReturnValue({ items: [first, second], next: second.id }) });

  render(<TasksPage info={info} graphs={[]} />);
  await screen.findByRole('table', { name: 'Task history' });
  await user.type(screen.getByRole('searchbox', { name: 'Search tasks' }), '  CHANGE#12 ');
  await user.selectOptions(screen.getByRole('combobox', { name: 'Task status' }), 'failed');
  await user.selectOptions(screen.getByRole('combobox', { name: 'Workflow' }), 'reviewer');
  const row = screen.getByRole('row', { name: /tsk_one/ });
  expect(within(row).getByText('Failed')).toBeTruthy();
  expect(within(row).getByText('Version 2')).toBeTruthy();
  expect(row.querySelector('time')?.dateTime).toBe(createdAt);
  expect(screen.queryByText('tsk_two')).toBeNull();

  await user.click(screen.getByRole('button', { name: 'Next page' }));
  await screen.findByText('No tasks match these filters on this page.');
  expect(screen.getByRole('combobox', { name: 'Workflow' })).toHaveProperty('value', 'reviewer');
  expect(backend.fetch.mock.calls.some(([url]) => String(url).includes('after=tsk_two'))).toBe(true);
  await user.click(screen.getByRole('button', { name: 'Previous page' }));
  await screen.findByText('tsk_one');
  await user.click(screen.getByRole('button', { name: 'Clear filters' }));
  expect(screen.getByRole('status').textContent).toBe('2 of 2 tasks on this page');
});
