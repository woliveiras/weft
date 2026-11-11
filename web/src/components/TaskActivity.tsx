import { runLink } from './runPresentation';
import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, api, errorMessage } from '../api/client';
import type { Approval, Execution, GraphStatus, HumanReview, WeftEvent, MessageRecord } from '../api/types';
import { useThrottled } from '../hooks/useData';
import { useEventStream } from '../hooks/useEventStream';
import { ApprovalCard } from './ApprovalsView';
import { Badge, Btn, ErrorBox, JsonView, fmtDateTime } from './common';
import { describeEvent } from './describe';
import { belongsToExecution, executionProgress } from './activityPresentation';

type Snapshot = { status: GraphStatus; executions: Execution[]; events: WeftEvent[]; messages: MessageRecord[]; approvals: Approval[]; reviews: HumanReview[] };
type Section = keyof Snapshot;
const sections: Section[] = ['status', 'executions', 'events', 'messages', 'approvals', 'reviews'];

/** Bounded, read-only aggregation of the existing authorized APIs. */
export function TaskActivity({ graphId, role }: { graphId: string; role?: string }) {
  const [data, setData] = useState<Partial<Snapshot>>({});
  const [errors, setErrors] = useState<Partial<Record<Section, string>>>({});
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState('');
  const generation = useRef(0);
  const inFlight = useRef(false);
  const refresh = useCallback(async () => {
    if (inFlight.current) return;
    inFlight.current = true;
    const current = ++generation.current;
    setLoading(true);
    const results = await Promise.allSettled([
      api.status(graphId), api.executions(graphId, { limit: 100 }),
      api.events(graphId, { limit: 300 }), api.messages(graphId, { limit: 100 }),
      api.approvals({ graph: graphId }), api.reviews(),
    ]);
    if (current !== generation.current) return;
    const next: Partial<Snapshot> = {};
    const failures: Partial<Record<Section, string>> = {};
    results.forEach((result, index) => {
      const key = sections[index];
      if (result.status === 'fulfilled') Object.assign(next, { [key]: result.value });
      else if (key === 'reviews' && result.reason instanceof ApiError && result.reason.status === 404) {
        failures[key] = 'Human reviews are not configured on this installation.';
      } else if (key === 'reviews' && result.reason instanceof ApiError && result.reason.status === 403) {
        failures[key] = 'Your role cannot access human reviews.';
      } else failures[key] = errorMessage(result.reason);
    });
    // Failed sections are cleared, never presented as fresh or empty success.
    setData(next);
    setErrors(failures);
    setLoading(false);
    inFlight.current = false;
  }, [graphId]);
  useEffect(() => {
    setData({}); setErrors({}); setSelected(''); inFlight.current = false;
    void refresh();
    const timer = setInterval(() => void refresh(), 5000);
    return () => { generation.current++; clearInterval(timer); };
  }, [refresh]);
  const reload = useThrottled(() => void refresh(), 500);
  useEventStream({ graph: graphId }, reload);
  const executions = (data.executions ?? []).filter(e => e.graphId === graphId);
  const execution = executions.find(e => e.id === selected);
  const events = (data.events ?? []).filter(e => e.graphId === graphId && (!selected || e.executionId === selected));
  const messages = (data.messages ?? []).filter(m => m.graphId === graphId && (!selected || !!execution && belongsToExecution(m, execution)));
  const approvals = (data.approvals ?? []).filter(a => a.graphId === graphId && (!selected || a.executionId === selected));
  const reviews = (data.reviews ?? []).filter(r => r.graphId === graphId);
  const canDecide = role === 'admin' || role === 'operator';
  return <div className="page fill" aria-label="Task activity">
    <div className="page-head"><h2>Task activity</h2><Btn onClick={() => void refresh()} disabled={loading}>Refresh activity</Btn><a href={`#/g/${encodeURIComponent(graphId)}/files`}>Inspect files and diff</a></div>
    <p className="muted">Recent activity, refreshed every five seconds. Up to 100 executions, 100 messages and 300 events; this is not the complete history or a percentage of work completed.</p>
    <p role="status">{loading ? 'Refreshing activity…' : Object.keys(errors).length ? 'Some activity is unavailable. Retry with Refresh activity.' : 'Activity updated.'}</p>
    {sections.map(key => errors[key] && <ErrorBox key={key} error={`${key}: ${errors[key]}`} />)}
    <section aria-label="Graph progress"><h3>Graph progress</h3>
      {data.status && <><p>Graph: <Badge>{data.status.state}</Badge> · Requested: {data.status.desiredState} · {data.status.approvalsPending} pending approvals</p>
        <ul>{data.status.nodes?.flatMap(n => (n.replicas ?? []).map(r => <li key={`${n.nodeId}:${r.replica}`}><a href={`#/g/${encodeURIComponent(graphId)}/canvas?sel=node:${encodeURIComponent(n.nodeId)}`}>{n.name || n.nodeId}</a> · replica {r.replica}: {r.state}{r.detail ? ` — ${r.detail}` : ''}</li>))}</ul></>}
    </section>
    <section aria-label="Executions and results"><h3>Executions and results</h3>
      <label>Execution <select value={selected} onChange={e => setSelected(e.target.value)}><option value="">All recent executions</option>{executions.map(e => <option key={e.id} value={e.id}>{e.nodeId} · {executionProgress(e)} · {e.id}</option>)}</select></label>
      {selected && !execution && <p>The selected execution is outside the recent window or unavailable. Select all executions to continue.</p>}
      {(selected ? executions.filter(e => e.id === selected) : executions).map(e => <article className="card" key={e.id}>
        <h4>{e.nodeId} · {executionProgress(e)}</h4><a href={runLink(graphId,e.id)}>Open run details</a><p><code>{e.id}</code> · replica {e.replica} · {fmtDateTime(e.startedAt)}</p>
        <p>{e.toolCalls} tool calls · {e.usage?.inputTokens ?? 0} input / {e.usage?.outputTokens ?? 0} output tokens</p>
        {e.summary && <p>{e.summary}</p>}{e.error && <ErrorBox error={e.error} />}
        {!e.summary && <p>No result summary recorded.</p>}
      </article>)}
      {data.executions && !executions.length && <p>No executions recorded yet. Start a saved graph with Run all or send an instruction from its agent panel.</p>}
    </section>
    <p className="muted">{selected ? 'Messages, approvals and events below are linked by execution or message ID. Graph progress and reviews remain graph-wide.' : 'Showing graph-wide activity. Select an execution to inspect its linked records.'}</p>
    <section aria-label="Task approvals"><h3>Approvals</h3>
      {approvals.map(a => <ApprovalCard key={a.id} a={a} canDecide={canDecide && (a.kind !== 'environment' || role === 'admin')} onDone={() => void refresh()} />)}
      {data.approvals && !approvals.length && <p>No approvals in this scope.</p>}
    </section>
    <section aria-label="Task reviews"><h3>Human reviews · graph-wide</h3>
      {reviews.map(r => <article className="card" key={r.id}><strong>{r.request.title}</strong><p>{r.state} · preview {r.session}</p>{r.error && <ErrorBox error={r.error} />}<a href="#/approvals">Open review and evidence</a></article>)}
      {data.reviews && !reviews.length && <p>No human reviews for this graph.</p>}
    </section>
    <section aria-label="Task messages"><h3>Messages</h3>
      {messages.map((m, i) => <article className="card" key={`${m.message.id}:${m.channel}:${m.group}:${i}`}><strong>{m.message.type}</strong><p>{m.status} · {m.channel} · {fmtDateTime(m.createdAt)}</p>{m.error && <ErrorBox error={m.error} />}<JsonView value={m.message.payload} label="Message content" /></article>)}
      {data.messages && !messages.length && <p>No messages in this recent scope. Operator instructions are also recorded in Events.</p>}
    </section>
    <section aria-label="Task events"><h3>Events</h3>
      <ol>{events.map(e => <li key={e.id}><time>{fmtDateTime(e.time)}</time> · <strong>{e.type}</strong> · {e.nodeId || 'graph'}<p>{describeEvent(e)}</p></li>)}</ol>
      {data.events && !events.length && <p>No events in this recent scope.</p>}
    </section>
  </div>;
}
