import { expect, it } from 'vitest';
import type { GraphSummary } from '../api/types';
import { listedWorkflows } from './workflowPresentation';
const workflow = (id: string, updatedAt: string, state = 'stopped', extra = {}): GraphSummary => ({ graph: { apiVersion: 'weft.io/v1', kind: 'Graph', metadata: { id, name: id, description: 'Reviews code', ...extra }, spec: { nodes: [] } }, record: { id, desiredState: 'stopped', version: 1, state, updatedAt } }) as GraphSummary;
it('sorts newest first with stable ties, missing dates last, and excludes execution copies without mutating input', () => {
  const input = [workflow('b', '2026-11-19T09:00:00Z'), workflow('a', '2026-11-19T09:00:00Z'), workflow('new', '2026-11-19T10:00:00Z'), workflow('old', ''), workflow('task-run-1', '2026-11-19'), workflow('labelled', '2026-11-19', 'running', { labels: { 'weft.task': 'task' } })];
  expect(listedWorkflows(input).map(item => item.graph.metadata.id)).toEqual(['new', 'a', 'b', 'old']);
  expect(input[0].graph.metadata.id).toBe('b');
});
it('combines exact state and case-insensitive name, description or ID search and handles empty results', () => {
  const input = [workflow('a', '', 'running', { name: 'Factory' }), workflow('b', '', 'paused')];
  expect(listedWorkflows(input, ' FACTORY ', 'running')).toHaveLength(1);
  expect(listedWorkflows(input, 'reviews', 'paused')[0].graph.metadata.id).toBe('b');
  expect(listedWorkflows(input, 'a', 'paused')).toEqual([]);
  expect(listedWorkflows(input, 'unknown')).toEqual([]);
  expect(listedWorkflows([])).toEqual([]);
});
