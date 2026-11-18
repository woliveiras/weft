import { cleanup, render, screen } from '@testing-library/react';
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
