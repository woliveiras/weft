import {expect, it} from 'vitest';
import type {Execution} from '../api/types';
import {taskExecutionWindow, taskSection, taskSectionLink} from './taskDetailPresentation';

it('keeps task section links scoped, encoded and compatible with the bare route', () => {
  expect(taskSectionLink('tsk_1', 'overview')).toBe('#/tasks/tsk_1');
  expect(taskSection(taskSectionLink('tsk_/1', 'changes'), 'tsk_/1')).toBe('changes');
  expect(taskSection('#/tasks/tsk_1?tab=activity&execution=e1', 'tsk_1')).toBe('activity');
  expect(taskSection('#/tasks/other?tab=settings', 'tsk_1')).toBe('overview');
  expect(taskSection('#/tasks/tsk_1?tab=constructor', 'tsk_1')).toBe('overview');
  expect(taskSection('#/tasks/tsk_1?tab=unknown', 'tsk_1')).toBe('overview');
});

it('computes the execution window from owned records, never draft timestamps or foreign runs', () => {
  const run = (overrides: Partial<Execution>) => ({id:'e1', graphId:'g', startedAt:'2026-11-23T12:00:00Z', completedAt:'2026-11-23T12:02:00Z', status:'succeeded', ...overrides} as Execution);
  expect(taskExecutionWindow([run({}),run({graphId:'other',startedAt:'2020-01-01T00:00:00Z'}),run({startedAt:'invalid'})],'g')).toEqual({start:'2026-11-23T12:00:00.000Z',end:'2026-11-23T12:02:00.000Z'});
  expect(taskExecutionWindow([run({status:'running',completedAt:undefined})],'g')?.end).toBeUndefined();
  expect(taskExecutionWindow([run({completedAt:undefined})],'g')?.end).toBeUndefined();
  expect(taskExecutionWindow([run({})],'other')).toBeUndefined();
});
