import {expect, it} from 'vitest';
import type {TaskDraft} from '../api/tasks';
import {ownsTaskExecution, taskOwnerId} from './TaskExecutionPage';

it('recognizes only nonempty task execution identifiers', () => {
  expect(taskOwnerId('task-run-example')).toBe('tsk_example');
  for (const value of ['', 'workflow', 'task-run-', 'other-task-run-example']) expect(taskOwnerId(value)).toBeUndefined();
});

it('requires both persisted task identity and execution-copy membership', () => {
  const task = {id:'tsk_example', executionGraphId:'task-run-example'} as TaskDraft;
  expect(ownsTaskExecution(task, 'task-run-example')).toBe(true);
  expect(ownsTaskExecution({...task, id:'tsk_other'}, 'task-run-example')).toBe(false);
  expect(ownsTaskExecution({...task, executionGraphId:'task-run-other'}, 'task-run-example')).toBe(false);
  expect(ownsTaskExecution({...task, executionGraphId:undefined}, 'task-run-example')).toBe(false);
  expect(ownsTaskExecution(task, 'workflow')).toBe(false);
});
