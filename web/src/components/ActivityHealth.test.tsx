import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { HealthRow, HealthView } from '../api/activity';
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
function row(over: Partial<HealthRow>): HealthRow {
  return { key: 'wf/a', workflow: 'wf', workflowName: 'Software factory', nodeId: 'a', runs: 1, finished: 1, succeeded: 1, failed: 0, cancelled: 0,
    failureRate: 0, p50Ms: 1000, p90Ms: 1000, usage, costMicros: 0, topErrors: [], otherErrors: 0, ...over };
}
const tester = row({
  key: 'wf/tester', nodeId: 'tester', runs: 4, finished: 4, succeeded: 1, failed: 3, failureRate: 0.75, p50Ms: 20000, p90Ms: 40000, costMicros: 100,
  topErrors: [
    { message: 'read <path>: no such file', count: 2, lastAt: '2026-11-11T08:00:20Z', lastRun: { id: 't2', graphId: 'task-run-1', status: 'failed', startedAt: '2026-11-11T08:00:00Z' } },
    { message: '(no error message)', count: 1, lastAt: '2026-11-11T09:00:30Z', lastRun: { id: 't3', graphId: 'wf', status: 'interrupted', startedAt: '2026-11-11T09:00:00Z' } },
  ],
  otherErrors: 4,
  lastRun: { id: 't4', graphId: 'wf', status: 'succeeded', startedAt: '2026-11-11T10:00:00Z' },
  lastFailure: { id: 't3', graphId: 'wf', status: 'interrupted', startedAt: '2026-11-11T09:00:00Z' },
});
const coder = row({ key: 'wf/coder', nodeId: 'coder', runs: 11, finished: 10, succeeded: 10, p50Ms: 300000, p90Ms: 540000, costMicros: 2500000,
  lastRun: { id: 'c-run', graphId: 'task-run-1', status: 'running', startedAt: '2026-11-11T11:59:00Z' } });
function fixture(overrides: Partial<HealthView> = {}): HealthView {
  return {
    window: '24h', since: '2026-11-10T12:00:00Z', generatedAt: '2026-11-11T12:00:00Z', currency: 'USD',
    agents: [coder, tester],
    workflows: [row({ key: 'wf', nodeId: undefined, runs: 15, finished: 14, failed: 3, failureRate: 3 / 14, p90Ms: 540000, costMicros: 2500100 })],
    rankings: {
      failsMost: { agents: ['wf/tester'], workflows: ['wf'] },
      slowest: { agents: ['wf/coder', 'wf/tester'], workflows: ['wf'] },
      mostExpensive: { agents: ['wf/coder', 'wf/tester'], workflows: ['wf'] },
    },
    limits: { executions: 1000, costEvents: 5000, rows: 200, ranking: 10, errors: 5 },
    truncated: { executions: false, cost: false, agents: false, workflows: false },
    errors: [],
    ...overrides,
  };
}
function view(state: ActivityRouteState = parseActivityState('tab=health'), onChange = vi.fn()) {
  return <EventStreamProvider token="fixture"><ActivityPage state={state} onChange={onChange} /></EventStreamProvider>;
}

it('ranks agents and lists their health with links to filtered history and run details', async () => {
  const backend = fakeApi({ 'GET /api/v1/activity/health': () => fixture() });
  render(view());
  const fails = await screen.findByRole('article', { name: 'Fails most' });
  const failing = within(fails).getByRole('link', { name: 'tester · Software factory' });
  expect(failing.getAttribute('href')).toBe('#/activity?status=failed&workflow=wf&agent=tester&focus=history');
  expect(fails.textContent).toContain('75% · 3 of 4 failed');
  const slowest = within(screen.getByRole('article', { name: 'Slowest' })).getAllByRole('listitem');
  expect(slowest[0].textContent).toContain('coder · Software factory');
  expect(slowest[0].textContent).toContain('p90 9m 0s');
  expect(within(screen.getByRole('article', { name: 'Most expensive' })).getAllByRole('listitem')[0].textContent).toContain('$2.50');

  const table = screen.getByRole('region', { name: 'Agent health' });
  const rows = within(table).getAllByRole('row');
  expect(rows[1].textContent).toContain('coder');
  const t = rows[2];
  expect(t.textContent).toContain('75%');
  expect(t.textContent).toContain('20.0s');
  expect(t.textContent).toContain('40.0s');
  expect(t.textContent).toContain('1,200 in / 300 out');
  expect(t.textContent).toContain('read <path>: no such file');
  expect(t.textContent).toContain('×2');
  expect(t.textContent).toContain('+4 other');
  expect(within(t).getByRole('link', { name: 'Latest run with: read <path>: no such file' }).getAttribute('href')).toBe('#/g/task-run-1/runs?execution=t2&section=result');
  expect(within(t).getByRole('link', { name: 'History' }).getAttribute('href')).toBe('#/activity?workflow=wf&agent=tester&focus=history');
  expect(within(t).getByRole('link', { name: 'Last run' }).getAttribute('href')).toBe('#/g/wf/runs?execution=t4&section=result');
  expect(within(t).getByRole('link', { name: 'Last failure' }).getAttribute('href')).toBe('#/g/wf/runs?execution=t3&section=result');
  expect(String(backend.fetch.mock.calls[0][0])).toBe('/api/v1/activity/health?window=24h');
  expect(backend.find('GET', '/api/v1/activity')).toHaveLength(0);
});

it('switches to workflows, changes the window through the route and refetches on finished executions', async () => {
  const backend = fakeApi({ 'GET /api/v1/activity/health': () => fixture() });
  const onChange = vi.fn();
  render(view(parseActivityState('tab=health&window=7d'), onChange));
  await screen.findByRole('region', { name: 'Agent health' });
  expect(String(backend.fetch.mock.calls[0][0])).toBe('/api/v1/activity/health?window=7d');
  await userEvent.click(screen.getByRole('radio', { name: 'Workflows' }));
  const table = screen.getByRole('region', { name: 'Workflow health' });
  expect(within(table).getAllByRole('row')[1].textContent).toContain('21%');
  expect(within(screen.getByRole('article', { name: 'Fails most' })).getByRole('link', { name: 'Software factory' }).getAttribute('href'))
    .toBe('#/activity?window=7d&status=failed&workflow=wf&focus=history');
  await userEvent.selectOptions(screen.getByLabelText('Window'), '24h');
  expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ window: '24h', tab: 'health' }));
  expect([...(screen.getByLabelText('Window') as HTMLSelectElement).options].map(o => o.value)).toEqual(['24h', '7d']);

  emit('execution.completed', { graphId: 'wf' });
  emit('message.published', { graphId: 'wf' });
  await waitFor(() => expect(backend.find('GET', '/api/v1/activity/health')).toHaveLength(2), { timeout: 7000 });
}, 10000);

it('moves between tabs through the route', async () => {
  fakeApi({ 'GET /api/v1/activity/health': () => fixture(), 'GET /api/v1/activity': () => apiError(503, 'not under test', 'unavailable') });
  const onChange = vi.fn();
  const { rerender } = render(view(parseActivityState('tab=health'), onChange));
  const tabs = await screen.findByRole('tablist', { name: 'Activity views' });
  expect(within(tabs).getByRole('tab', { name: 'Health' }).getAttribute('aria-selected')).toBe('true');
  await userEvent.click(within(tabs).getByRole('tab', { name: 'Overview' }));
  expect(onChange).toHaveBeenLastCalledWith(expect.objectContaining({ tab: '' }));
  rerender(view(parseActivityState('window=1h&status=failed&focus=history'), onChange));
  await userEvent.click(screen.getByRole('tab', { name: 'Health' }));
  expect(onChange).toHaveBeenLastCalledWith({ window: '24h', status: '', workflow: '', agent: '', focus: '', tab: 'health' });
});

it('reports failed sections, truncation and empty windows explicitly', async () => {
  let mode = 'failed';
  fakeApi({ 'GET /api/v1/activity/health': () => mode === 'error' ? apiError(503, 'Store unavailable', 'unavailable') : mode === 'failed' ? fixture({
    agents: null, workflows: null, rankings: null,
    errors: [
      { section: 'executions', code: 'failed', message: 'Executions could not be loaded; health is unavailable.' },
      { section: 'cost', code: 'failed', message: 'Cost is unavailable; the most expensive ranking is not shown.' },
    ],
  }) : mode === 'nocost' ? fixture({
    agents: [{ ...coder, costMicros: null }], rankings: { ...fixture().rankings!, mostExpensive: null },
    truncated: { executions: true, cost: false, agents: true, workflows: false },
    errors: [{ section: 'cost', code: 'failed', message: 'Cost is unavailable; the most expensive ranking is not shown.' }],
  }) : fixture({ agents: [], workflows: [], rankings: { failsMost: { agents: [], workflows: [] }, slowest: { agents: [], workflows: [] }, mostExpensive: { agents: [], workflows: [] } } }) });
  render(view());
  expect(await screen.findByText('Executions could not be loaded; health is unavailable.')).toBeTruthy();
  expect(screen.queryByRole('region', { name: 'Rankings' })).toBeNull();
  expect(screen.queryByText(/No executions started/)).toBeNull();

  mode = 'nocost';
  await userEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  expect(await screen.findByText('Cost could not be read.')).toBeTruthy();
  expect(screen.getByText(/More than 1000 executions started in this window; health describes the newest 1000/)).toBeTruthy();
  expect(screen.getByText(/Only the 200 busiest agents and every ranked one are listed/)).toBeTruthy();
  expect(within(screen.getByRole('region', { name: 'Agent health' })).getAllByTitle('Cost could not be read')).toHaveLength(1);

  mode = 'empty';
  await userEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  expect(await screen.findByText('No executions started in this window.')).toBeTruthy();
  expect(screen.getByText('Nothing failed in this window.')).toBeTruthy();
  expect(screen.getAllByText('No run finished in this window.')).toHaveLength(1);

  mode = 'error';
  await userEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  expect(await screen.findByText(/Could not refresh health: Store unavailable/)).toBeTruthy();
  expect(screen.getByText('No executions started in this window.')).toBeTruthy();
});
