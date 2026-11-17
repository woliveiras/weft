import { useCallback, useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { WeftEvent } from '../api/types';
import { useOptionalEventStream } from '../hooks/useEventStream';
import { ErrorBox } from './common';

export interface PlanItem { step: string; status: string }
interface AgentPlan { executionId: string; nodeId: string; replica?: number; items: PlanItem[]; time: string }

const MARK: Record<string, string> = { done: '✓', in_progress: '▸', pending: '○' };
const LABEL: Record<string, string> = { done: 'done', in_progress: 'in progress', pending: 'pending' };

/** Plan items from an agent.plan event, or null when the payload is malformed. */
export function planItems(ev: WeftEvent): PlanItem[] | null {
  const data = ev.data && typeof ev.data === 'object' ? (ev.data as Record<string, unknown>) : {};
  if (!Array.isArray(data.items)) return null;
  const items = data.items.filter((x): x is PlanItem => !!x && typeof x === 'object' && typeof (x as PlanItem).step === 'string' && typeof (x as PlanItem).status === 'string');
  return items.length ? items : null;
}

/** Latest plan per execution, newest execution first. */
export function latestPlans(events: WeftEvent[]): AgentPlan[] {
  const byExecution = new Map<string, AgentPlan>();
  for (const ev of events) {
    if (ev.type !== 'agent.plan' || !ev.executionId) continue;
    const items = planItems(ev);
    if (!items) continue;
    byExecution.set(ev.executionId, { executionId: ev.executionId, nodeId: ev.nodeId ?? '', replica: ev.replica, items, time: ev.time });
  }
  return [...byExecution.values()].reverse();
}

/** Agents' working plans (plan.update), updated live. Plans are agent claims, not verification. */
export function TaskPlan({ graphId }: { graphId: string }) {
  const [events, setEvents] = useState<WeftEvent[]>([]);
  const [error, setError] = useState('');
  const load = useCallback(async () => {
    try {
      setEvents(await api.events(graphId, { types: 'agent.plan', limit: 200 }));
      setError('');
    } catch (e) {
      setError(errorMessage(e));
    }
  }, [graphId]);
  useEffect(() => { void load(); }, [load]);
  useOptionalEventStream({ graph: graphId, types: ['agent.plan'] }, ev => setEvents(current => current.some(x => x.id === ev.id) ? current : [...current, ev]));
  const plans = latestPlans(events);
  if (!plans.length && !error) return null;
  return <section aria-label="Agent plans"><h3>Agent plans</h3>
    <ErrorBox error={error && `Plans unavailable: ${error}`} />
    <p className="muted">Each agent's own checklist. Steps marked done are the agent's claim; check the evidence below.</p>
    {plans.map(p => {
      const done = p.items.filter(i => i.status === 'done').length;
      return <article className="card" key={p.executionId} aria-label={`Plan of ${p.nodeId}`}>
        <h4>{p.nodeId}{p.replica ? ` · replica ${p.replica}` : ''} · {done}/{p.items.length} done</h4>
        <ul>{p.items.map((i, n) => <li key={n}><span aria-hidden="true">{MARK[i.status] ?? '?'}</span> {i.step} <span className="muted">({LABEL[i.status] ?? i.status})</span></li>)}</ul>
      </article>;
    })}
  </section>;
}
