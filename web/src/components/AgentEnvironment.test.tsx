import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { EnvironmentView, Graph } from '../api/types';
import { fakeApi, apiError } from '../test/fakeApi';
import { EventStreamProvider } from '../hooks/useEventStream';
import { AgentEnvironment } from './AgentEnvironment';

class FakeEventSource { onopen = null; onerror = null; addEventListener() {} close() {} }
const graph: Graph = { apiVersion: 'weft.io/v1', kind: 'Graph', metadata: { id: 'g', name: 'Project' }, spec: { nodes: [{ id: 'n', isolation: { mode: 'docker' } }], environment: { image: 'alpine' } } };
const view: EnvironmentView = { graph: 'g', state: 'building', defined: true, upToDate: false, phase: 'waiting_capacity', buildReasons: ['repository_inputs_changed'] };
const renderPanel = (g = graph) => render(<EventStreamProvider token="test"><AgentEnvironment graph={g} node={g.spec.nodes[0]} /></EventStreamProvider>);
beforeEach(() => vi.stubGlobal('EventSource', FakeEventSource));
afterEach(cleanup);
describe('agent environment integration', () => {
  it('shows a persisted image blocked by current installation policy', async () => {
    fakeApi({ 'GET /api/v1/graphs/g/environment': () => ({ ...view, state: 'ready', changeReasons: ['installation_policy'], buildReasons: [], upToDate: false }), 'GET /api/v1/graphs/g/events': () => [] });
    renderPanel();
    expect(await screen.findByText('Blocked by installation policy')).toBeTruthy();
    expect(screen.getByText(/review the definition before building/)).toBeTruthy();
    expect(screen.getByText(/Agent start is blocked/)).toBeTruthy();
    expect(screen.queryByText(/Environment is ready for commands/)).toBeNull();
  });
  it('shows build waiting, rebuild reasons, execution admission and refreshes to reused ready', async () => {
    let response = view;
    fakeApi({
      'GET /api/v1/graphs/g/environment': () => response,
      'GET /api/v1/graphs/g/events': () => [{ seq: 1, id: 'e', type: 'sandbox.capacity', time: '2026-10-16T10:00:00Z', data: { node: 'n', replica: 0, state: 'waiting' } }],
    });
    renderPanel();
    expect(await screen.findByText('Waiting for sandbox capacity')).toBeTruthy();
    expect(screen.getByText(/manifests or lockfiles changed/)).toBeTruthy();
    expect(screen.getByText(/Agent start is blocked/)).toBeTruthy();
    expect(await screen.findByText(/Replica 0: Waiting for capacity/)).toBeTruthy();
    response = { ...view, state: 'ready', phase: undefined, upToDate: true, reused: true, image: 'prepared:ready' };
    await userEvent.click(screen.getByRole('button', { name: 'Refresh environment' }));
    expect(await screen.findByText('Reused compatible prepared image and cache snapshots.')).toBeTruthy();
    expect(screen.getByText(/Environment is ready for commands/)).toBeTruthy();
    expect(screen.queryByText('Waiting for sandbox capacity')).toBeNull();
  });
  it('shows errors without claiming stale readiness and allows retry', async () => {
    let fail = false;
    fakeApi({ 'GET /api/v1/graphs/g/environment': () => fail ? apiError(503, 'storage unavailable', 'unavailable') : { ...view, state: 'ready', upToDate: true }, 'GET /api/v1/graphs/g/events': () => [] });
    renderPanel();
    await screen.findByText(/Environment is ready for commands/);
    fail = true;
    await userEvent.click(screen.getByRole('button', { name: 'Refresh environment' }));
    expect(await screen.findByText('Environment readiness could not be verified.')).toBeTruthy();
    expect(screen.queryByText(/Environment is ready for commands/)).toBeNull();
  });
  it('does not load graph environment for agents with their own image or process isolation', async () => {
    const backend = fakeApi({ 'GET /api/v1/graphs/g/events': () => [] });
    const own = { ...graph, spec: { ...graph.spec, nodes: [{ id: 'n', isolation: { mode: 'docker' as const, image: 'own:latest' } }] } };
    const rendered = renderPanel(own);
    expect(screen.getByText(/own:latest/)).toBeTruthy();
    await screen.findByText('No recent admission transitions recorded for this agent.');
    expect(backend.calls.some(call => call.path.endsWith('/environment'))).toBe(false);
    rendered.unmount();
    renderPanel({ ...graph, spec: { ...graph.spec, nodes: [{ id: 'n' }] } });
    expect(screen.getByText(/process isolation inside/)).toBeTruthy();
  });
});
