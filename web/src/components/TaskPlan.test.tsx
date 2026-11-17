import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { EventStreamProvider } from '../hooks/useEventStream';
import { apiError, fakeApi } from '../test/fakeApi';
import { TaskPlan, latestPlans } from './TaskPlan';

const sources: EventSourceStub[] = [];
class EventSourceStub {
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {};
  constructor() { sources.push(this); }
  addEventListener(type: string, fn: (e: MessageEvent) => void) { (this.listeners[type] ??= []).push(fn); }
  close() {}
  emit(type: string, data: unknown) { for (const fn of this.listeners[type] ?? []) fn(new MessageEvent(type, { data: JSON.stringify(data) })); }
}
beforeEach(() => { sources.length = 0; vi.stubGlobal('EventSource', EventSourceStub); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

const plan = (id: string, seq: number, execution: string, items: unknown) => ({ id, seq, type: 'agent.plan', time: '2026-11-11T10:00:00Z', graphId: 'g', nodeId: 'worker', executionId: execution, data: { items } });

it('keeps the latest valid plan of each execution', () => {
  const plans = latestPlans([
    plan('1', 1, 'e1', [{ step: 'read', status: 'in_progress' }]),
    plan('2', 2, 'e1', [{ step: 'read', status: 'done' }, { step: 'fix', status: 'pending' }]),
    plan('3', 3, 'e1', 'malformed'),
    plan('4', 4, 'e2', [{ step: 'other', status: 'done' }]),
    { ...plan('5', 5, '', [{ step: 'no execution', status: 'done' }]), executionId: undefined },
  ]);
  expect(plans.map(p => p.executionId)).toEqual(['e2', 'e1']);
  expect(plans[1].items).toHaveLength(2);
});

it('shows recorded plans and updates them live from the event stream', async () => {
  const backend = fakeApi({ 'GET /api/v1/graphs/g/events': () => [plan('1', 1, 'e1', [{ step: 'Inspect calc.go', status: 'in_progress' }, { step: 'Run tests', status: 'pending' }])] });
  render(<EventStreamProvider token="fixture"><TaskPlan graphId="g" /></EventStreamProvider>);
  expect(await screen.findByText(/Inspect calc.go/)).toBeTruthy();
  expect(screen.getByText(/0\/2 done/)).toBeTruthy();
  expect(backend.calls[0].path).toBe('/api/v1/graphs/g/events');
  act(() => sources[0].emit('agent.plan', plan('2', 2, 'e1', [{ step: 'Inspect calc.go', status: 'done' }, { step: 'Run tests', status: 'done' }])));
  expect(await screen.findByText(/2\/2 done/)).toBeTruthy();
  act(() => sources[0].emit('agent.plan', { ...plan('3', 3, 'e9', [{ step: 'Elsewhere', status: 'done' }]), graphId: 'other' }));
  expect(screen.queryByText(/Elsewhere/)).toBeNull();
  expect(screen.getByText(/agent's claim/)).toBeTruthy();
});

it('renders nothing without plans and reports a failed load', async () => {
  fakeApi({ 'GET /api/v1/graphs/g/events': () => [] });
  const view = render(<EventStreamProvider token="fixture"><TaskPlan graphId="g" /></EventStreamProvider>);
  await act(async () => {});
  expect(view.container.textContent).toBe('');
  cleanup();
  fakeApi({ 'GET /api/v1/graphs/g/events': () => apiError(503, 'events unavailable', 'unavailable') });
  render(<EventStreamProvider token="fixture"><TaskPlan graphId="g" /></EventStreamProvider>);
  expect(await screen.findByText(/Plans unavailable: events unavailable/)).toBeTruthy();
});
