import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { api, errorMessage } from '../api/client';
import type { ActivityView } from '../api/activity';
import { useThrottled } from '../hooks/useData';
import { useEventStream } from '../hooks/useEventStream';
import { ActivityHealth } from './ActivityHealth';
import { executionProgress } from './activityPresentation';
import { Badge, Btn, ErrorBox, PageHeading, fmtDateTime, fmtDuration, fmtTime, since } from './common';
import { money } from './cost';
import { runLink } from './runPresentation';
import {
  activityQuery, activityWindows, counterText, elapsedMs, groupTitle, healthWindow, needLabel, nodeLink, phaseLabel, phaseTone,
  sectionErrors, statusFilters, taskLink, tokens, type ActivityFocus, type ActivityRouteState,
} from './activityView';

/** Events that change what the page shows; each triggers a throttled refetch. */
const LIVE_PREFIXES = ['execution.', 'replica.', 'approval.', 'graph.', 'agent.plan', 'trigger.'];
const REFETCH_MS = 1500;

function useActivity(state: ActivityRouteState) {
  const [view, setView] = useState<ActivityView>();
  const [error, setError] = useState<string>();
  const [loading, setLoading] = useState(true);
  const [fetchedAt, setFetchedAt] = useState(0);
  const generation = useRef(0);
  const key = JSON.stringify(activityQuery(state));
  const load = useCallback(async () => {
    const mine = ++generation.current;
    setLoading(true);
    try {
      const v = await api.activity(JSON.parse(key));
      if (mine !== generation.current) return;
      setView(v);
      setFetchedAt(Date.now());
      setError(undefined);
    } catch (e) {
      if (mine === generation.current) setError(errorMessage(e));
    } finally {
      if (mine === generation.current) setLoading(false);
    }
  }, [key]);
  useEffect(() => {
    void load();
    return () => { generation.current++; };
  }, [load]);
  const refetch = useThrottled(() => void load(), REFETCH_MS);
  useEventStream({ prefixes: LIVE_PREFIXES }, refetch);
  return { view, error, loading, fetchedAt, reload: load };
}

function useNow(active: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [active]);
  return now;
}

function costCell(micros: number | null | undefined, currency?: string) {
  return micros === null || micros === undefined ? <span className="muted" title="Cost could not be read">—</span> : money(micros, currency);
}

function SectionNotes({ view, sections }: { view?: ActivityView; sections: string[] }) {
  return <>{sectionErrors(view, ...sections).map(e => e.code === 'failed'
    ? <ErrorBox key={e.section} error={e.message} />
    : <p key={e.section} className="muted small" role="note">{e.message}</p>)}</>;
}

function ActivityTabs({ state, onChange }: { state: ActivityRouteState; onChange: (next: ActivityRouteState) => void }) {
  const tabs: { tab: ActivityRouteState['tab']; label: string }[] = [{ tab: '', label: 'Overview' }, { tab: 'health', label: 'Health' }];
  return <div className="tabs activity-tabs" role="tablist" aria-label="Activity views">
    {tabs.map(t => <button key={t.label} type="button" role="tab" aria-selected={state.tab === t.tab} className={state.tab === t.tab ? 'on' : ''}
      onClick={() => state.tab !== t.tab && onChange(t.tab === 'health'
        ? { ...state, tab: 'health', window: healthWindow(state.window), focus: '', status: '' }
        : { ...state, tab: '' })}>{t.label}</button>)}
  </div>;
}

/** The Activity page: the live overview (OBS-01) or health rankings (OBS-02). */
export function ActivityPage({ state, onChange }: { state: ActivityRouteState; onChange: (next: ActivityRouteState) => void }) {
  const tabs = <ActivityTabs state={state} onChange={onChange} />;
  return state.tab === 'health'
    ? <ActivityHealth state={state} onChange={onChange} tabs={tabs} />
    : <ActivityOverview state={state} onChange={onChange} tabs={tabs} />;
}

function ActivityOverview({ state, onChange, tabs }: { state: ActivityRouteState; onChange: (next: ActivityRouteState) => void; tabs: ReactNode }) {
  const { view, error, loading, fetchedAt, reload } = useActivity(state);
  const ticking = !!view?.needsYou?.length || !!view?.running?.some(g => g.replicas.some(r => r.startedAt));
  const now = useNow(ticking);
  const c = view?.counters;
  const t = view?.truncated;
  const set = (patch: Partial<ActivityRouteState>) => onChange({ ...state, ...patch });
  const focusOn = (focus: ActivityFocus, status = state.status) =>
    state.focus === focus && state.status === status ? set({ focus: '', status: '' }) : set({ focus, status });
  const show = (f: ActivityFocus) => !state.focus || state.focus === f;
  const counters: { label: string; value: string; focus: ActivityFocus; status?: typeof state.status; hint: string }[] = [
    { label: 'Agents running', value: counterText(c?.running, t?.running), focus: 'running', hint: 'Agent replicas in a turn now' },
    { label: 'Waiting for a person', value: counterText(c?.waiting, t?.needsYou), focus: 'needs', hint: 'Pending approvals, questions and reviews' },
    { label: 'Failed', value: counterText(c?.failed, t?.executions), focus: 'history', status: 'failed', hint: `Failed or interrupted executions in the last ${state.window}` },
    { label: 'Finished', value: counterText(c?.finished, t?.executions), focus: 'history', status: 'succeeded', hint: `Succeeded executions in the last ${state.window}` },
  ];

  return <div className="page activity-page" aria-label="Activity">
    <PageHeading title="Activity" description="What is running across workflows, what is waiting for you, and what happened recently.">
      <div className="btn-row">
        <label>Window <select value={state.window} onChange={e => set({ window: e.target.value })}>
          {activityWindows.map(w => <option key={w} value={w}>last {w}</option>)}
        </select></label>
        <Btn onClick={() => void reload()} disabled={loading}>Refresh</Btn>
      </div>
    </PageHeading>
    {tabs}
    <p role="status" className="muted small">
      {loading && !view ? 'Loading activity…' : view ? `Updated ${fmtTime(view.generatedAt)} · updates live from the event stream.` : ''}
    </p>
    {error && <ErrorBox error={view ? `Could not refresh activity: ${error}. Showing data from ${fmtTime(view.generatedAt)}.` : error} />}

    {view && <>
      <section className="activity-counters" aria-label="Activity counters">
        {counters.map(k => {
          const pressed = state.focus === k.focus && (k.status ?? '') === (k.focus === 'history' ? state.status : '');
          return <button key={k.label} type="button" className={`activity-counter ${pressed ? 'on' : ''}`} aria-pressed={pressed} title={k.hint}
            onClick={() => focusOn(k.focus, k.status ?? '')}>
            <span className="activity-counter-value">{k.value}</span><span className="activity-counter-label">{k.label}</span>
          </button>;
        })}
      </section>
      {state.focus && <p className="activity-focus">Showing only {state.focus === 'needs' ? 'what needs you' : state.focus === 'running' ? 'running agents' : 'recent history'}.{' '}
        <Btn onClick={() => set({ focus: '', status: '' })}>Show everything</Btn></p>}

      {show('needs') && <section className="activity-section" aria-label="Needs you">
        <h2>Needs you</h2>
        <p className="muted small">Oldest first. Decide each item where it is reviewed.</p>
        <SectionNotes view={view} sections={['needsYou', 'reviews']} />
        {view.needsYou && (view.needsYou.length === 0
          ? <p>Nothing is waiting for a person.</p>
          : <ol className="activity-needs">{view.needsYou.map(n => <li key={`${n.kind}:${n.id}`} className="card">
            <header><Badge tone={n.kind === 'question' ? 'info' : 'warn'}>{needLabel(n.kind)}</Badge><strong>{n.title || n.id}</strong></header>
            <p className="muted small">
              {n.task ? `Task ${n.task} · ` : ''}{n.graphName || n.graphId}{n.nodeId ? ` · ${n.nodeId} #${n.replica}` : ''} · waiting {since(n.createdAt, new Date(now).toISOString())}
              {n.expiresAt ? ` · expires ${fmtDateTime(n.expiresAt)}` : ''}
            </p>
            <p className="btn-row">
              <a href="#/approvals">{n.kind === 'review' ? 'Open the review' : n.kind === 'question' ? 'Answer in Approvals' : 'Decide in Approvals'}</a>
              {n.executionId && <a href={runLink(n.graphId, n.executionId)}>Run details</a>}
              {n.task && <a href={taskLink(n.task)}>Open task</a>}
            </p>
          </li>)}</ol>)}
        {t?.needsYou && <p className="muted small">More items are waiting than shown here; the {view.limits.needsYou} oldest are listed. See Approvals for all of them.</p>}
      </section>}

      {show('needs') && (Boolean(view.triggerHolds?.length) || view.errors.some(e => e.section === 'triggerHolds')) && (
        <section className="activity-section" aria-label="Issues awaiting readiness">
          <h2>Issues awaiting readiness</h2>
          <SectionNotes view={view} sections={['triggerHolds']} />
          <ul>{view.triggerHolds?.map(h => <li key={`${h.triggerId}:${h.key}`} className="card">
            <strong>{h.url ? <a href={h.url}>{h.key}</a> : h.key}</strong> · {h.triggerName} · round {h.round}
            <p>{h.reason}{h.eligibility ? ` The issue ${h.eligibility}.` : ''}{h.disabled ? ' Readiness is disabled for this trigger.' : ''}</p>
            {h.publicationMessage && <p>{h.publicationMessage}</p>}
            {h.publication === 'comment_pending' && <p>Tracker comment delivery is unconfirmed.</p>}
            {(h.publication === 'label_pending' || h.publication === 'label_pending_only') && <p>Tracker label update is pending.</p>}
          </li>)}</ul>
          {view.triggerHoldsTruncated && <p>Open the trigger for its complete list of held issues.</p>}
        </section>
      )}

      {(show('needs') || show('history')) && Boolean(view.resolvedTriggerHolds?.length) && (
        <section className="activity-section" aria-label="Resolved readiness holds">
          <h2>Resolved readiness holds</h2>
          <ul>{view.resolvedTriggerHolds?.map((h, index) => <li key={`${h.triggerId}:${h.key}:${h.resolvedAt}:${index}`}>
            <strong>{h.key}</strong> · {h.triggerName} · round {h.round} · {h.reason}
          </li>)}</ul>
        </section>
      )}

      {show('running') && <section className="activity-section" aria-label="Running now">
        <h2>Running now</h2>
        <SectionNotes view={view} sections={['running']} />
        {view.running && (view.running.length === 0
          ? <p>No workflow or task is running.</p>
          : view.running.map(g => <article key={g.graphId} className="card activity-group" aria-label={groupTitle(g)}>
            <header>
              <strong>{groupTitle(g)}</strong><Badge>{g.state}</Badge>
              <a href={`#/g/${encodeURIComponent(g.graphId)}/canvas`}>Open workflow</a>
              {g.task && <a href={taskLink(g.task)}>Open task</a>}
            </header>
            <div className="connection-table"><table>
              <caption className="sr-only">Agents of {groupTitle(g)}</caption>
              <thead><tr><th>Agent</th><th>State</th><th>Current step</th><th>Elapsed</th><th>Tokens</th><th>Cost</th><th>Details</th></tr></thead>
              <tbody>{g.replicas.map(r => {
                const ms = elapsedMs(r, fetchedAt, now);
                return <tr key={`${r.nodeId}#${r.replica}`}>
                  <td><a href={nodeLink(g.graphId, r.nodeId)}>{r.nodeId}</a>{r.replica > 0 && <span className="muted"> #{r.replica}</span>}</td>
                  <td><Badge tone={phaseTone(r.phase)}>{phaseLabel(r.phase)}</Badge>{r.detail && <div className="muted small">{r.detail}</div>}</td>
                  <td>{r.planStep || <span className="muted">—</span>}</td>
                  <td>{ms === undefined ? <span className="muted">—</span> : fmtDuration(ms)}</td>
                  <td>{r.executionId ? tokens(r.usage) : <span className="muted">—</span>}</td>
                  <td>{r.executionId ? costCell(r.costMicros, view.currency) : <span className="muted">—</span>}</td>
                  <td>{r.executionId ? <a href={runLink(g.graphId, r.executionId)}>Run details</a> : <a href={nodeLink(g.graphId, r.nodeId)}>Agent panel</a>}</td>
                </tr>;
              })}</tbody>
            </table></div>
          </article>))}
        {t?.running && <p className="muted small">Only the {view.limits.activeGraphs} most recently started workflows and {view.limits.replicas} agents are shown.</p>}
      </section>}

      {show('history') && <section className="activity-section" aria-label="Recent history">
        <h2>Recent history</h2>
        <p className="muted small">Executions started in the last {view.window}, newest first, up to {view.limits.history}. Cost counts records in the window.</p>
        <div className="connection-filters">
          <label>Status<select value={state.status} onChange={e => set({ status: e.target.value as typeof state.status })}>
            {statusFilters.map(s => <option key={s.value} value={s.value}>{s.label}</option>)}
          </select></label>
          <label>Workflow<select value={state.workflow} onChange={e => set({ workflow: e.target.value })}>
            <option value="">All workflows</option>
            {state.workflow && !view.options?.workflows.some(w => w.id === state.workflow) && <option value={state.workflow}>{state.workflow}</option>}
            {view.options?.workflows.map(w => <option key={w.id} value={w.id}>{w.name || w.id}</option>)}
          </select></label>
          <label>Agent<select value={state.agent} onChange={e => set({ agent: e.target.value })}>
            <option value="">All agents</option>
            {state.agent && !view.options?.agents.includes(state.agent) && <option value={state.agent}>{state.agent}</option>}
            {view.options?.agents.map(a => <option key={a} value={a}>{a}</option>)}
          </select></label>
        </div>
        <SectionNotes view={view} sections={['history', 'cost']} />
        {view.history && (view.history.length === 0
          ? <p>{state.status || state.workflow || state.agent ? 'No executions match these filters in this window.' : 'No executions started in this window.'}</p>
          : <div className="connection-table"><table>
            <caption className="sr-only">Recent executions</caption>
            <thead><tr><th>Execution</th><th>Agent</th><th>Workflow</th><th>Status</th><th>Started</th><th>Duration</th><th>Tool calls</th><th>Tokens</th><th>Cost</th><th>Error</th></tr></thead>
            <tbody>{view.history.map(r => <tr key={r.id}>
              <td><a href={runLink(r.graphId, r.id)}>{r.id}</a></td>
              <td>{r.nodeId}</td>
              <td>{r.task ? <a href={taskLink(r.task)}>Task {r.task}</a> : (r.graphName || r.graphId)}</td>
              <td><Badge tone={r.status === 'succeeded' ? 'ok' : r.status === 'failed' || r.status === 'interrupted' ? 'bad' : undefined}>{executionProgress(r)}</Badge></td>
              <td>{fmtDateTime(r.startedAt)}</td>
              <td>{fmtDuration(r.durationMs)}</td>
              <td>{r.toolCalls}</td>
              <td>{tokens(r.usage)}</td>
              <td>{costCell(r.costMicros, view.currency)}</td>
              <td className="activity-error">{r.error || ''}</td>
            </tr>)}</tbody>
          </table></div>)}
        {t?.history && <p className="muted small">{t.executions
          ? `More than ${view.limits.executions} executions started in this window; counts are lower bounds and older ones are not listed.`
          : 'More executions match; narrow the filters or the window to see them.'}</p>}
        {t?.cost && <p className="muted small">Cost reads the newest {view.limits.costEvents} cost records; older costs in this window are not included.</p>}
      </section>}
    </>}
  </div>;
}
