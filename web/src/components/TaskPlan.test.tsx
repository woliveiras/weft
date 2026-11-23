import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { EventStreamProvider } from '../hooks/useEventStream';
import { apiError, fakeApi } from '../test/fakeApi';
import { TaskPlan, latestPlans, currentPlans } from './TaskPlan';

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

it('orders out-of-order snapshots by sequence and separates older attempts of the same replica', () => {
  const plans=latestPlans([
    plan('new',9,'e2',[{step:'Newest',status:'pending'}]),
    plan('old',1,'e1',[{step:'Earlier attempt',status:'done'}]),
    plan('stale',3,'e2',[{step:'Stale update',status:'pending'}]),
    {...plan('other-replica',8,'e3',[{step:'Replica one',status:'in_progress'}]),replica:1},
  ]);
  expect(plans[0].items[0].step).toBe('Newest');
  expect(currentPlans(plans).map(item=>item.executionId)).toEqual(['e2','e3']);
});

it('keeps live updates when a slower initial snapshot arrives and allows retry after failure', async () => {
  let resolve:(value:unknown)=>void=()=>{};
  fakeApi({'GET /api/v1/graphs/g/events':()=>new Promise(done=>{resolve=done;})});
  render(<EventStreamProvider token="fixture"><TaskPlan graphId="g"/></EventStreamProvider>);
  expect(screen.getByText('Loading agent plans…')).toBeTruthy();
  act(()=>sources[0].emit('agent.plan',plan('new',9,'e1',[{step:'Live update',status:'done'}])));
  await act(async()=>resolve([plan('old',1,'e1',[{step:'Old update',status:'pending'}])]));
  expect(screen.getByText('Live update')).toBeTruthy();
  expect(screen.queryByText('Old update')).toBeNull();
  cleanup();
  let offline=true;
  fakeApi({'GET /api/v1/graphs/g/events':()=>offline?apiError(503,'Offline','unavailable'):[plan('recovered',1,'e1',[{step:'Recovered plan',status:'pending'}])]});
  render(<EventStreamProvider token="fixture"><TaskPlan graphId="g"/></EventStreamProvider>);
  await screen.findByText('Plans unavailable: Offline');offline=false;
  const {fireEvent}=await import('@testing-library/react');
  fireEvent.click(screen.getByRole('button',{name:'Refresh plans'}));
  expect(await screen.findByText('Recovered plan')).toBeTruthy();
});

it('shows recorded plans and updates them live from the event stream', async () => {
  const backend = fakeApi({ 'GET /api/v1/graphs/g/events': () => [plan('1', 1, 'e1', [{ step: 'Inspect calc.go', status: 'in_progress' }, { step: 'Run tests', status: 'pending' }])] });
  render(<EventStreamProvider token="fixture"><TaskPlan graphId="g" /></EventStreamProvider>);
  expect(await screen.findByText('Inspect calc.go')).toBeTruthy();
  expect(screen.getByText(/0\/2 done/)).toBeTruthy();
  expect(backend.calls[0].path).toBe('/api/v1/graphs/g/events');
  act(() => sources[0].emit('agent.plan', plan('2', 2, 'e1', [{ step: 'Inspect calc.go', status: 'done' }, { step: 'Run tests', status: 'done' }])));
  expect(await screen.findByText(/2\/2 done/)).toBeTruthy();
  act(() => sources[0].emit('agent.plan', { ...plan('3', 3, 'e9', [{ step: 'Elsewhere', status: 'done' }]), graphId: 'other' }));
  expect(screen.queryByText(/Elsewhere/)).toBeNull();
  expect(screen.getByText(/agent's claim/)).toBeTruthy();
});

it('explains the empty state and reports a failed load without hiding the plans entry', async () => {
  fakeApi({ 'GET /api/v1/graphs/g/events': () => [] });
  const view = render(<EventStreamProvider token="fixture"><TaskPlan graphId="g" /></EventStreamProvider>);
  await act(async () => {});
  expect(view.container.textContent).toContain('No agent plan recorded yet.');
  expect(screen.getByRole('heading',{name:'Agent plans'})).toBeTruthy();
  cleanup();
  fakeApi({ 'GET /api/v1/graphs/g/events': () => apiError(503, 'events unavailable', 'unavailable') });
  render(<EventStreamProvider token="fixture"><TaskPlan graphId="g" /></EventStreamProvider>);
  expect(await screen.findByText(/Plans unavailable: events unavailable/)).toBeTruthy();
});

it('does not retain another graph plan while its replacement snapshot is unavailable', async () => {
  fakeApi({'GET /api/v1/graphs/g/events':()=>[plan('a',1,'e1',[{step:'Previous graph plan',status:'done'}])],
    'GET /api/v1/graphs/other/events':()=>apiError(503,'Other graph offline','unavailable')});
  const view=render(<TaskPlan graphId="g"/>);
  await screen.findByText('Previous graph plan');
  view.rerender(<TaskPlan graphId="other"/>);
  expect(await screen.findByText('Plans unavailable: Other graph offline')).toBeTruthy();
  expect(screen.queryByText('Previous graph plan')).toBeNull();
});
