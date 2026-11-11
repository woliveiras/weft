import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { ActivityView } from '../api/activity';
import { EventStreamProvider } from '../hooks/useEventStream';
import { apiError, fakeApi } from '../test/fakeApi';
import { ActivityPage } from './ActivityPage';
import { parseActivityState, type ActivityRouteState } from './activityView';

type Listener = (e: { data: string }) => void;
const streams: { listeners: Record<string, Listener[]> }[] = [];
class EventSourceStub {
  listeners: Record<string, Listener[]> = {};
  onopen?: () => void;
  onerror?: () => void;
  constructor() { streams.push(this); }
  addEventListener(type: string, fn: Listener) { (this.listeners[type] ??= []).push(fn); }
  close() {}
}
function emit(type: string, data: Record<string, unknown>) {
  act(() => { for (const s of streams) for (const fn of s.listeners[type] ?? []) fn({ data: JSON.stringify({ seq: Date.now(), type, ...data }) }); });
}
beforeEach(() => { streams.length = 0; vi.stubGlobal('EventSource', EventSourceStub); vi.stubGlobal('requestAnimationFrame', (f: FrameRequestCallback) => setTimeout(() => f(0), 0)); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const usage = { inputTokens: 1200, outputTokens: 300 };
function fixture(overrides: Partial<ActivityView> = {}): ActivityView {
  return {
    window: '24h', since: '2026-11-10T12:00:00Z', generatedAt: '2026-11-11T12:00:00Z', currency: 'USD',
    counters: { running: 2, waiting: 2, failed: 1, finished: 3, cancelled: 0 },
    needsYou: [
      { kind: 'message', id: 'a-old', title: 'Release to shipper', graphId: 'wf', graphName: 'Software factory', nodeId: 'shipper', replica: 0, executionId: 'x0', createdAt: '2026-11-10T13:00:00Z' },
      { kind: 'review', id: 'r1', title: 'Check the preview', graphId: 'task-run-1', task: 'task-1', replica: 0, createdAt: '2026-11-11T11:00:00Z', expiresAt: '2026-11-11T13:00:00Z' },
    ],
    running: [{ graphId: 'wf', graphName: 'Software factory', workflow: 'wf', state: 'running', replicas: [
      { nodeId: 'coder', replica: 0, state: 'running', phase: 'running', executionId: 'x1', startedAt: '2026-11-11T11:58:00Z', elapsedMs: 120000, planStep: 'Edit the handler', usage, toolCalls: 4, costMicros: 12500 },
      { nodeId: 'coder', replica: 1, state: 'running', phase: 'waiting_for_capacity', detail: 'waiting for sandbox capacity to resume', usage: { inputTokens: 0, outputTokens: 0 }, toolCalls: 0, costMicros: null },
    ] }],
    history: [
      { id: 'x9', graphId: 'wf', graphName: 'Software factory', workflow: 'wf', nodeId: 'tester', replica: 0, status: 'failed', startedAt: '2026-11-11T10:00:00Z', completedAt: '2026-11-11T10:00:05Z', durationMs: 5000, toolCalls: 3, error: 'go test failed', usage, costMicros: null },
      { id: 'x8', graphId: 'task-run-1', task: 'task-1', workflow: 'wf', nodeId: 'coder', replica: 0, status: 'succeeded', startedAt: '2026-11-11T09:00:00Z', durationMs: 65000, toolCalls: 7, usage, costMicros: 2500000 },
    ],
    options: { workflows: [{ id: 'wf', name: 'Software factory' }], agents: ['coder', 'tester'] },
    limits: { history: 100, needsYou: 100, executions: 1000, activeGraphs: 50, replicas: 200, costEvents: 5000 },
    truncated: { needsYou: false, running: false, executions: false, history: false, cost: false },
    errors: [{ section: 'cost', code: 'failed', message: 'Cost is unavailable; tokens are still shown.' }],
    ...overrides,
  };
}
function view(state: ActivityRouteState = parseActivityState(''), onChange = vi.fn()) {
  return <EventStreamProvider token="fixture"><ActivityPage state={state} onChange={onChange} /></EventStreamProvider>;
}

it('shows counters, waiting items oldest first, running agents and history with links to the detail views', async () => {
  const backend = fakeApi({ 'GET /api/v1/activity': () => fixture() });
  render(view());
  const needs = await screen.findByRole('region', { name: 'Needs you' });
  const items = within(needs).getAllByRole('listitem');
  expect(items[0].textContent).toContain('Hand-off approval');
  expect(items[0].textContent).toContain('Release to shipper');
  expect(within(items[0]).getByRole('link', { name: 'Decide in Approvals' }).getAttribute('href')).toBe('#/approvals');
  expect(within(items[0]).getByRole('link', { name: 'Run details' }).getAttribute('href')).toBe('#/g/wf/runs?execution=x0&section=result');
  expect(within(items[1]).getByRole('link', { name: 'Open the review' })).toBeTruthy();
  expect(within(items[1]).getByRole('link', { name: 'Open task' }).getAttribute('href')).toBe('#/tasks/task-1');

  const counters = screen.getByRole('region', { name: 'Activity counters' });
  expect(within(counters).getByRole('button', { name: /2\s*Agents running/ })).toBeTruthy();
  expect(within(counters).getByRole('button', { name: /3\s*Finished/ })).toBeTruthy();

  const group = within(screen.getByRole('region', { name: 'Running now' })).getByRole('article', { name: 'Software factory' });
  const rows = within(group).getAllByRole('row');
  expect(rows[1].textContent).toContain('Edit the handler');
  expect(rows[1].textContent).toContain('1,200 in / 300 out');
  expect(rows[1].textContent).toContain('$0.01');
  expect(within(rows[1]).getByRole('link', { name: 'coder' }).getAttribute('href')).toBe('#/g/wf/canvas?sel=node%3Acoder');
  expect(within(rows[1]).getByRole('link', { name: 'Run details' }).getAttribute('href')).toBe('#/g/wf/runs?execution=x1&section=result');
  expect(rows[2].textContent).toContain('Waiting for capacity');
  expect(within(rows[2]).getByRole('link', { name: 'Agent panel' })).toBeTruthy();

  const history = screen.getByRole('region', { name: 'Recent history' });
  expect(within(history).getByRole('link', { name: 'x9' }).getAttribute('href')).toBe('#/g/wf/runs?execution=x9&section=result');
  expect(within(history).getByText('go test failed')).toBeTruthy();
  expect(within(history).getByRole('link', { name: 'Task task-1' })).toBeTruthy();
  expect(within(history).getByText('$2.50')).toBeTruthy();
  // Cost that could not be read is shown as unknown, with the reason, never as zero.
  expect(within(history).getByText('Cost is unavailable; tokens are still shown.')).toBeTruthy();
  expect(within(history).getAllByTitle('Cost could not be read')).toHaveLength(1);
  expect(backend.find('GET', '/api/v1/activity')).toHaveLength(1);
});

it('filters the page from counters and history controls through the route', async () => {
  fakeApi({ 'GET /api/v1/activity': () => fixture() });
  const onChange = vi.fn();
  const { rerender } = render(view(parseActivityState(''), onChange));
  await userEvent.click(await screen.findByRole('button', { name: /1\s*Failed/ }));
  expect(onChange).toHaveBeenLastCalledWith({ window: '24h', status: 'failed', workflow: '', agent: '', focus: 'history', tab: '' });
  await userEvent.click(screen.getByRole('button', { name: /2\s*Waiting for a person/ }));
  expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ focus: 'needs', status: '' }));
  await userEvent.selectOptions(screen.getByLabelText('Agent'), 'tester');
  expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ agent: 'tester' }));
  await userEvent.selectOptions(screen.getByLabelText('Window'), '7d');
  expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ window: '7d' }));

  rerender(view(parseActivityState('status=failed&focus=history'), onChange));
  expect(await screen.findByText(/Showing only recent history/)).toBeTruthy();
  expect(screen.queryByRole('region', { name: 'Needs you' })).toBeNull();
  expect(screen.queryByRole('region', { name: 'Running now' })).toBeNull();
  expect(screen.getByRole('button', { name: /1\s*Failed/ }).getAttribute('aria-pressed')).toBe('true');
  await userEvent.click(screen.getByRole('button', { name: /1\s*Failed/ }));
  expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ focus: '', status: '' }));
});

it('sends filters to the API and refetches, throttled, on live events', async () => {
  let count = 0;
  const backend = fakeApi({ 'GET /api/v1/activity': () => { count++; return fixture({ counters: { ...fixture().counters, finished: 3 + count } }); } });
  render(view(parseActivityState('window=7d&status=failed&workflow=wf&agent=coder')));
  await screen.findByRole('button', { name: /4\s*Finished/ });
  const first = backend.calls[0];
  expect(first.path).toBe('/api/v1/activity');
  expect(String(backend.fetch.mock.calls[0][0])).toBe('/api/v1/activity?window=7d&status=failed&workflow=wf&agent=coder');
  emit('execution.completed', { graphId: 'wf' });
  emit('replica.state_changed', { graphId: 'wf' });
  emit('agent.plan', { graphId: 'wf' });
  emit('message.published', { graphId: 'wf' });
  await waitFor(() => expect(backend.find('GET', '/api/v1/activity')).toHaveLength(2), { timeout: 4000 });
  expect(await screen.findByRole('button', { name: /5\s*Finished/ })).toBeTruthy();
});

it('reports failed sections explicitly instead of showing them as empty', async () => {
  fakeApi({ 'GET /api/v1/activity': () => fixture({
    counters: { running: null, waiting: 1, failed: null, finished: null, cancelled: null },
    running: null, history: null, options: null,
    needsYou: [fixture().needsYou![0]],
    errors: [
      { section: 'running', code: 'failed', message: 'Running agents could not be loaded.' },
      { section: 'history', code: 'failed', message: 'Recent executions could not be loaded.' },
      { section: 'reviews', code: 'forbidden', message: 'Your role cannot see human reviews; they are not counted.' },
    ],
  }) });
  render(view());
  expect(await screen.findByText('Running agents could not be loaded.')).toBeTruthy();
  expect(screen.getByText('Recent executions could not be loaded.')).toBeTruthy();
  expect(screen.getByRole('note').textContent).toContain('Your role cannot see human reviews');
  expect(screen.queryByText(/No workflow or task is running/)).toBeNull();
  expect(screen.queryByText(/No executions started/)).toBeNull();
  expect(screen.getByRole('button', { name: /—\s*Agents running/ })).toBeTruthy();
});

it('shows truncation, empty states and request failures with retry', async () => {
  let fail = true;
  const backend = fakeApi({ 'GET /api/v1/activity': () => fail ? apiError(503, 'Store unavailable', 'unavailable') : fixture({
    needsYou: [], running: [], history: [], errors: [],
    counters: { running: 0, waiting: 0, failed: 1000, finished: 0, cancelled: 0 },
    truncated: { needsYou: false, running: false, executions: true, history: true, cost: true },
  }) });
  render(view());
  expect(await screen.findByText('Store unavailable')).toBeTruthy();
  expect(screen.queryByRole('region', { name: 'Activity counters' })).toBeNull();
  fail = false;
  await userEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  expect(await screen.findByText('Nothing is waiting for a person.')).toBeTruthy();
  expect(screen.getByText('No workflow or task is running.')).toBeTruthy();
  expect(screen.getByRole('button', { name: /1000\+\s*Failed/ })).toBeTruthy();
  expect(screen.getByText(/More than 1000 executions started in this window/)).toBeTruthy();
  expect(screen.getByText(/Cost reads the newest 5000 cost records/)).toBeTruthy();
  // A later failure keeps the last data but says it is stale.
  fail = true;
  await userEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  expect(await screen.findByText(/Could not refresh activity: Store unavailable/)).toBeTruthy();
  expect(screen.getByText('Nothing is waiting for a person.')).toBeTruthy();
  expect(backend.find('GET', '/api/v1/activity')).toHaveLength(3);
});
