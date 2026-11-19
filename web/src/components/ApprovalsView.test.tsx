import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { Approval, HumanReview } from '../api/types';
import { EventStreamProvider } from '../hooks/useEventStream';
import { apiError, fakeApi } from '../test/fakeApi';
import { ApprovalsView } from './ApprovalsView';
class EventSourceStub { addEventListener() {} close() {} }
const show = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal');
const close = Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close');
beforeEach(() => {
  vi.stubGlobal('EventSource', EventSourceStub);
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { this.removeAttribute('open'); fireEvent(this, new Event('close')); } });
});
afterEach(() => {
  cleanup();
  for (const [key, descriptor] of [['showModal', show], ['close', close]] as const) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, key, descriptor);
    else Reflect.deleteProperty(HTMLDialogElement.prototype, key);
  }
});
const approval = (id = 'old', extra = {}): Approval => ({ id, graphId: 'flow', nodeId: 'worker', replica: 0, kind: 'tool', status: 'pending', title: 'Read repository ' + id, createdAt: '2026-11-19T10:00:00Z', detail: { tool: 'filesystem.read' }, ...extra });
const review = { id: 'r', graphId: 'other', nodeId: 'builder', replica: 0, state: 'pending', createdAt: '2026-11-19T10:01:00Z', candidate: 'abc123', generation: 1, session: 'none', request: { title: 'Check application', instructions: 'Inspect behavior' } } as HumanReview;
function view(role = 'admin', onChanged = vi.fn()) { return <EventStreamProvider token="fixture"><ApprovalsView role={role} onChanged={onChanged} /></EventStreamProvider>; }
function routes(approvals: Approval[] = [approval()], reviews: HumanReview[] = []) { return { 'GET /api/v1/approvals': () => approvals, 'GET /api/v1/reviews': () => reviews }; }
it('merges reviews and approvals in a table, filters conjunctively and clears filters', async () => {
  fakeApi(routes([approval(), approval('decided', { status: 'approved' })], [review])); render(view());
  const table = await screen.findByRole('table');
  expect(within(table).getAllByRole('row')[1].textContent).toContain('Check application');
  expect(screen.queryByText('filesystem.read')).toBeNull();
  await userEvent.type(screen.getByLabelText('Search requests'), ' READ ');
  expect(within(table).getAllByRole('row')).toHaveLength(2);
  await userEvent.selectOptions(screen.getByLabelText('Request type'), 'review');
  expect(await screen.findByText('No requests match these filters.')).toBeTruthy();
  await userEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
  expect(await screen.findByRole('table')).toBeTruthy();
  expect(within(screen.getByRole('table')).getAllByRole('row')).toHaveLength(4);
  expect(document.activeElement).toBe(screen.getByLabelText('Search requests'));
  await userEvent.selectOptions(screen.getByLabelText('Workflow'), 'other');
  expect(within(screen.getByRole('table')).getAllByRole('row')).toHaveLength(2);
});
it('preserves feedback on a failed decision and closes with a refreshed result on successful retry', async () => {
  let failed = true; let decided = false;
  const changed = vi.fn();
  const backend = fakeApi({ ...routes(), 'GET /api/v1/approvals': () => [approval('old', { status: decided ? 'approved' : 'pending' })],
    'POST /api/v1/approvals/old/approve': () => { if (failed) return apiError(409, 'Request changed; refresh before deciding', 'conflict'); decided = true; return approval('old', { status: 'approved' }); } });
  render(view('admin', changed));
  await userEvent.click(await screen.findByRole('button', { name: 'Read repository old' }));
  const dialog = screen.getByRole('dialog');
  await userEvent.type(within(dialog).getByLabelText('comment'), 'Verified scope');
  await userEvent.click(within(dialog).getByRole('button', { name: 'Approve' }));
  expect(await within(dialog).findByText('Request changed; refresh before deciding')).toBeTruthy();
  expect((within(dialog).getByLabelText('comment') as HTMLInputElement).value).toBe('Verified scope');
  failed = false;
  await userEvent.click(within(dialog).getByRole('button', { name: 'Approve' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(changed).toHaveBeenCalledOnce();
  expect(backend.find('POST', '/api/v1/approvals/old/approve')[1].body).toEqual({ comment: 'Verified scope' });
  expect(await screen.findByText('No requests match these filters.')).toBeTruthy();
});
it.each(['viewer', 'operator'])('keeps administrator-only decisions read-only for %s and restores focus on close', async role => {
  const backend = fakeApi(routes([approval('environment', { kind: 'task_environment' })])); render(view(role));
  const button = await screen.findByRole('button', { name: 'Read repository environment' });
  await userEvent.click(button);
  expect(within(screen.getByRole('dialog')).queryByRole('button', { name: 'Approve' })).toBeNull();
  await userEvent.click(screen.getByRole('button', { name: 'Close' }));
  expect(document.activeElement).toBe(button);
  expect(backend.calls.some(call => call.method === 'POST')).toBe(false);
  if (role === 'viewer') expect(backend.find('GET', '/api/v1/reviews')).toHaveLength(0);
});
it('reports source failure, preserves healthy requests, and retries without a false empty success', async () => {
  let failed = true;
  fakeApi({ ...routes(), 'GET /api/v1/approvals': () => failed ? apiError(503, 'Storage unavailable', 'unavailable') : [approval()] }); render(view());
  expect(await screen.findByText('Approval requests: Storage unavailable')).toBeTruthy();
  expect(screen.queryByText('No requests match these filters.')).toBeNull();
  failed = false;
  await userEvent.click(screen.getByRole('button', { name: 'Refresh approvals' }));
  expect(await screen.findByRole('button', { name: 'Read repository old' })).toBeTruthy();
  expect(screen.queryByText('Approval requests: Storage unavailable')).toBeNull();
});
it('paginates large scopes and resets the page when filters change', async () => {
  fakeApi(routes(Array.from({ length: 21 }, (_, i) => approval(String(i), { createdAt: new Date(Date.UTC(2026, 10, 19, 10, i)).toISOString() })))); render(view());
  const table = await screen.findByRole('table');
  expect(within(table).getAllByRole('row')).toHaveLength(21);
  expect(within(table).getAllByRole('row')[1].textContent).toContain('Read repository 20');
  await userEvent.click(screen.getByRole('button', { name: 'Next page' }));
  expect(within(table).getAllByRole('row')).toHaveLength(2);
  expect(screen.getByText('Page 2 of 2')).toBeTruthy();
  await userEvent.type(screen.getByLabelText('Search requests'), 'repository 20');
  expect(screen.getByRole('button', { name: 'Read repository 20' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
});
it('requires a nonblank answer and sends rejection feedback through the same decision API', async () => {
  const backend = fakeApi({ ...routes([approval('question', { kind: 'question' })]), 'POST /api/v1/approvals/question/reject': () => approval('question', { status: 'rejected' }) });
  render(view());
  await userEvent.click(await screen.findByRole('button', { name: 'Read repository question' }));
  const dialog = within(screen.getByRole('dialog'));
  expect((dialog.getByRole('button', { name: 'Answer' }) as HTMLButtonElement).disabled).toBe(true);
  await userEvent.type(dialog.getByLabelText('answer'), '   ');
  expect((dialog.getByRole('button', { name: 'Answer' }) as HTMLButtonElement).disabled).toBe(true);
  await userEvent.type(dialog.getByLabelText('comment'), 'Needs clarification');
  await userEvent.click(dialog.getByRole('button', { name: 'Reject' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(backend.find('POST', '/api/v1/approvals/question/reject')[0].body).toEqual({ answer: '   ', comment: 'Needs clarification' });
});
it('keeps approvals accessible when application reviews fail and recovers that source', async () => {
  let failed = true;
  fakeApi({ ...routes(), 'GET /api/v1/reviews': () => failed ? apiError(503, 'Preview store unavailable', 'unavailable') : [review] });
  render(view());
  expect(await screen.findByText('Application reviews: Preview store unavailable')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Read repository old' })).toBeTruthy();
  failed = false;
  await userEvent.click(screen.getByRole('button', { name: 'Refresh approvals' }));
  expect(await screen.findByRole('button', { name: 'Check application' })).toBeTruthy();
  expect(screen.queryByText('Application reviews: Preview store unavailable')).toBeNull();
});
