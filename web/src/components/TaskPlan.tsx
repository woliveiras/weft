import { useEffect, useState } from 'react';
import { api, errorMessage } from '../api/client';
import type { WeftEvent } from '../api/types';
import { useOptionalEventStream } from '../hooks/useEventStream';
import { Badge, Btn, ErrorBox, fmtDateTime } from './common';

export interface PlanItem { step: string; status: string }
interface AgentPlan { executionId: string; nodeId: string; replica?: number; items: PlanItem[]; time: string; seq: number }

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
  for (const ev of [...events].sort((a,b) => a.seq-b.seq)) {
    if (ev.type !== 'agent.plan' || !ev.executionId) continue;
    const items = planItems(ev);
    if (!items) continue;
    byExecution.set(ev.executionId, { executionId: ev.executionId, nodeId: ev.nodeId ?? '', replica: ev.replica, items, time: ev.time, seq: ev.seq });
  }
  return [...byExecution.values()].sort((a,b) => b.seq-a.seq);
}

/** Current plans are separate from older attempts by the same agent replica. */
export function currentPlans(plans: AgentPlan[]): AgentPlan[] {
  const seen = new Set<string>();
  return plans.filter(plan => {
    const key = JSON.stringify([plan.nodeId, plan.replica ?? 0]);
    if (seen.has(key)) return false;
    seen.add(key); return true;
  });
}

function PlanCard({plan, expanded}: {plan: AgentPlan; expanded: boolean}) {
  const done = plan.items.filter(item => item.status === 'done').length;
  const working = plan.items.find(item => item.status === 'in_progress');
  return <details className="task-agent-plan" open={expanded}>
    <summary><span><strong>{plan.nodeId || 'Agent'} · replica {plan.replica ?? 0}</strong><span className="task-plan-current">{working ? `In progress: ${working.step}` : done === plan.items.length ? 'All plan steps marked done' : 'Waiting for the next step'}</span></span><Badge>{done}/{plan.items.length} done</Badge></summary>
    <ol>{plan.items.map((item, index) => <li className={`task-plan-step plan-${Object.hasOwn(MARK,item.status) ? item.status : 'unknown'}`} key={index}><span className="task-plan-mark" aria-hidden="true">{MARK[item.status] ?? '?'}</span><span>{item.step}</span><span className="muted small">{LABEL[item.status] ?? item.status}</span></li>)}</ol>
    <p className="muted small">Plan updated <time dateTime={plan.time}>{fmtDateTime(plan.time)}</time> · Execution <code>{plan.executionId}</code></p>
  </details>;
}

/** Agents' working plans (plan.update), updated live. Plans are agent claims, not verification. */
export function TaskPlan({ graphId }: { graphId: string }) {
  const [events, setEvents] = useState<WeftEvent[]>([]);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    let disposed = false;
    setLoading(true);
    void api.events(graphId, {types: 'agent.plan', limit: 200}).then(loaded => {
      if (disposed) return;
      setEvents(current => [...loaded.filter(event => event.graphId === graphId), ...current.filter(event => event.graphId === graphId && !loaded.some(other => other.id === event.id))].sort((a,b) => a.seq-b.seq).slice(-400)); setError('');
    }).catch(cause => {if (!disposed) setError(errorMessage(cause));}).finally(() => {if (!disposed) setLoading(false);});
    return () => {disposed = true;};
  }, [graphId, refresh]);
  useOptionalEventStream({ graph: graphId, types: ['agent.plan'] }, ev => setEvents(current => current.some(x => x.id === ev.id) ? current : [...current, ev].slice(-400)));
  const plans = latestPlans(events.filter(event => event.graphId === graphId));
  const current = currentPlans(plans);
  const older = plans.filter(plan => !current.includes(plan));
  return <section className="card task-plans" aria-label="Agent plans"><div className="task-detail-heading"><h2>Agent plans</h2><Btn small disabled={loading} onClick={() => setRefresh(value => value+1)}>Refresh plans</Btn></div>
    <ErrorBox error={error && `Plans unavailable: ${error}`} />
    <p className="muted small">Each agent's own checklist. Steps marked done are the agent's claim, not verified test results.</p>
    {loading && !plans.length && <p role="status">Loading agent plans…</p>}
    {!loading && !error && !plans.length && <p>No agent plan recorded yet. Plans appear when an agent publishes its working checklist.</p>}
    {error && !!plans.length && <p className="muted">Showing previously recorded plans; refresh failed.</p>}
    {current.map((plan, index) => <PlanCard key={plan.executionId} plan={plan} expanded={index === 0 || plan.items.some(item => item.status === 'in_progress')}/>)}
    {!!older.length && <details className="task-plan-history"><summary>Earlier execution plans ({older.length})</summary>{older.map(plan => <PlanCard key={plan.executionId} plan={plan} expanded={false}/>)}</details>}
    <p className="muted small">Latest plans from up to 200 recorded plan events, plus live updates.</p>
  </section>;
}
