import type { ActivityGroup, ActivityQuery, ActivityReplica, ActivityStatusFilter, ActivityView } from '../api/activity';

/** Windows offered by the page; the API accepts 5m to 7d. */
export const activityWindows = ['1h', '6h', '24h', '7d'] as const;
export const statusFilters: { value: ActivityStatusFilter; label: string }[] = [
  { value: '', label: 'All statuses' },
  { value: 'active', label: 'In progress' },
  { value: 'succeeded', label: 'Succeeded' },
  { value: 'failed', label: 'Failed or interrupted' },
  { value: 'cancelled', label: 'Cancelled' },
];

/** Which part of the page a counter focuses; "" shows everything. */
export type ActivityFocus = '' | 'running' | 'needs' | 'history';

export interface ActivityRouteState {
  window: string;
  status: ActivityStatusFilter;
  workflow: string;
  agent: string;
  focus: ActivityFocus;
}

const focuses: ActivityFocus[] = ['', 'running', 'needs', 'history'];

export function parseActivityState(query: string | undefined): ActivityRouteState {
  const q = new URLSearchParams(query ?? '');
  const window = q.get('window') ?? '';
  const status = q.get('status') ?? '';
  const focus = q.get('focus') ?? '';
  return {
    window: (activityWindows as readonly string[]).includes(window) ? window : '24h',
    status: statusFilters.some(s => s.value === status) ? status as ActivityStatusFilter : '',
    workflow: q.get('workflow') ?? '',
    agent: q.get('agent') ?? '',
    focus: focuses.includes(focus as ActivityFocus) ? focus as ActivityFocus : '',
  };
}

export function activityHash(s: Partial<ActivityRouteState>): string {
  const q = new URLSearchParams();
  if (s.window && s.window !== '24h') q.set('window', s.window);
  if (s.status) q.set('status', s.status);
  if (s.workflow) q.set('workflow', s.workflow);
  if (s.agent) q.set('agent', s.agent);
  if (s.focus) q.set('focus', s.focus);
  const qs = q.toString();
  return qs ? `#/activity?${qs}` : '#/activity';
}

export function activityQuery(s: ActivityRouteState): ActivityQuery {
  return { window: s.window, status: s.status || undefined, workflow: s.workflow || undefined, agent: s.agent || undefined };
}

const needLabels: Record<string, string> = {
  tool: 'Tool approval',
  question: 'Agent question',
  message: 'Hand-off approval',
  turn: 'Turn approval',
  delegation: 'Delegation approval',
  environment: 'Environment approval',
  review: 'Human review',
};

export function needLabel(kind: string): string {
  return needLabels[kind] ?? kind;
}

const phaseLabels: Record<string, string> = {
  waiting_for_capacity: 'Waiting for capacity',
  awaiting_approval: 'Awaiting approval',
  awaiting_review: 'Awaiting human review',
  waiting_for_message: 'Waiting for a message',
  running: 'Running',
  idle: 'Idle',
  starting: 'Starting',
  paused: 'Paused',
  stopping: 'Stopping',
};

export function phaseLabel(phase: string): string {
  return phaseLabels[phase] ?? phase.replace(/_/g, ' ');
}

export function phaseTone(phase: string): string | undefined {
  if (phase === 'running') return 'ok';
  if (phase === 'awaiting_approval' || phase === 'awaiting_review' || phase === 'waiting_for_capacity') return 'warn';
  return undefined;
}

export function groupTitle(g: Pick<ActivityGroup, 'graphId' | 'graphName' | 'task' | 'workflow'>): string {
  const name = g.graphName || g.graphId;
  return g.task ? `Task ${g.task} · ${name}` : name;
}

export function nodeLink(graphId: string, nodeId: string): string {
  return `#/g/${encodeURIComponent(graphId)}/canvas?sel=${encodeURIComponent(`node:${nodeId}`)}`;
}

export function taskLink(task: string): string {
  return `#/tasks/${encodeURIComponent(task)}`;
}

/** Elapsed time of a turn, advanced locally since the response was generated. */
export function elapsedMs(r: Pick<ActivityReplica, 'elapsedMs' | 'startedAt'>, fetchedAt: number, now: number): number | undefined {
  if (!r.startedAt) return undefined;
  return (r.elapsedMs ?? 0) + Math.max(0, now - fetchedAt);
}

/** A counter: unknown when its section failed, a lower bound when truncated. */
export function counterText(value: number | null | undefined, truncated = false): string {
  if (value === null || value === undefined) return '—';
  return truncated ? `${value}+` : String(value);
}

export function sectionErrors(view: ActivityView | undefined, ...sections: string[]) {
  return (view?.errors ?? []).filter(e => sections.includes(e.section));
}

export function tokens(u: { inputTokens: number; outputTokens: number } | undefined): string {
  return `${(u?.inputTokens ?? 0).toLocaleString('en')} in / ${(u?.outputTokens ?? 0).toLocaleString('en')} out`;
}
