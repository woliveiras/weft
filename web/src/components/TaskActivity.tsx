import { runLink } from './runPresentation';
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
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
const views = ['Overview', 'Approvals', 'Messages', 'Events'] as const;
type ActivityView = typeof views[number];

/** Bounded, read-only aggregation of the existing authorized APIs. */
export function TaskActivity({ graphId, role }: { graphId: string; role?: string }) {
  const [data, setData] = useState<Partial<Snapshot>>({});
  const [errors, setErrors] = useState<Partial<Record<Section, string>>>({});
  const [loading, setLoading] = useState(true);
  const [selected, setSelected] = useState('');
  const [view, setView] = useState<ActivityView>('Overview');
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
    setData({}); setErrors({}); setSelected(''); setView('Overview'); inFlight.current = false;
    void refresh();
    const timer = setInterval(() => void refresh(), 5000);
    return () => { generation.current++; clearInterval(timer); };
  }, [refresh]);
  const reload = useThrottled(() => void refresh(), 500);
  useEventStream({ graph: graphId }, reload);
  const executions = (data.executions ?? []).filter(e => e.graphId === graphId);
  const execution = executions.find(e => e.id === selected);
  const events = (data.events ?? []).filter(e => e.graphId === graphId && (!selected || e.executionId === selected)).sort((a, b) => b.seq - a.seq);
  const messages = (data.messages ?? []).filter(m => m.graphId === graphId && (!selected || !!execution && belongsToExecution(m, execution)));
  const approvals = (data.approvals ?? [])
    .filter(a => a.graphId === graphId && (!selected || a.executionId === selected))
    .sort((a, b) => Number(b.status === 'pending') - Number(a.status === 'pending'));
  const reviews = (data.reviews ?? []).filter(r => r.graphId === graphId);
  const canDecide = role === 'admin' || role === 'operator';
  const shownExecutions = selected ? (execution ? [execution] : []) : executions;
  const pending = approvals.filter(approval => approval.status === 'pending').length;
  const sectionError = (key: Section) => errors[key] ? `${key}: ${errors[key]}` : undefined;

  return <div className="page fill task-activity" aria-label="Task activity">
    <header className="page-head">
      <div><h2>Task activity</h2><p className="muted">Recent workflow activity, refreshed every five seconds.</p></div>
      <div className="btn-row"><Btn onClick={() => void refresh()} disabled={loading}>Refresh activity</Btn><a href={`#/g/${encodeURIComponent(graphId)}/files`}>Inspect files and diff</a></div>
    </header>
    <div className="task-activity-controls">
      <div className="section-navigation" role="group" aria-label="Activity views">
        {views.map(item => <Btn key={item} small aria-pressed={view === item} onClick={() => setView(item)}>
          {item}{item === 'Approvals' && pending > 0 ? ` (${pending} pending)` : ''}
        </Btn>)}
      </div>
      <label>Execution<select value={selected} onChange={event => setSelected(event.target.value)}>
        <option value="">All recent executions</option>
        {selected && !execution && <option value={selected}>Selected execution unavailable</option>}
        {executions.map(run => <option key={run.id} value={run.id}>{run.nodeId} · {executionProgress(run)} · {fmtDateTime(run.startedAt)} · {run.id}</option>)}
      </select></label>
    </div>
    <p className="small muted" role="status">{loading ? 'Refreshing activity…' : Object.keys(errors).length ? 'Some activity is unavailable. Open its section for details or refresh to retry.' : selected ? 'Showing records linked to this execution. Workflow progress and reviews cover the entire workflow.' : 'Showing recent records for the workflow.'}</p>
    {selected && !execution && <p>The selected execution is outside the recent window or unavailable. Select all executions to continue.</p>}

    {view === 'Overview' && <>
      <section className="card" aria-label="Graph progress"><h3>Workflow progress</h3>
        <ErrorBox error={sectionError('status')} />
        {data.status && <>
          <p>Graph: <Badge>{data.status.state}</Badge> · Requested: {data.status.desiredState} · {data.status.approvalsPending} pending approvals</p>
          <ul className="task-replicas">{data.status.nodes?.flatMap(node => (node.replicas ?? []).map(replica =>
            <li key={`${node.nodeId}:${replica.replica}`}><a href={`#/g/${encodeURIComponent(graphId)}/canvas?sel=node:${encodeURIComponent(node.nodeId)}`}>{node.name || node.nodeId}</a> · replica {replica.replica}: {replica.state}{replica.detail ? ` — ${replica.detail}` : ''}</li>,
          ))}</ul>
        </>}
      </section>
      <section aria-label="Executions and results"><h3>Executions and results</h3>
        <ErrorBox error={sectionError('executions')} />
        {data.executions && executions.length === 0 && <p>No executions recorded yet. Start a saved graph with Run all or send an instruction from its agent panel.</p>}
        <ActivityPages key={selected} label="Executions" items={shownExecutions} limit={6}>{runs => <div className="connection-table task-activity-table"><table>
          <thead><tr><th scope="col">Agent</th><th scope="col">Status</th><th scope="col">Started</th><th scope="col">Result</th></tr></thead>
          <tbody>{runs.map(run => <tr key={run.id}>
            <td><a href={runLink(graphId, run.id)}>{run.nodeId}</a><span className="small muted">Replica {run.replica}</span></td>
            <td><Badge>{executionProgress(run)}</Badge></td><td>{fmtDateTime(run.startedAt)}</td>
            <td>{run.summary ? preview(run.summary) : !run.error && 'No result summary recorded.'}
              {run.error && <span className="bad small">{preview(run.error)}</span>}
              {selected && <span className="small muted">{run.toolCalls} tool calls · {run.usage?.inputTokens ?? 0} input / {run.usage?.outputTokens ?? 0} output tokens</span>}
            </td>
          </tr>)}</tbody>
        </table></div>}</ActivityPages>
      </section>
    </>}
    {view === 'Approvals' && <>
      <section aria-label="Task approvals"><h3>Approvals</h3><ErrorBox error={sectionError('approvals')} />
        <ActivityPages key={selected} label="Approvals" items={approvals} limit={6}>{rows => <div className="task-activity-records">{rows.map(a => <ApprovalCard key={a.id} a={a}
          canDecide={canDecide && (a.kind !== 'environment' || role === 'admin')} onDone={() => void refresh()} />)}</div>}</ActivityPages>
        {data.approvals && !approvals.length && <p>No approvals in this scope.</p>}
      </section>
      <section aria-label="Task reviews"><h3>Workflow reviews</h3><ErrorBox error={sectionError('reviews')} />
        <ActivityPages label="Reviews" items={reviews} limit={6}>{rows => <div className="task-activity-records">{rows.map(review => <article className="card" key={review.id}>
          <strong>{review.request.title}</strong><p>{review.state} · preview {review.session}</p><ErrorBox error={review.error} /><a href="#/approvals">Open review and evidence</a>
        </article>)}</div>}</ActivityPages>
        {data.reviews && !reviews.length && <p>No human reviews for this graph.</p>}
      </section>
    </>}
    {view === 'Messages' && <section aria-label="Task messages"><h3>Messages</h3><ErrorBox error={sectionError('messages')} />
      {data.messages && !messages.length && <p>No messages in this recent scope. Operator instructions are also recorded in Events.</p>}
      <ActivityPages key={selected} label="Messages" items={messages} limit={8}>{rows => <div className="task-activity-records">{rows.map((message, index) => <article className="card" key={`${message.message.id}:${message.channel}:${message.group}:${index}`}>
        <strong>{message.message.type}</strong><p className="muted small">{message.status} · {message.channel} · {fmtDateTime(message.createdAt)}</p>
        <ErrorBox error={message.error} /><JsonView value={message.message.payload} label="Message content" />
      </article>)}</div>}</ActivityPages>
    </section>}
    {view === 'Events' && <section aria-label="Task events"><h3>Events</h3><ErrorBox error={sectionError('events')} />
      {data.events && !events.length && <p>No events in this recent scope.</p>}
      <ActivityPages key={selected} label="Events" items={events} limit={12}>{rows => <div className="connection-table task-activity-table"><table>
        <thead><tr><th scope="col">Time</th><th scope="col">Event</th><th scope="col">Source</th><th scope="col">Details</th></tr></thead>
        <tbody>{rows.map(event => <tr key={event.id}>
          <td><time dateTime={event.time}>{fmtDateTime(event.time)}</time></td><td><code>{event.type}</code></td><td>{event.nodeId || event.actor || 'Workflow'}</td>
          <td>{activityEventDescription(event)}{event.data !== undefined && <JsonView value={event.data} label="Event data" />}</td>
        </tr>)}</tbody>
      </table></div>}</ActivityPages>
    </section>}
  </div>;
}

function preview(text: string) {
  return text.length > 240 ? `${text.slice(0, 239)}…` : text;
}

function activityEventDescription(event: WeftEvent) {
  if (event.type === 'workspace.observed' && event.data && typeof event.data === 'object' && 'status' in event.data) {
    if (event.data.status === 'partial') return 'Workspace observation recorded with incomplete file information.';
    return 'Workspace observation recorded.';
  }
  const description = describeEvent(event);
  return /^[{\[]/.test(description) ? '' : preview(description);
}

function ActivityPages<T>({ label, items, limit, children }: {
  label: string; items: T[]; limit: number; children: (rows: T[]) => ReactNode;
}) {
  const [page, setPage] = useState(0);
  if (!items.length) return null;
  const last = Math.ceil(items.length / limit) - 1;
  const current = Math.min(page, last);
  const start = current * limit;
  return <>
    {children(items.slice(start, start + limit))}
    {last > 0 && <nav className="btn-row task-activity-pages" aria-label={`${label} pages`}>
      <Btn small disabled={current === 0} onClick={() => setPage(current - 1)}>Previous page</Btn>
      <span className="muted small">{start + 1}–{Math.min(start + limit, items.length)} of {items.length} recent records</span>
      <Btn small disabled={current === last} onClick={() => setPage(current + 1)}>Next page</Btn>
    </nav>}
  </>;
}
