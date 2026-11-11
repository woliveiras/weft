import { describe, expect, it } from 'vitest';
import { activityHash, activityQuery, counterText, elapsedMs, groupTitle, needLabel, nodeLink, parseActivityState, phaseLabel, phaseTone, sectionErrors } from './activityView';
import type { ActivityView } from '../api/activity';

describe('activity route state', () => {
  it('round-trips filters and drops unknown values', () => {
    const state = parseActivityState('window=7d&status=failed&workflow=wf&agent=coder&focus=history');
    expect(state).toEqual({ window: '7d', status: 'failed', workflow: 'wf', agent: 'coder', focus: 'history' });
    expect(activityHash(state)).toBe('#/activity?window=7d&status=failed&workflow=wf&agent=coder&focus=history');
    expect(parseActivityState('window=30d&status=bogus&focus=everything')).toEqual({ window: '24h', status: '', workflow: '', agent: '', focus: '' });
    expect(activityHash(parseActivityState(undefined))).toBe('#/activity');
  });
  it('sends only set filters to the API', () => {
    expect(activityQuery(parseActivityState('agent=a'))).toEqual({ window: '24h', status: undefined, workflow: undefined, agent: 'a' });
  });
});

describe('activity presentation', () => {
  it('names waiting items and replica phases', () => {
    expect(needLabel('message')).toBe('Hand-off approval');
    expect(needLabel('question')).toBe('Agent question');
    expect(needLabel('review')).toBe('Human review');
    expect(needLabel('future_kind')).toBe('future_kind');
    expect(phaseLabel('waiting_for_capacity')).toBe('Waiting for capacity');
    expect(phaseLabel('some_new_phase')).toBe('some new phase');
    expect(phaseTone('awaiting_approval')).toBe('warn');
    expect(phaseTone('idle')).toBeUndefined();
  });
  it('titles task runs by task and links node panels safely', () => {
    expect(groupTitle({ graphId: 'task-run-1', graphName: 'Fix login', task: 't1', workflow: 'wf' })).toBe('Task t1 · Fix login');
    expect(groupTitle({ graphId: 'wf', workflow: 'wf' })).toBe('wf');
    expect(nodeLink('a b', 'c/d')).toBe('#/g/a%20b/canvas?sel=node%3Ac%2Fd');
  });
  it('marks unknown and truncated counters', () => {
    expect(counterText(null)).toBe('—');
    expect(counterText(undefined)).toBe('—');
    expect(counterText(0)).toBe('0');
    expect(counterText(1000, true)).toBe('1000+');
  });
  it('advances elapsed time from the response only for turns in progress', () => {
    expect(elapsedMs({ startedAt: '2026-11-11T10:00:00Z', elapsedMs: 5000 }, 1000, 3500)).toBe(7500);
    expect(elapsedMs({ startedAt: '2026-11-11T10:00:00Z', elapsedMs: 5000 }, 1000, 500)).toBe(5000);
    expect(elapsedMs({ elapsedMs: 5000 }, 0, 10)).toBeUndefined();
  });
  it('selects section errors', () => {
    const view = { errors: [{ section: 'history', code: 'failed', message: 'x' }, { section: 'reviews', code: 'forbidden', message: 'y' }] } as ActivityView;
    expect(sectionErrors(view, 'reviews', 'needsYou').map(e => e.code)).toEqual(['forbidden']);
    expect(sectionErrors(undefined, 'history')).toEqual([]);
  });
});
