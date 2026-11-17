import { describe, expect, it } from 'vitest';
import type { EnvironmentView, Graph, WeftEvent } from '../api/types';
import { agentEnvironment, capacityTransitions, environmentPhase, reasonLabel } from './environmentPresentation';
const graph: Graph = { apiVersion: 'weft.io/v1', kind: 'Graph', metadata: { id: 'g', name: 'G' }, spec: { nodes: [], environment: { image: 'alpine' }, defaults: { isolation: { mode: 'docker' } } } };
describe('environment presentation', () => {
  it('honors inherited isolation and explicit image overrides', () => {
    expect(agentEnvironment(graph, { id: 'n' })).toBe('graph');
    expect(agentEnvironment(graph, { id: 'n', isolation: { image: 'own' } })).toBe('image');
    expect(agentEnvironment(graph, { id: 'n', isolation: { mode: 'process' } })).toBe('process');
    expect(agentEnvironment({ ...graph, spec: { ...graph.spec, environment: undefined } }, { id: 'n' })).toBe('image');
  });
  it('never presents a terminal environment as waiting, and handles older servers', () => {
    const view: EnvironmentView = { graph: 'g', defined: true, state: 'building', phase: 'waiting_capacity', upToDate: false };
    expect(environmentPhase(view)).toBe('Waiting for sandbox capacity');
    expect(environmentPhase({ ...view, state: 'failed' })).toBe('Build failed');
    expect(environmentPhase({ ...view, state: 'ready' })).toBe('Outdated');
    expect(environmentPhase({ ...view, phase: undefined })).toBe('Building…');
    expect(reasonLabel('repository_inputs_changed')).toContain('lockfiles');
    expect(reasonLabel('installation_policy')).toContain('review the definition');
    expect(environmentPhase({ ...view, state: 'ready', changeReasons: ['installation_policy'] })).toBe('Blocked by installation policy');
    expect(reasonLabel('future')).toBe('Other reason: future');
  });
  it('keeps the newest valid transition per replica and excludes builds and other agents', () => {
    const event = (seq: number, data: unknown): WeftEvent => ({ seq, id: `${seq}`, time: 'now', type: 'sandbox.capacity', data });
    const events = [event(3, { node: 'n', replica: 0, state: 'admitted' }), event(1, { node: 'n', replica: 0, state: 'waiting' }), event(4, { node: 'other', replica: 0, state: 'waiting' }), event(5, { phase: 'environment', node: 'n', replica: 0, state: 'waiting' }), event(6, null), event(7, { node: 'n', replica: -1, state: 'waiting' })];
    expect(capacityTransitions(events, 'n')).toEqual([{ replica: 0, label: 'Capacity granted', time: 'now', seq: 3 }]);
  });
});
