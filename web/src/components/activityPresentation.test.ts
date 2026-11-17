import { expect, it } from 'vitest';
import type { Execution, MessageRecord } from '../api/types';
import { belongsToExecution, executionProgress } from './activityPresentation';
const execution = { id: 'e', graphId: 'g', inputIds: ['input'], outputIds: ['output'] } as Execution;
const message = (id: string, graphId = 'g', metadata = {}) => ({ graphId, message: { id, metadata } }) as MessageRecord;
it('links only explicit input, output and emitting execution identities within a graph', () => {
  expect(belongsToExecution(message('input'), execution)).toBe(true);
  expect(belongsToExecution(message('output'), execution)).toBe(true);
  expect(belongsToExecution(message('other', 'g', {execution_id:'e'}), execution)).toBe(true);
  expect(belongsToExecution(message('other'), execution)).toBe(false);
  expect(belongsToExecution(message('input', 'another'), execution)).toBe(false);
  expect(belongsToExecution(message('input'), { ...execution, inputIds: undefined, outputIds: undefined })).toBe(false);
});
it('distinguishes waiting, failure and terminal outcomes without fabricating progress', () => {
  const labels = ['pending', 'running', 'awaiting_approval', 'succeeded', 'failed', 'cancelled', 'interrupted'].map(status => executionProgress({ ...execution, status } as Execution));
  expect(new Set(labels).size).toBe(7);
  expect(labels).toContain('Waiting for approval');
  expect(executionProgress({ ...execution, status: 'invalid' } as unknown as Execution)).toBe('Unknown state');
});
