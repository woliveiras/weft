import type {Execution} from '../api/types';

export const taskSections = {
  overview: 'Overview', changes: 'Changes & checks', activity: 'Activity',
  request: 'Request', settings: 'Settings & access',
} as const;
export type TaskSection = keyof typeof taskSections;

export function taskSection(hash: string, taskId: string): TaskSection {
  const [path, query] = hash.split('?');
  if (path !== `#/tasks/${encodeURIComponent(taskId)}`) return 'overview';
  const section = new URLSearchParams(query).get('tab') ?? '';
  return Object.hasOwn(taskSections, section) ? section as TaskSection : 'overview';
}

export function taskSectionLink(taskId: string, section: TaskSection): string {
  return `#/tasks/${encodeURIComponent(taskId)}${section === 'overview' ? '' : `?tab=${section}`}`;
}

/** Execution timestamps, never the draft creation/update time, describe a run. */
export function taskExecutionWindow(executions: Execution[], graphId?: string) {
  const owned = executions.filter(e => e.graphId === graphId && Number.isFinite(Date.parse(e.startedAt)));
  if (!owned.length) return undefined;
  const start = Math.min(...owned.map(e => Date.parse(e.startedAt)));
  const finished = owned.every(e => ['succeeded', 'failed', 'cancelled', 'interrupted'].includes(e.status) && Number.isFinite(Date.parse(e.completedAt ?? '')));
  return {start: new Date(start).toISOString(), end: finished ? new Date(Math.max(...owned.map(e => Date.parse(e.completedAt!)))).toISOString() : undefined};
}
